package goq_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/oleexo/goq"
)

type emp struct {
	Name string
	Dept string
	Age  int
}

func staff() []emp {
	return []emp{
		{"Cai", "ops", 41}, {"Ann", "eng", 34},
		{"Dee", "ops", 22}, {"Bob", "eng", 34},
	}
}

func TestOrderByThenBy(t *testing.T) {
	t.Parallel()
	got := goq.From(staff()).
		OrderBy(func(e emp) string { return e.Dept }).
		ThenByDesc(func(e emp) int { return e.Age }).
		ThenBy(func(e emp) string { return e.Name }).
		Select(func(e emp) string { return e.Name }).
		ToSlice()
	// eng before ops; within eng, age 34 twice so Name breaks the tie.
	if d := cmp.Diff([]string{"Ann", "Bob", "Cai", "Dee"}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestOrderByDesc(t *testing.T) {
	t.Parallel()
	got := goq.From([]int{3, 1, 2}).OrderByDesc(func(i int) int { return i }).ToSlice()
	if d := cmp.Diff([]int{3, 2, 1}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

// Sorting must be stable: equal keys keep source order.
func TestOrderByIsStable(t *testing.T) {
	t.Parallel()
	src := []emp{{"first", "x", 1}, {"second", "x", 1}, {"third", "x", 1}}
	got := goq.From(src).
		OrderBy(func(e emp) int { return e.Age }).
		Select(func(e emp) string { return e.Name }).
		ToSlice()
	if d := cmp.Diff([]string{"first", "second", "third"}, got); d != "" {
		t.Errorf("unstable sort (-want +got):\n%s", d)
	}
}

func TestOrderedQueryAsQueryReEnters(t *testing.T) {
	t.Parallel()
	got := goq.From([]int{5, 1, 3}).
		OrderBy(func(i int) int { return i }).
		AsQuery().
		Where(func(i int) bool { return i > 1 }).
		ToSlice()
	if d := cmp.Diff([]int{3, 5}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestOrderByFunc(t *testing.T) {
	t.Parallel()
	got := goq.From([]string{"bb", "a", "ccc"}).
		OrderByFunc(func(a, b string) int { return len(a) - len(b) }).
		ToSlice()
	if d := cmp.Diff([]string{"a", "bb", "ccc"}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestReverse(t *testing.T) {
	t.Parallel()
	if d := cmp.Diff([]int{3, 2, 1}, goq.From([]int{1, 2, 3}).Reverse().ToSlice()); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
	if got := goq.Empty[int]().Reverse().ToSlice(); len(got) != 0 {
		t.Errorf("Reverse on empty = %v", got)
	}
}

// Ordering is a buffering operator: it must consume the entire source before
// yielding the first element.
func TestOrderByConsumesWholeSourceBeforeYielding(t *testing.T) {
	t.Parallel()
	pulled := 0
	src := func(yield func(int) bool) {
		for i := range 10 {
			pulled++
			if !yield(i) {
				return
			}
		}
	}
	q := goq.FromSeq(src).OrderBy(func(i int) int { return i })
	if _, ok := q.AsQuery().First(); !ok {
		t.Fatal("First reported empty")
	}
	if pulled != 10 {
		t.Errorf("pulled %d before first element, want 10 (ordering must buffer)", pulled)
	}
}
