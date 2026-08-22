package seqcore_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/oleexo/goq/internal/seqcore"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestMap(t *testing.T) {
	t.Parallel()
	got := slices.Collect(seqcore.Map(slices.Values([]int{1, 2, 3}), func(i int) int { return i * 2 }))
	if d := cmp.Diff([]int{2, 4, 6}, got); d != "" {
		t.Errorf("Map mismatch (-want +got):\n%s", d)
	}
}

func TestFilter(t *testing.T) {
	t.Parallel()
	got := slices.Collect(seqcore.Filter(slices.Values([]int{1, 2, 3, 4}), func(i int) bool { return i%2 == 0 }))
	if d := cmp.Diff([]int{2, 4}, got); d != "" {
		t.Errorf("Filter mismatch (-want +got):\n%s", d)
	}
}

// TestMapIsLazy is the pattern every later operator task reuses: the counting
// source proves the operator pulls no more than the consumer demands.
func TestMapIsLazy(t *testing.T) {
	t.Parallel()
	c := &seqcore.Counter{}
	mapped := seqcore.Map(c.Seq(100), func(i int) int { return i })
	for range mapped { // take exactly one, then stop
		break
	}
	if got := c.Pulls(); got != 1 {
		t.Errorf("Map pulled %d elements, want 1 — operator is not lazy", got)
	}
}

func TestFilterTerminatesOnInfiniteSource(t *testing.T) {
	t.Parallel()
	// If Filter were eager this test would hang rather than fail.
	for v := range seqcore.Filter(seqcore.Infinite(), func(i int) bool { return i > 5 }) {
		if v != 6 {
			t.Errorf("got %d, want 6", v)
		}
		break
	}
}
