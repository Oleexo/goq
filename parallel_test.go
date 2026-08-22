package goq_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/oleexo/goq"
)

func TestParallelUnorderedProducesEveryElement(t *testing.T) {
	t.Parallel()
	got, err := goq.Range(0, 100).
		AsParallel(goq.Workers(8)).
		Select(func(i int) int { return i * i }).
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := make([]int, 0, 100)
	for i := range 100 {
		want = append(want, i*i)
	}
	// Unordered: compare as a multiset.
	if d := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b int) bool { return a < b })); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestParallelRunsConcurrently(t *testing.T) {
	t.Parallel()
	var inFlight, peak atomic.Int64
	_, err := goq.Range(0, 32).
		AsParallel(goq.Workers(4)).
		SelectCtx(func(_ context.Context, i int) (int, error) {
			n := inFlight.Add(1)
			for {
				old := peak.Load()
				if n <= old || peak.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			inFlight.Add(-1)
			return i, nil
		}).
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if peak.Load() < 2 {
		t.Errorf("peak concurrency = %d, want at least 2 — work is not parallel", peak.Load())
	}
	if peak.Load() > 4 {
		t.Errorf("peak concurrency = %d, want at most 4 — worker cap not honoured", peak.Load())
	}
}

func TestParallelErrorShortCircuits(t *testing.T) {
	t.Parallel()
	got, err := goq.Range(0, 200).
		AsParallel(goq.Workers(4)).
		SelectErr(func(i int) (int, error) {
			if i == 7 {
				return 0, errBoom
			}
			time.Sleep(time.Millisecond)
			return i, nil
		}).
		ToSlice(context.Background())
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want errBoom", err)
	}
	if got != nil {
		t.Errorf("got %v on error, want nil", got)
	}
}

// The bug found during design: on cancellation, workers exit silently, the
// output channel closes normally, and a naive terminal returns a short slice
// with a nil error. This test is the regression guard.
func TestParallelCancellationIsNotSilentTruncation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	got, err := goq.Range(0, 500).
		AsParallel(goq.Workers(4)).
		SelectCtx(func(_ context.Context, i int) (int, error) {
			time.Sleep(5 * time.Millisecond)
			return i, nil
		}).
		ToSlice(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded — cancellation was reported as success", err)
	}
	if got != nil {
		t.Errorf("got %v on cancellation, want nil", got)
	}
}

// A panicking callback must not kill the process from a worker goroutine: it
// must surface on the caller's goroutine, where recover can see it.
func TestParallelPanicSurfacesOnCallerGoroutine(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("no panic reached the caller")
		}
		pv, ok := r.(goq.PanicValue)
		if !ok {
			t.Fatalf("recovered %T, want goq.PanicValue", r)
		}
		if pv.Value != "worker exploded" {
			t.Errorf("Value = %v, want \"worker exploded\"", pv.Value)
		}
		if len(pv.Stack) == 0 {
			t.Error("Stack is empty; the worker stack was not captured")
		}
	}()

	_, _ = goq.Range(0, 50).
		AsParallel(goq.Workers(4)).
		Select(func(i int) int {
			if i == 3 {
				panic("worker exploded")
			}
			return i
		}).
		ToSlice(context.Background())
	t.Fatal("ToSlice returned instead of panicking")
}

func TestParallelWhere(t *testing.T) {
	t.Parallel()
	got, err := goq.Range(0, 20).
		AsParallel(goq.Workers(4)).
		Where(func(i int) bool { return i%2 == 0 }).
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	slices.Sort(got)
	want := []int{0, 2, 4, 6, 8, 10, 12, 14, 16, 18}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

// TestParallelSelectMany asserts as a MULTISET, since unordered mode is the
// default and does not promise batch order. TestAsOrderedSelectMany adds the
// order-preserving assertion.
func TestParallelSelectMany(t *testing.T) {
	t.Parallel()
	got, err := goq.Range(1, 4).
		AsParallel(goq.Workers(4)).
		SelectMany(func(i int) []int { return []int{i, i} }).
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	slices.Sort(got)
	if d := cmp.Diff([]int{1, 1, 2, 2, 3, 3, 4, 4}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

// AsSequential is the visible barrier back into an ordered, sequential pipeline.
func TestAsSequentialReEntersTryQuery(t *testing.T) {
	t.Parallel()
	got, err := goq.Range(0, 10).
		AsParallel(goq.Workers(4)).
		Select(func(i int) int { return i * 2 }).
		AsSequential().
		Take(3).
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
}

// Consumer abandoning the pipeline must not leak workers. goleak in TestMain is
// what fails if it does.
func TestParallelEarlyBreakDoesNotLeak(t *testing.T) {
	t.Parallel()
	p := goq.Range(0, 10_000).
		AsParallel(goq.Workers(8)).
		Select(func(i int) int { return i })
	n := 0
	for _, err := range p.AsSequential().Seq(context.Background()) {
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		n++
		if n == 5 {
			break
		}
	}
	if n != 5 {
		t.Errorf("consumed %d, want 5", n)
	}
	// Give any leaked goroutine a chance to be observed by goleak at exit.
	time.Sleep(50 * time.Millisecond)
}

func TestParallelEmptySource(t *testing.T) {
	t.Parallel()
	got, err := goq.Empty[int]().AsParallel().Select(func(i int) int { return i }).
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestParallelCountAndForEach(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n, err := goq.Range(0, 50).AsParallel(goq.Workers(4)).
		Select(func(i int) int { return i }).Count(ctx)
	if err != nil || n != 50 {
		t.Errorf("Count = (%v, %v), want (50, nil)", n, err)
	}

	var seen atomic.Int64
	if err := goq.Range(0, 50).AsParallel(goq.Workers(4)).
		Select(func(i int) int { return i }).
		ForEach(ctx, func(int) error { seen.Add(1); return nil }); err != nil {
		t.Fatalf("ForEach err = %v", err)
	}
	if seen.Load() != 50 {
		t.Errorf("ForEach saw %d, want 50", seen.Load())
	}
}

// Where must survive a nil interface element: a plain type assertion on a
// boxed nil panics, but a nil error in a []error is an ordinary input, not a
// bug.
func TestParallelWhereHandlesNilInterfaceElements(t *testing.T) {
	t.Parallel()
	got, err := goq.From([]error{nil, errors.New("real"), nil}).
		AsParallel(goq.Workers(2)).
		Where(func(error) bool { return true }).
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d elements, want 3", len(got))
	}
	// This test exists specifically to protect the comma-ok unbox in Where's
	// k.val.(T) — asserting only the count would pass even if that unbox
	// silently produced some non-nil zero value instead of a true nil error.
	var nils int
	for _, e := range got {
		if e == nil {
			nils++
			continue
		}
		if e.Error() != "real" {
			t.Errorf("non-nil element = %q, want %q", e.Error(), "real")
		}
	}
	if nils != 2 {
		t.Errorf("got %d nil elements, want 2 (the comma-ok unbox may not be "+
			"producing a true nil)", nils)
	}
}

// PanicValue.String is what the Go runtime prints for an unrecovered
// panic(PanicValue{...}) — it is the parallel engine's primary diagnostic
// surface, and nothing else exercises it. It must contain both the original
// panic value and (some of) the captured stack.
func TestPanicValueString(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		pv, ok := r.(goq.PanicValue)
		if !ok {
			t.Fatalf("recovered %T, want goq.PanicValue", r)
		}
		s := pv.String()
		if !strings.Contains(s, "diagnostic marker 42") {
			t.Errorf("String() = %q, does not contain the original panic value", s)
		}
		if !strings.Contains(s, "goroutine") {
			t.Errorf("String() = %q, does not contain a stack trace", s)
		}
	}()

	_, _ = goq.From([]int{1}).
		AsParallel(goq.Workers(1)).
		Select(func(int) int { panic("diagnostic marker 42") }).
		ToSlice(context.Background())
	t.Fatal("ToSlice returned instead of panicking")
}

// A panic in a caller-supplied stage upstream of AsParallel runs on the
// producer goroutine, not the caller's. Without a recover there it would kill
// the process instead of reaching the caller.
func TestParallelUpstreamPanicReachesCaller(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("no panic reached the caller — it died on the producer goroutine")
		}
		pv, ok := r.(goq.PanicValue)
		if !ok {
			t.Fatalf("recovered %T, want goq.PanicValue", r)
		}
		if pv.Value != "upstream exploded" {
			t.Errorf("Value = %v, want \"upstream exploded\"", pv.Value)
		}
	}()

	_, _ = goq.From([]int{1, 2, 3}).
		Select(func(int) int { panic("upstream exploded") }).
		AsParallel(goq.Workers(2)).
		Select(func(i int) int { return i }).
		ToSlice(context.Background())
	t.Fatal("ToSlice returned instead of panicking")
}

// A panic from an inner AsParallel pipeline, consumed by an outer one via
// AsSequential, must surface on the caller's goroutine unwrapped — not
// double-wrapped in a PanicValue, and not silently swallowed as a truncated
// success.
func TestNestedParallelPanicReachesCaller(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("no panic reached the caller from a nested parallel pipeline")
		}
		pv, ok := r.(goq.PanicValue)
		if !ok {
			t.Fatalf("recovered %T, want goq.PanicValue", r)
		}
		// Unwrapped, not double-wrapped.
		if pv.Value != "inner exploded" {
			t.Errorf("Value = %v, want \"inner exploded\"", pv.Value)
		}
	}()

	_, _ = goq.Range(0, 50).
		AsParallel(goq.Workers(3)).
		Select(func(i int) int {
			if i == 3 {
				panic("inner exploded")
			}
			return i
		}).
		AsSequential().
		AsParallel(goq.Workers(2)).
		Select(func(i int) int { return i }).
		ToSlice(context.Background())
	t.Fatal("ToSlice returned instead of panicking")
}

// A panic from an inner AsParallel pipeline whose terminal is invoked
// directly inside an outer Select callback — as opposed to reached through
// AsSequential, covered above — runs the inner pipeline's own consumption
// loop, and thus its re-panic, on the OUTER worker goroutine rather than the
// outer producer goroutine. This exercises the per-worker recover's
// PanicValue unwrap specifically, which TestNestedParallelPanicReachesCaller
// does not: that test's inner pipeline is consumed via AsSequential, so its
// panic surfaces on the outer producer, exercising only the producer's
// unwrap.
func TestNestedParallelPanicInsideSelectReachesCaller(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("no panic reached the caller from a pipeline nested inside a Select callback")
		}
		pv, ok := r.(goq.PanicValue)
		if !ok {
			t.Fatalf("recovered %T, want goq.PanicValue", r)
		}
		// Unwrapped, not double-wrapped.
		if pv.Value != "inner exploded" {
			t.Errorf("Value = %v, want \"inner exploded\"", pv.Value)
		}
	}()

	_, _ = goq.Range(0, 10).
		AsParallel(goq.Workers(4)).
		Select(func(i int) int {
			_, _ = goq.Range(0, 5).
				AsParallel(goq.Workers(2)).
				Select(func(j int) int {
					if j == 2 {
						panic("inner exploded")
					}
					return j
				}).
				ToSlice(context.Background())
			return i
		}).
		ToSlice(context.Background())
	t.Fatal("ToSlice returned instead of panicking")
}

// Nested cancellation (as opposed to nested panics, covered above) already
// works: this locks it in so a future change to the parent/ctx split cannot
// silently regress it.
func TestNestedParallelCancellationReachesCaller(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()

	got, err := goq.Range(0, 500).
		AsParallel(goq.Workers(4)).
		SelectCtx(func(_ context.Context, i int) (int, error) {
			time.Sleep(5 * time.Millisecond)
			return i, nil
		}).
		AsSequential().
		AsParallel(goq.Workers(2)).
		Select(func(i int) int { return i }).
		ToSlice(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if got != nil {
		t.Errorf("got %v on cancellation, want nil", got)
	}
}

// The zero-value ParQuery must behave like the zero-value Query and TryQuery:
// yield nothing, never panic, never deadlock.
func TestZeroValueParQueryDoesNotPanic(t *testing.T) {
	t.Parallel()
	var p goq.ParQuery[int]
	got, err := p.ToSlice(context.Background())
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// Invariant 4 (backpressure) rests entirely on the input channel being
// unbuffered. TestParallelRunsConcurrently bounds in-flight concurrency,
// which a buffered input channel could also satisfy; this test specifically
// checks that the producer cannot run ahead of the workers.
func TestParallelInputChannelIsUnbuffered(t *testing.T) {
	t.Parallel()
	var pulls atomic.Int64
	release := make(chan struct{})
	src := func(yield func(int) bool) {
		for i := range 1000 {
			pulls.Add(1)
			if !yield(i) {
				return
			}
		}
	}
	p := goq.FromSeq(src).AsParallel(goq.Workers(1)).
		SelectCtx(func(context.Context, int) (int, error) {
			<-release
			return 0, nil
		})

	done := make(chan struct{})
	go func() { defer close(done); _, _ = p.ToSlice(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	got := pulls.Load()
	close(release)
	<-done

	// With an unbuffered input and one blocked worker the producer cannot run
	// ahead: one element is held by the worker and one is pending on the send.
	// The exact count is 2, not merely "small" — a threshold like <= 10 would
	// still pass with an input buffer of 4-9, silently missing that bug.
	if got != 2 {
		t.Errorf("producer pulled %d elements while the only worker was blocked, "+
			"want exactly 2 — the input channel is buffered", got)
	}
}
