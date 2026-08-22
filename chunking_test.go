package goq_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/oleexo/goq"
	"github.com/oleexo/goq/internal/seqcore"
)

func TestChunk(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  [][]int
		want [][]int
	}{
		{"exact", goq.Range(1, 4).Chunk(2).ToSlice(), [][]int{{1, 2}, {3, 4}}},
		{"ragged final chunk", goq.Range(1, 5).Chunk(2).ToSlice(), [][]int{{1, 2}, {3, 4}, {5}}},
		{"size exceeds source", goq.Range(1, 2).Chunk(10).ToSlice(), [][]int{{1, 2}}},
		{"empty source", goq.Empty[int]().Chunk(3).ToSlice(), [][]int{}},
		{"size zero yields nothing", goq.Range(1, 3).Chunk(0).ToSlice(), [][]int{}},
		{"negative size yields nothing", goq.Range(1, 3).Chunk(-1).ToSlice(), [][]int{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if d := cmp.Diff(tc.want, tc.got); d != "" {
				t.Errorf("mismatch (-want +got):\n%s", d)
			}
		})
	}
}

func TestChunkSelectReEntersQuery(t *testing.T) {
	t.Parallel()
	got := goq.Range(1, 5).
		Chunk(2).
		Where(func(batch []int) bool { return len(batch) == 2 }).
		Select(func(batch []int) int { return len(batch) }).
		ToSlice()
	if d := cmp.Diff([]int{2, 2}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

// Chunk streams: it must emit the first batch without draining the source, and
// must terminate against an infinite source.
func TestChunkStreams(t *testing.T) {
	t.Parallel()
	c := &seqcore.Counter{}
	first := true
	for batch := range goq.FromSeq(c.Seq(1000)).Chunk(3).Seq() {
		if len(batch) != 3 {
			t.Errorf("batch len = %d, want 3", len(batch))
		}
		if first && c.Pulls() != 3 {
			t.Errorf("pulled %d for the first batch, want 3", c.Pulls())
		}
		break
	}
	c2 := &seqcore.Counter{}
	got := goq.FromSeq(c2.Seq(1_000_000)).Chunk(2).Select(func(b []int) int { return b[0] }).Take(2).ToSlice()
	if d := cmp.Diff([]int{0, 2}, got); d != "" {
		t.Errorf("large finite source mismatch (-want +got):\n%s", d)
	}
	if pulls := c2.Pulls(); pulls != 4 {
		t.Errorf("Chunk pulled %d for two batches of 2, want 4", pulls)
	}
}

// Each yielded batch must be independent; reusing one buffer would alias.
func TestChunkBatchesDoNotAlias(t *testing.T) {
	t.Parallel()
	batches := goq.Range(1, 4).Chunk(2).ToSlice()
	batches[0][0] = 99
	if batches[1][0] == 99 {
		t.Error("batches share backing storage")
	}
	if d := cmp.Diff([][]int{{99, 2}, {3, 4}}, batches); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}
