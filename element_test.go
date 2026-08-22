package goq_test

import (
	"errors"
	"testing"

	"github.com/oleexo/goq"
	"github.com/oleexo/goq/internal/seqcore"
)

func TestFirst(t *testing.T) {
	t.Parallel()
	if v, ok := goq.From([]int{7, 8}).First(); !ok || v != 7 {
		t.Errorf("First() = (%v, %v), want (7, true)", v, ok)
	}
	if v, ok := goq.From([]int{}).First(); ok || v != 0 {
		t.Errorf("First() on empty = (%v, %v), want (0, false)", v, ok)
	}
}

// First must stop pulling as soon as it has an element.
func TestFirstIsLazy(t *testing.T) {
	t.Parallel()
	c := &seqcore.Counter{}
	if _, ok := goq.FromSeq(c.Seq(1000)).First(); !ok {
		t.Fatal("First() reported empty")
	}
	if got := c.Pulls(); got != 1 {
		t.Errorf("First pulled %d elements, want 1", got)
	}
}

func TestLast(t *testing.T) {
	t.Parallel()
	if v, ok := goq.From([]int{1, 2, 3}).Last(); !ok || v != 3 {
		t.Errorf("Last() = (%v, %v), want (3, true)", v, ok)
	}
	if _, ok := goq.Empty[int]().Last(); ok {
		t.Error("Last() on empty reported ok")
	}
}

// Single distinguishes empty from multiple; that is its whole purpose.
func TestSingle(t *testing.T) {
	t.Parallel()
	if v, err := goq.From([]int{42}).Single(); err != nil || v != 42 {
		t.Errorf("Single() = (%v, %v), want (42, nil)", v, err)
	}
	if _, err := goq.Empty[int]().Single(); !errors.Is(err, goq.ErrEmpty) {
		t.Errorf("Single() on empty err = %v, want ErrEmpty", err)
	}
	if _, err := goq.From([]int{1, 2}).Single(); !errors.Is(err, goq.ErrMultiple) {
		t.Errorf("Single() on two err = %v, want ErrMultiple", err)
	}
}

// Single must not drain a long source to discover it has more than one element.
func TestSingleStopsAtTwo(t *testing.T) {
	t.Parallel()
	c := &seqcore.Counter{}
	if _, err := goq.FromSeq(c.Seq(1000)).Single(); !errors.Is(err, goq.ErrMultiple) {
		t.Fatalf("err = %v, want ErrMultiple", err)
	}
	if got := c.Pulls(); got != 2 {
		t.Errorf("Single pulled %d elements, want 2", got)
	}
}

func TestElementAt(t *testing.T) {
	t.Parallel()
	if v, ok := goq.From([]string{"a", "b", "c"}).ElementAt(1); !ok || v != "b" {
		t.Errorf("ElementAt(1) = (%v, %v), want (b, true)", v, ok)
	}
	if _, ok := goq.From([]string{"a"}).ElementAt(5); ok {
		t.Error("ElementAt past end reported ok")
	}
	if _, ok := goq.From([]string{"a"}).ElementAt(-1); ok {
		t.Error("ElementAt(-1) reported ok")
	}
}

func TestAnyAll(t *testing.T) {
	t.Parallel()
	if !goq.From([]int{1}).Any() {
		t.Error("Any() on non-empty = false")
	}
	if goq.Empty[int]().Any() {
		t.Error("Any() on empty = true")
	}
	if !goq.From([]int{2, 4}).AnyWhere(func(i int) bool { return i == 4 }) {
		t.Error("AnyWhere failed to find 4")
	}
	if !goq.From([]int{2, 4}).All(func(i int) bool { return i%2 == 0 }) {
		t.Error("All(even) = false")
	}
	if goq.From([]int{2, 5}).All(func(i int) bool { return i%2 == 0 }) {
		t.Error("All(even) = true for odd member")
	}
	// Vacuous truth, matching C#.
	if !goq.Empty[int]().All(func(int) bool { return false }) {
		t.Error("All on empty = false, want true")
	}
}

func TestAnyWhereShortCircuits(t *testing.T) {
	t.Parallel()
	c := &seqcore.Counter{}
	if !goq.FromSeq(c.Seq(1000)).AnyWhere(func(i int) bool { return i == 0 }) {
		t.Fatal("AnyWhere = false")
	}
	if got := c.Pulls(); got != 1 {
		t.Errorf("AnyWhere pulled %d, want 1", got)
	}
}

func TestContainsAndSequenceEqual(t *testing.T) {
	t.Parallel()
	if !goq.Contains(goq.From([]int{1, 2, 3}), 2) {
		t.Error("Contains(2) = false")
	}
	if goq.Contains(goq.From([]int{1}), 9) {
		t.Error("Contains(9) = true")
	}
	if !goq.SequenceEqual(goq.From([]int{1, 2}), goq.From([]int{1, 2})) {
		t.Error("SequenceEqual on equal = false")
	}
	if goq.SequenceEqual(goq.From([]int{1, 2}), goq.From([]int{1})) {
		t.Error("SequenceEqual on differing lengths = true")
	}
	if goq.SequenceEqual(goq.From([]int{1}), goq.From([]int{1, 2})) {
		t.Error("SequenceEqual on differing lengths (reversed) = true")
	}
}
