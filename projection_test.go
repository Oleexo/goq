package goq_test

import (
	"fmt"
	"iter"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/oleexo/goq"
	"github.com/oleexo/goq/internal/seqcore"
)

func TestSelectChangesElementType(t *testing.T) {
	t.Parallel()
	got := goq.From([]int{1, 2, 3}).
		Select(func(i int) string { return fmt.Sprintf("n%d", i) }).
		Select(func(s string) int { return len(s) }).
		ToSlice()
	if d := cmp.Diff([]int{2, 2, 2}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestSelectIndex(t *testing.T) {
	t.Parallel()
	got := goq.From([]string{"a", "b"}).
		SelectIndex(func(i int, s string) string { return fmt.Sprintf("%d%s", i, s) }).
		ToSlice()
	if d := cmp.Diff([]string{"0a", "1b"}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestSelectMany(t *testing.T) {
	t.Parallel()
	got := goq.From([][]int{{1, 2}, {}, {3}}).
		SelectMany(func(s []int) []int { return s }).
		ToSlice()
	if d := cmp.Diff([]int{1, 2, 3}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestSelectManySeq(t *testing.T) {
	t.Parallel()
	got := goq.From([]int{2, 3}).
		SelectManySeq(func(n int) iter.Seq[int] { return slices.Values(make([]int, n)) }).
		ToSlice()
	if len(got) != 5 {
		t.Errorf("len = %d, want 5", len(got))
	}
}

// Zip stops at the shorter input, in both orders.
func TestZip(t *testing.T) {
	t.Parallel()
	long, short := goq.From([]int{1, 2, 3}), goq.From([]string{"a", "b"})
	got := long.Zip(short, func(i int, s string) string { return fmt.Sprintf("%d%s", i, s) }).ToSlice()
	if d := cmp.Diff([]string{"1a", "2b"}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
	got2 := short.Zip(long, func(s string, i int) string { return fmt.Sprintf("%s%d", s, i) }).ToSlice()
	if d := cmp.Diff([]string{"a1", "b2"}, got2); d != "" {
		t.Errorf("reversed mismatch (-want +got):\n%s", d)
	}
}

func TestSelectIsLazy(t *testing.T) {
	t.Parallel()
	calls := 0
	c := &seqcore.Counter{}
	q := goq.FromSeq(c.Seq(1000)).Select(func(i int) int { calls++; return i })
	if _, ok := q.First(); !ok {
		t.Fatal("First() reported empty")
	}
	if calls != 1 || c.Pulls() != 1 {
		t.Errorf("selector called %d times, pulled %d; want 1 and 1", calls, c.Pulls())
	}
}

// Zip must stop at the shorter side and must not leak the pull-iterator
// coroutine it uses internally. goleak in TestMain catches the leak; the
// Counter assertion catches eagerness without hanging.
func TestZipStopsAtShorterSide(t *testing.T) {
	t.Parallel()
	c := &seqcore.Counter{}
	got := goq.FromSeq(c.Seq(1_000_000)).
		Zip(goq.From([]int{10, 20}), func(a, b int) int { return a + b }).
		ToSlice()
	if d := cmp.Diff([]int{10, 21}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
	// Two pairs consumed, so at most three pulls from the long side.
	if pulls := c.Pulls(); pulls > 3 {
		t.Errorf("Zip pulled %d from the long side, want <= 3", pulls)
	}
}
