package goq_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/oleexo/goq"
	"github.com/oleexo/goq/internal/seqcore"
)

func TestFilteringOperators(t *testing.T) {
	t.Parallel()
	src := []int{1, 2, 3, 4, 5, 6}
	tests := []struct {
		name string
		got  []int
		want []int
	}{
		{"Where even", goq.From(src).Where(func(i int) bool { return i%2 == 0 }).ToSlice(), []int{2, 4, 6}},
		{"Where none", goq.From(src).Where(func(int) bool { return false }).ToSlice(), []int{}},
		{"Take 2", goq.From(src).Take(2).ToSlice(), []int{1, 2}},
		{"Take 0", goq.From(src).Take(0).ToSlice(), []int{}},
		{"Take negative", goq.From(src).Take(-1).ToSlice(), []int{}},
		{"Take past end", goq.From(src).Take(99).ToSlice(), src},
		{"TakeWhile", goq.From(src).TakeWhile(func(i int) bool { return i < 4 }).ToSlice(), []int{1, 2, 3}},
		{"TakeLast 2", goq.From(src).TakeLast(2).ToSlice(), []int{5, 6}},
		{"TakeLast past end", goq.From(src).TakeLast(99).ToSlice(), src},
		{"Skip 4", goq.From(src).Skip(4).ToSlice(), []int{5, 6}},
		{"Skip past end", goq.From(src).Skip(99).ToSlice(), []int{}},
		{"SkipWhile", goq.From(src).SkipWhile(func(i int) bool { return i < 4 }).ToSlice(), []int{4, 5, 6}},
		{"SkipLast 2", goq.From(src).SkipLast(2).ToSlice(), []int{1, 2, 3, 4}},
		{"SkipLast past end", goq.From(src).SkipLast(99).ToSlice(), []int{}},
		{"empty source", goq.Empty[int]().Where(func(int) bool { return true }).ToSlice(), []int{}},
		{"chained", goq.From(src).Where(func(i int) bool { return i > 2 }).Take(2).ToSlice(), []int{3, 4}},
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

// Take must not pull more than it needs, and must terminate on an infinite
// source. If this hangs rather than fails, Take is eager.
func TestTakeIsLazyAndBounded(t *testing.T) {
	t.Parallel()
	if d := cmp.Diff([]int{0, 1, 2}, goq.FromSeq(seqcore.Infinite()).Take(3).ToSlice()); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
	c := &seqcore.Counter{}
	goq.FromSeq(c.Seq(1000)).Take(3).ToSlice()
	if got := c.Pulls(); got != 3 {
		t.Errorf("Take(3) pulled %d, want 3", got)
	}
}

// TakeWhile must stop at the first failure, not scan the whole source.
func TestTakeWhileStopsEarly(t *testing.T) {
	t.Parallel()
	c := &seqcore.Counter{}
	goq.FromSeq(c.Seq(1000)).TakeWhile(func(i int) bool { return i < 2 }).ToSlice()
	if got := c.Pulls(); got != 3 { // 0, 1 pass; 2 fails and stops
		t.Errorf("TakeWhile pulled %d, want 3", got)
	}
}

func TestTakeLastTerminatesOnFiniteOnly(t *testing.T) {
	t.Parallel()
	// TakeLast is a bounded-buffer operator: it must read to the end, but must
	// retain only n elements. Correctness here is the retained window.
	if d := cmp.Diff([]int{98, 99}, goq.Range(0, 100).TakeLast(2).ToSlice()); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

// TakeLast and SkipLast promise O(n) memory regardless of source length. That
// promise is invisible to an output-equality test: a "collect everything, then
// slice" implementation would satisfy every other test in this file. This
// asserts the promise directly via allocated bytes, which separates the two
// shapes by several orders of magnitude (measured: ~0 B/op bounded vs ~8 MB/op
// for collect-then-slice over 200k elements).
//
// It deliberately does not call t.Parallel: testing.Benchmark drives its own
// iteration count and should not compete with other parallel tests.
func TestTakeLastAndSkipLastAreMemoryBounded(t *testing.T) {
	const (
		sourceLen  = 100_000
		maxBytesOp = 64 << 10 // 64 KiB
	)

	// Consume through Seq rather than ToSlice: SkipLast yields almost the whole
	// source, and materialising that would dwarf the buffer we are measuring.
	drain := func(q goq.Query[int]) {
		for v := range q.Seq() {
			_ = v // consume all elements
		}
	}

	for _, tc := range []struct {
		name string
		run  func()
	}{
		{"TakeLast", func() { drain(goq.Range(0, sourceLen).TakeLast(2)) }},
		{"SkipLast", func() { drain(goq.Range(0, sourceLen).SkipLast(2)) }},
	} {
		res := testing.Benchmark(func(b *testing.B) {
			b.Helper()
			for range b.N {
				tc.run()
			}
		})
		if got := res.AllocedBytesPerOp(); got > maxBytesOp {
			t.Errorf("%s allocated %d B/op over a %d-element source, want <= %d — "+
				"the operator is buffering the whole source instead of a bounded window",
				tc.name, got, sourceLen, maxBytesOp)
		}
	}
}
