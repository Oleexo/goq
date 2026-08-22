package goq_test

import (
	"context"
	"errors"
	"slices"
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

// TestParallelSelectMany asserts as a MULTISET, because AsOrdered does not
// exist until Task 18 and unordered mode is the default. Task 18 adds the
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
