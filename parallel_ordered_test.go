package goq_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/oleexo/goq"
)

// Randomised-looking durations must not disturb the output order.
func TestAsOrderedPreservesSourceOrder(t *testing.T) {
	t.Parallel()
	got, err := goq.Range(0, 64).
		AsParallel(goq.Workers(8), goq.Window(16)).
		SelectCtx(func(_ context.Context, i int) (int, error) {
			time.Sleep(time.Duration((37*i)%11) * time.Millisecond)
			return i * i, nil
		}).
		AsOrdered().
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := make([]int, 0, 64)
	for i := range 64 {
		want = append(want, i*i)
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("order not preserved (-want +got):\n%s", d)
	}
}

// Ordered output must equal the sequential result exactly. This is the
// equivalence property the whole ordered mode exists to provide.
func TestAsOrderedEqualsSequential(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, workers := range []int{1, 2, 3, 8, 16} {
		for _, window := range []int{1, 2, 7, 64} {
			sequential, err := goq.Range(0, 50).AsTry().
				Select(func(i int) int { return i * 3 }).ToSlice(ctx)
			if err != nil {
				t.Fatalf("sequential err = %v", err)
			}
			parallel, err := goq.Range(0, 50).
				AsParallel(goq.Workers(workers), goq.Window(window)).
				SelectCtx(func(_ context.Context, i int) (int, error) {
					time.Sleep(time.Duration((13*i)%5) * time.Millisecond)
					return i * 3, nil
				}).
				AsOrdered().
				ToSlice(ctx)
			if err != nil {
				t.Fatalf("parallel err = %v (workers=%d window=%d)", err, workers, window)
			}
			if d := cmp.Diff(sequential, parallel); d != "" {
				t.Errorf("workers=%d window=%d mismatch (-seq +par):\n%s", workers, window, d)
			}
		}
	}
}

// A window of 1 forces strictly serialised emission and must still be correct.
func TestAsOrderedWindowOneIsCorrect(t *testing.T) {
	t.Parallel()
	got, err := goq.Range(0, 20).
		AsParallel(goq.Workers(8), goq.Window(1)).
		Select(func(i int) int { return i }).
		AsOrdered().
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d := cmp.Diff(goq.Range(0, 20).ToSlice(), got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

// TestAsOrderedSelectMany is the order-preserving half of Task 17's
// multiset-only SelectMany test: with AsOrdered, each input's flattened batch
// must stay contiguous AND the batches must appear in source order.
func TestAsOrderedSelectMany(t *testing.T) {
	t.Parallel()
	got, err := goq.Range(1, 4).
		AsParallel(goq.Workers(4)).
		SelectMany(func(i int) []int { return []int{i, i} }).
		AsOrdered().
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d := cmp.Diff([]int{1, 1, 2, 2, 3, 3, 4, 4}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestAsOrderedErrorShortCircuits(t *testing.T) {
	t.Parallel()
	_, err := goq.Range(0, 100).
		AsParallel(goq.Workers(8)).
		SelectErr(func(i int) (int, error) {
			if i == 5 {
				return 0, errBoom
			}
			return i, nil
		}).
		AsOrdered().
		ToSlice(context.Background())
	if !errors.Is(err, errBoom) {
		t.Errorf("err = %v, want errBoom", err)
	}
}

func TestAsOrderedCancellationIsReported(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	_, err := goq.Range(0, 500).
		AsParallel(goq.Workers(4)).
		SelectCtx(func(_ context.Context, i int) (int, error) {
			time.Sleep(5 * time.Millisecond)
			return i, nil
		}).
		AsOrdered().
		ToSlice(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
}

func TestAsOrderedEarlyBreakDoesNotLeak(t *testing.T) {
	t.Parallel()
	p := goq.Range(0, 10_000).
		AsParallel(goq.Workers(8), goq.Window(4)).
		Select(func(i int) int { return i }).
		AsOrdered()
	n := 0
	for v, err := range p.AsSequential().Seq(context.Background()) {
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if v != n {
			t.Errorf("element %d = %d, want %d", n, v, n)
		}
		n++
		if n == 5 {
			break
		}
	}
	time.Sleep(50 * time.Millisecond)
}
