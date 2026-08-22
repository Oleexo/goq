package goq_test

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/oleexo/goq"
	"github.com/oleexo/goq/internal/seqcore"
)

func TestToMapRejectsDuplicates(t *testing.T) {
	t.Parallel()
	type rec struct {
		ID   int
		Name string
	}
	unique := []rec{{1, "a"}, {2, "b"}}
	got, err := goq.From(unique).ToMap(func(r rec) int { return r.ID })
	if err != nil {
		t.Fatalf("ToMap err = %v, want nil", err)
	}
	if len(got) != 2 || got[1].Name != "a" {
		t.Errorf("ToMap = %v", got)
	}

	dupes := []rec{{1, "a"}, {1, "b"}}
	m, err := goq.From(dupes).ToMap(func(r rec) int { return r.ID })
	if !errors.Is(err, goq.ErrDuplicateKey) {
		t.Errorf("err = %v, want ErrDuplicateKey", err)
	}
	if m != nil {
		t.Errorf("ToMap returned %v on error, want nil", m)
	}

	// ToMapLast overwrites on purpose.
	last := goq.From(dupes).ToMapLast(func(r rec) int { return r.ID })
	if last[1].Name != "b" {
		t.Errorf("ToMapLast kept %q, want b", last[1].Name)
	}
}

func TestToSet(t *testing.T) {
	t.Parallel()
	got := goq.ToSet(goq.From([]int{1, 2, 2, 3}))
	if len(got) != 3 {
		t.Errorf("ToSet size = %d, want 3", len(got))
	}
	if _, ok := got[2]; !ok {
		t.Error("ToSet missing 2")
	}
}

// Memoize makes a single-shot or expensive source re-enumerable, and pulls the
// underlying source exactly once no matter how many times it is enumerated.
func TestMemoize(t *testing.T) {
	t.Parallel()
	c := &seqcore.Counter{}
	q := goq.FromSeq(c.Seq(3)).Memoize()
	first, second := q.ToSlice(), q.ToSlice()
	if d := cmp.Diff(first, second); d != "" {
		t.Errorf("enumerations differ (-first +second):\n%s", d)
	}
	if d := cmp.Diff([]int{0, 1, 2}, first); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
	if got := c.Pulls(); got != 3 {
		t.Errorf("source pulled %d times, want 3 — Memoize re-ran the source", got)
	}
}

// Memoize is eager by necessity: it must read the whole source to cache it.
func TestMemoizeIsIdempotentUnderConcurrentUse(t *testing.T) {
	t.Parallel()
	q := goq.Range(0, 100).Memoize()
	done := make(chan []int, 2)
	for range 2 {
		go func() { done <- q.ToSlice() }()
	}
	a, b := <-done, <-done
	if d := cmp.Diff(a, b); d != "" {
		t.Errorf("concurrent enumerations differ (-a +b):\n%s", d)
	}
}
