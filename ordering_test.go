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

// orderingFixture is deliberately built so that the tie-breakers matter:
// sorting by Dept alone, or by Name alone, each gives a DIFFERENT answer than
// the full Dept → Age-desc → Name chain. That is what makes this test able to
// fail if ThenBy stops appending.
func orderingFixture() []emp {
	return []emp{
		{"Zoe", "eng", 30},
		{"Ann", "eng", 40},
		{"Dee", "ops", 20},
		{"Bob", "ops", 20},
	}
}

func TestOrderByThenBy(t *testing.T) {
	t.Parallel()
	got := goq.From(orderingFixture()).
		OrderBy(func(e emp) string { return e.Dept }).
		ThenByDesc(func(e emp) int { return e.Age }).
		ThenBy(func(e emp) string { return e.Name }).
		Select(func(e emp) string { return e.Name }).
		ToSlice()
	// eng before ops; within eng, Age desc puts Ann(40) before Zoe(30);
	// within ops, Age ties at 20 so Name breaks it: Bob before Dee.
	//
	// This expectation discriminates: ThenBy as a no-op gives
	// [Zoe Ann Dee Bob], and ThenBy resetting gives [Ann Bob Dee Zoe].
	if d := cmp.Diff([]string{"Ann", "Zoe", "Bob", "Dee"}, got); d != "" {
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

// TestOrderByIsStable needs enough elements, and enough interleaved keys, to
// force pdqsort to actually partition. A small all-equal-key fixture is
// already in sorted order, so Go's unstable sort short-circuits and preserves
// it — measured: 3 and 50 elements with one key do NOT discriminate, while 64
// elements across 4 keys do.
func TestOrderByIsStable(t *testing.T) {
	t.Parallel()
	type item struct{ Key, Seq int }

	const (
		n    = 64
		keys = 4
	)
	src := make([]item, 0, n)
	for i := range n {
		src = append(src, item{Key: i % keys, Seq: i})
	}

	sorted := goq.From(src).OrderBy(func(x item) int { return x.Key }).ToSlice()

	if len(sorted) != n {
		t.Fatalf("got %d elements, want %d", len(sorted), n)
	}
	// Within each key group, original sequence numbers must stay ascending.
	// An unstable sort permutes them; a stable one cannot.
	lastSeq := make(map[int]int, keys)
	for i, x := range sorted {
		if prev, seen := lastSeq[x.Key]; seen && x.Seq < prev {
			t.Errorf("unstable sort at index %d: key %d saw Seq %d after %d",
				i, x.Key, x.Seq, prev)
		}
		lastSeq[x.Key] = x.Seq
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

// The zero OrderedQuery must behave as empty across every operator and
// terminal, not merely the first one anyone happens to call.
func TestZeroValueOrderedQueryOperatorsDoNotPanic(t *testing.T) {
	t.Parallel()
	var o goq.OrderedQuery[int]

	for range o.Seq() {
		t.Error("Seq yielded an element from the zero value")
	}
	if got := o.ToSlice(); len(got) != 0 {
		t.Errorf("ToSlice = %v, want empty", got)
	}
	if got := o.AsQuery().ToSlice(); len(got) != 0 {
		t.Errorf("AsQuery.ToSlice = %v, want empty", got)
	}
	if got := o.ThenBy(func(i int) int { return i }).ToSlice(); len(got) != 0 {
		t.Errorf("ThenBy.ToSlice = %v, want empty", got)
	}
	if got := o.ThenByDesc(func(i int) int { return i }).ToSlice(); len(got) != 0 {
		t.Errorf("ThenByDesc.ToSlice = %v, want empty", got)
	}
	if got := o.Select(func(i int) int { return i * 2 }).ToSlice(); len(got) != 0 {
		t.Errorf("Select.ToSlice = %v, want empty", got)
	}
}
