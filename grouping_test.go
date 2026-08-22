package goq_test

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/oleexo/goq"
)

func staff() []emp {
	return []emp{
		{"Cai", "ops", 41}, {"Ann", "eng", 34},
		{"Dee", "ops", 22}, {"Bob", "eng", 34},
	}
}

func TestGroupByPreservesFirstAppearanceOrder(t *testing.T) {
	t.Parallel()
	got := goq.From(staff()).
		GroupBy(func(e emp) string { return e.Dept }).
		Select(func(g goq.Group[string, emp]) string {
			return fmt.Sprintf("%s:%d", g.Key, len(g.Items))
		}).
		ToSlice()
	// staff() order is ops, eng, ops, eng -> ops seen first.
	if d := cmp.Diff([]string{"ops:2", "eng:2"}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

// The chain that the satellite composition law makes possible: group, filter
// groups, order groups, then project back into a normal Query.
func TestGroupByWhereOrderBySelect(t *testing.T) {
	t.Parallel()
	got := goq.From(staff()).
		GroupBy(func(e emp) string { return e.Dept }).
		Where(func(g goq.Group[string, emp]) bool { return len(g.Items) > 1 }).
		OrderBy(func(g goq.Group[string, emp]) string { return g.Key }).
		Select(func(g goq.Group[string, emp]) string { return g.Key }).
		Select(func(s string) string { return "[" + s + "]" }).
		ToSlice()
	if d := cmp.Diff([]string{"[eng]", "[ops]"}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

// groupOrderingFixture has groups of DIFFERENT sizes so that OrderByDesc
// actually discriminates: ops=2, eng=2, hr=1. Descending by size puts the
// two 2s first (tie, broken by Key) and hr last; ascending would put hr
// first, so a wrong direction changes the result.
func groupOrderingFixture() []emp {
	return []emp{
		{"Cai", "ops", 41},
		{"Ann", "eng", 34},
		{"Dee", "ops", 22},
		{"Bob", "eng", 34},
		{"Eve", "hr", 28},
	}
}

func TestGroupQueryOrderByDescThenBy(t *testing.T) {
	t.Parallel()
	got := goq.From(groupOrderingFixture()).
		GroupBy(func(e emp) string { return e.Dept }).
		OrderByDesc(func(g goq.Group[string, emp]) int { return len(g.Items) }).
		ThenBy(func(g goq.Group[string, emp]) string { return g.Key }).
		Select(func(g goq.Group[string, emp]) string { return g.Key }).
		ToSlice()
	// ops=2, eng=2, hr=1. Descending by size puts both 2s first (tie broken by Key alphabetically),
	// then hr=1 last. Expected: [eng, ops, hr].
	if d := cmp.Diff([]string{"eng", "ops", "hr"}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestGroupBySelect(t *testing.T) {
	t.Parallel()
	got := goq.From(staff()).
		GroupBySelect(
			func(e emp) string { return e.Dept },
			func(g goq.Group[string, emp]) int { return len(g.Items) },
		).
		ToSlice()
	if d := cmp.Diff([]int{2, 2}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestToLookup(t *testing.T) {
	t.Parallel()
	got := goq.From(staff()).ToLookup(func(e emp) string { return e.Dept })
	if len(got) != 2 || len(got["eng"]) != 2 {
		t.Errorf("ToLookup = %v", got)
	}
}

// Nested grouping is only expressible as a free function; the method form is
// an instantiation cycle.
func TestNestedGroupByViaFreeFunction(t *testing.T) {
	t.Parallel()
	inner := goq.From(staff()).GroupBy(func(e emp) string { return e.Dept })
	outer := goq.GroupBy(inner, func(g goq.Group[string, emp]) int { return len(g.Items) })
	got := outer.ToSlice()
	if len(got) != 1 {
		t.Fatalf("expected 1 outer group (both depts have 2 members), got %d", len(got))
	}
	if got[0].Key != 2 || len(got[0].Items) != 2 {
		t.Errorf("outer group = key %d with %d inner groups, want key 2 with 2",
			got[0].Key, len(got[0].Items))
	}
	// Verify inner groups are in first-appearance order: ops seen first in staff()
	if got[0].Items[0].Key != "ops" {
		t.Errorf("first inner group key = %q, want ops", got[0].Items[0].Key)
	}
	if got[0].Items[1].Key != "eng" {
		t.Errorf("second inner group key = %q, want eng", got[0].Items[1].Key)
	}
}

func TestGroupByEmptySource(t *testing.T) {
	t.Parallel()
	got := goq.Empty[emp]().GroupBy(func(e emp) string { return e.Dept }).ToSlice()
	if len(got) != 0 {
		t.Errorf("groups = %v, want empty", got)
	}
}

func TestGroupItemsAreInSourceOrder(t *testing.T) {
	t.Parallel()
	got := goq.From(staff()).
		GroupBy(func(e emp) string { return e.Dept }).
		ToSlice()
	var eng goq.Group[string, emp]
	for _, g := range got {
		if g.Key == "eng" {
			eng = g
		}
	}
	names := make([]string, 0, len(eng.Items))
	for _, e := range eng.Items {
		names = append(names, e.Name)
	}
	if d := cmp.Diff([]string{"Ann", "Bob"}, names, cmpopts.EquateEmpty()); d != "" {
		t.Errorf("group items out of source order (-want +got):\n%s", d)
	}
}
