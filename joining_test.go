package goq_test

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/oleexo/goq"
	"github.com/oleexo/goq/internal/seqcore"
)

type dept struct {
	Code string
	Name string
}

func TestJoinIsInner(t *testing.T) {
	t.Parallel()
	depts := []dept{{"eng", "Engineering"}, {"ops", "Operations"}, {"zzz", "Unused"}}
	people := []emp{{"Ann", "eng", 34}, {"Cai", "ops", 41}, {"Xan", "nope", 20}}

	got := goq.From(people).Join(
		goq.From(depts),
		func(e emp) string { return e.Dept },
		func(d dept) string { return d.Code },
		func(e emp, d dept) string { return e.Name + "@" + d.Name },
	).ToSlice()

	// Xan has no matching dept and zzz has no people: both are dropped.
	if d := cmp.Diff([]string{"Ann@Engineering", "Cai@Operations"}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

// One outer element matching several inner elements yields one result per pair.
func TestJoinProducesCartesianPerKey(t *testing.T) {
	t.Parallel()
	got := goq.From([]dept{{"eng", "Engineering"}}).Join(
		goq.From([]emp{{"Ann", "eng", 34}, {"Bob", "eng", 34}}),
		func(d dept) string { return d.Code },
		func(e emp) string { return e.Dept },
		func(_ dept, e emp) string { return e.Name },
	).ToSlice()
	if d := cmp.Diff([]string{"Ann", "Bob"}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

// GroupJoin is a left outer join: unmatched outer elements survive with an
// empty slice.
func TestGroupJoinKeepsUnmatchedOuter(t *testing.T) {
	t.Parallel()
	depts := []dept{{"eng", "Engineering"}, {"zzz", "Unused"}}
	people := []emp{{"Ann", "eng", 34}, {"Bob", "eng", 34}}

	got := goq.From(depts).GroupJoin(
		goq.From(people),
		func(d dept) string { return d.Code },
		func(e emp) string { return e.Dept },
		func(d dept, es []emp) string { return fmt.Sprintf("%s:%d", d.Code, len(es)) },
	).ToSlice()

	if d := cmp.Diff([]string{"eng:2", "zzz:0"}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestJoinEmptyInputs(t *testing.T) {
	t.Parallel()
	id := func(i int) int { return i }
	if got := goq.Empty[int]().Join(goq.From([]int{1}), id, id,
		func(a, _ int) int { return a }).ToSlice(); len(got) != 0 {
		t.Errorf("empty outer = %v, want empty", got)
	}
	if got := goq.From([]int{1}).Join(goq.Empty[int](), id, id,
		func(a, _ int) int { return a }).ToSlice(); len(got) != 0 {
		t.Errorf("empty inner = %v, want empty", got)
	}
}

// TestJoinStreamsOuter proves the outer side streams by counting pulls, not by
// comparing output. An implementation that buffered the outer first would
// produce the same result slice, so output equality cannot detect it — it
// would, however, pull all 1,000,000 elements.
func TestJoinStreamsOuter(t *testing.T) {
	t.Parallel()
	id := func(i int) int { return i }
	c := &seqcore.Counter{}

	got := goq.FromSeq(c.Seq(1_000_000)).
		Join(goq.From([]int{5}), id, id, func(a, _ int) int { return a }).
		Take(1).
		ToSlice()

	if d := cmp.Diff([]int{5}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
	// Outer elements 0..4 find no match against an inner of [5]; the sixth pull
	// yields, and Take(1) then stops the pipeline. A buffered outer would pull
	// all 1,000,000.
	if pulls := c.Pulls(); pulls != 6 {
		t.Errorf("outer pulled %d elements, want 6 — the outer side is not streaming", pulls)
	}
}
