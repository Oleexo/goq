package goq_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/oleexo/goq"
)

func closedChan(vs ...int) <-chan int {
	ch := make(chan int, len(vs))
	for _, v := range vs {
		ch <- v
	}
	close(ch)
	return ch
}

func TestFromChanToSlice(t *testing.T) {
	t.Parallel()
	got, err := goq.FromChan(closedChan(1, 2, 3)).ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d := cmp.Diff([]int{1, 2, 3}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

// A single-shot source must report ErrConsumed rather than silently yielding
// nothing on a second enumeration.
func TestFromChanIsSingleShot(t *testing.T) {
	t.Parallel()
	q := goq.FromChan(closedChan(1, 2))
	if _, err := q.ToSlice(context.Background()); err != nil {
		t.Fatalf("first enumeration err = %v", err)
	}
	got, err := q.ToSlice(context.Background())
	if !errors.Is(err, goq.ErrConsumed) {
		t.Errorf("second enumeration err = %v, want ErrConsumed", err)
	}
	if got != nil {
		t.Errorf("second enumeration returned %v, want nil", got)
	}
}

// Memoize makes a single-shot source re-enumerable and must clear the guard.
func TestTryMemoizeClearsSingleShotGuard(t *testing.T) {
	t.Parallel()
	q := goq.FromChan(closedChan(1, 2)).Memoize()
	first, err1 := q.ToSlice(context.Background())
	second, err2 := q.ToSlice(context.Background())
	if err1 != nil || err2 != nil {
		t.Fatalf("errs = %v, %v; want nil, nil", err1, err2)
	}
	if d := cmp.Diff(first, second); d != "" {
		t.Errorf("enumerations differ (-first +second):\n%s", d)
	}
}

// ctx cancellation must surface as an error, never as a short slice with nil.
func TestFromChanCancellation(t *testing.T) {
	t.Parallel()
	blocked := make(chan int) // never written, never closed
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	got, err := goq.FromChan(blocked).ToSlice(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if got != nil {
		t.Errorf("got %v on error, want nil", got)
	}
}

// The plan is parameterised by ctx, so one pipeline is reusable across
// contexts. Memoize first, since the underlying channel is single-shot.
func TestTryQueryIsReusableAcrossContexts(t *testing.T) {
	t.Parallel()
	q := goq.FromChan(closedChan(1, 2)).Memoize()
	if _, err := q.ToSlice(context.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := q.ToSlice(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want nil or Canceled", err)
	}
}

func TestTryElementOperators(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	v, ok, err := goq.From([]int{7, 8}).AsTry().First(ctx)
	if err != nil || !ok || v != 7 {
		t.Errorf("First = (%v, %v, %v), want (7, true, nil)", v, ok, err)
	}
	// Empty is reported by the bool, NOT as an error.
	if _, ok, err := goq.Empty[int]().AsTry().First(ctx); err != nil || ok {
		t.Errorf("First on empty = (_, %v, %v), want (false, nil)", ok, err)
	}
	if v, ok, err := goq.From([]int{1, 2, 3}).AsTry().Last(ctx); err != nil || !ok || v != 3 {
		t.Errorf("Last = (%v, %v, %v), want (3, true, nil)", v, ok, err)
	}
	if _, err := goq.Empty[int]().AsTry().Single(ctx); !errors.Is(err, goq.ErrEmpty) {
		t.Errorf("Single on empty err = %v, want ErrEmpty", err)
	}
	if _, err := goq.From([]int{1, 2}).AsTry().Single(ctx); !errors.Is(err, goq.ErrMultiple) {
		t.Errorf("Single on two err = %v, want ErrMultiple", err)
	}
	if n, err := goq.From([]int{1, 2}).AsTry().Count(ctx); err != nil || n != 2 {
		t.Errorf("Count = (%v, %v), want (2, nil)", n, err)
	}
}

func TestFromSeqTryIsSingleShot(t *testing.T) {
	t.Parallel()
	q := goq.FromSeqTry(goq.From([]int{1, 2}).Seq())
	if _, err := q.ToSlice(context.Background()); err != nil {
		t.Fatalf("first err = %v", err)
	}
	if _, err := q.ToSlice(context.Background()); !errors.Is(err, goq.ErrConsumed) {
		t.Errorf("second err = %v, want ErrConsumed", err)
	}
}
