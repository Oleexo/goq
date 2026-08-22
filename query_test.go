package goq_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/oleexo/goq"
	"github.com/oleexo/goq/internal/seqcore"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestFromToSlice(t *testing.T) {
	t.Parallel()
	got := goq.From([]int{1, 2, 3}).ToSlice()
	if d := cmp.Diff([]int{1, 2, 3}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestFromNilSliceIsEmptyNotNilPanic(t *testing.T) {
	t.Parallel()
	if got := goq.From[int](nil).ToSlice(); len(got) != 0 {
		t.Errorf("From(nil).ToSlice() = %v, want empty", got)
	}
}

// Seq and ToSlice are two exits from the same pipeline; they must never differ.
func TestSeqMatchesToSlice(t *testing.T) {
	t.Parallel()
	q := goq.From([]string{"a", "b", "c"})
	if d := cmp.Diff(q.ToSlice(), slices.Collect(q.Seq())); d != "" {
		t.Errorf("Seq/ToSlice diverged (-toSlice +seq):\n%s", d)
	}
}

func TestRange(t *testing.T) {
	t.Parallel()
	if d := cmp.Diff([]int{5, 6, 7}, goq.Range(5, 3).ToSlice()); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
	if got := goq.Range(0, 0).ToSlice(); len(got) != 0 {
		t.Errorf("Range(0,0) = %v, want empty", got)
	}
}

func TestRepeatAndEmpty(t *testing.T) {
	t.Parallel()
	if d := cmp.Diff([]string{"x", "x"}, goq.Repeat("x", 2).ToSlice()); d != "" {
		t.Errorf("Repeat mismatch (-want +got):\n%s", d)
	}
	if got := goq.Empty[int]().ToSlice(); len(got) != 0 {
		t.Errorf("Empty = %v, want empty", got)
	}
}

func TestFromMap(t *testing.T) {
	t.Parallel()
	got := goq.FromMap(map[string]int{"a": 1, "b": 2}).ToSlice()
	want := []goq.Pair[string, int]{{Key: "a", Value: 1}, {Key: "b", Value: 2}}
	// Map iteration order is unspecified, so compare as a set.
	if d := cmp.Diff(want, got, cmpopts.SortSlices(func(x, y goq.Pair[string, int]) bool {
		return x.Key < y.Key
	})); d != "" {
		t.Errorf("FromMap mismatch (-want +got):\n%s", d)
	}
}

// A slice-backed query re-executes on every enumeration (spec §3.4).
func TestQueryIsReEnumerable(t *testing.T) {
	t.Parallel()
	q := goq.From([]int{1, 2, 3})
	if d := cmp.Diff(q.ToSlice(), q.ToSlice()); d != "" {
		t.Errorf("second enumeration differed (-first +second):\n%s", d)
	}
}

func TestFromSeqIsLazy(t *testing.T) {
	t.Parallel()
	c := &seqcore.Counter{}
	q := goq.FromSeq(c.Seq(100))
	for range q.Seq() {
		break
	}
	if got := c.Pulls(); got != 1 {
		t.Errorf("pulled %d, want 1", got)
	}
}
