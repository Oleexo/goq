package goq_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/oleexo/goq"
	"github.com/oleexo/goq/internal/seqcore"
)

func TestConcat(t *testing.T) {
	t.Parallel()
	got := goq.From([]int{1, 2}).Concat(goq.From([]int{2, 3})).ToSlice()
	if d := cmp.Diff([]int{1, 2, 2, 3}, got); d != "" {
		t.Errorf("Concat must not deduplicate (-want +got):\n%s", d)
	}
}

func TestSetOperators(t *testing.T) {
	t.Parallel()
	a := goq.From([]int{1, 2, 2, 3})
	b := goq.From([]int{3, 4})
	tests := []struct {
		name string
		got  []int
		want []int
	}{
		{"Distinct", goq.Distinct(a).ToSlice(), []int{1, 2, 3}},
		{"Union", goq.Union(a, b).ToSlice(), []int{1, 2, 3, 4}},
		{"Intersect", goq.Intersect(a, b).ToSlice(), []int{3}},
		{"Intersect dedupes receiver", goq.Intersect(goq.From([]int{1, 2, 2, 3}), goq.From([]int{2})).ToSlice(), []int{2}},
		{"Except", goq.Except(a, b).ToSlice(), []int{1, 2}},
		{"Intersect empty", goq.Intersect(a, goq.Empty[int]()).ToSlice(), []int{}},
		{"Except empty", goq.Except(a, goq.Empty[int]()).ToSlice(), []int{1, 2, 3}},
		{"Except everything", goq.Except(a, a).ToSlice(), []int{}},
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

// All set operators yield distinct results in first-appearance order.
func TestSetOperatorsDeduplicateAndPreserveOrder(t *testing.T) {
	t.Parallel()
	got := goq.Union(goq.From([]int{3, 1, 3}), goq.From([]int{1, 2})).ToSlice()
	if d := cmp.Diff([]int{3, 1, 2}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestByVariantsUseKeys(t *testing.T) {
	t.Parallel()
	people := []emp{{"Ann", "eng", 34}, {"Bob", "eng", 28}, {"Cai", "ops", 41}}
	byDept := func(e emp) string { return e.Dept }

	// DistinctBy keeps the first element per key.
	got := goq.From(people).DistinctBy(byDept).Select(func(e emp) string { return e.Name }).ToSlice()
	if d := cmp.Diff([]string{"Ann", "Cai"}, got); d != "" {
		t.Errorf("DistinctBy mismatch (-want +got):\n%s", d)
	}

	other := []emp{{"Dee", "ops", 22}}
	ex := goq.From(people).ExceptBy(goq.From(other), byDept).
		Select(func(e emp) string { return e.Name }).ToSlice()
	if d := cmp.Diff([]string{"Ann"}, ex); d != "" {
		t.Errorf("ExceptBy mismatch (-want +got):\n%s", d)
	}

	in := goq.From(people).IntersectBy(goq.From(other), byDept).
		Select(func(e emp) string { return e.Name }).ToSlice()
	if d := cmp.Diff([]string{"Cai"}, in); d != "" {
		t.Errorf("IntersectBy mismatch (-want +got):\n%s", d)
	}

	un := goq.From(people[:1]).UnionBy(goq.From(other), byDept).
		Select(func(e emp) string { return e.Name }).ToSlice()
	if d := cmp.Diff([]string{"Ann", "Dee"}, un); d != "" {
		t.Errorf("UnionBy mismatch (-want +got):\n%s", d)
	}
}

// Distinct streams: it must not buffer the source, only the keys seen so far.
func TestDistinctStreams(t *testing.T) {
	t.Parallel()
	c := &seqcore.Counter{}
	got := goq.Distinct(goq.FromSeq(c.Seq(1_000_000))).Take(3).ToSlice()
	if d := cmp.Diff([]int{0, 1, 2}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
	if pulls := c.Pulls(); pulls != 3 {
		t.Errorf("Distinct pulled %d, want 3 — it is buffering", pulls)
	}
}

// Intersect and Except buffer the ARGUMENT, then stream the receiver.
func TestIntersectStreamsReceiver(t *testing.T) {
	t.Parallel()
	c := &seqcore.Counter{}
	got := goq.Intersect(goq.FromSeq(c.Seq(1_000_000)), goq.From([]int{2, 4})).Take(1).ToSlice()
	if d := cmp.Diff([]int{2}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
	// Pulls 0,1,2 then yields 2 and the consumer stops.
	if pulls := c.Pulls(); pulls != 3 {
		t.Errorf("Intersect pulled %d from the receiver, want 3", pulls)
	}
}
