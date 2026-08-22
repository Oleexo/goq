package goq_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/oleexo/goq"
)

var errBoom = errors.New("boom")

func TestSelectErrShortCircuits(t *testing.T) {
	t.Parallel()
	calls := 0
	got, err := goq.From([]string{"1", "2", "x", "4"}).
		SelectErr(func(s string) (int, error) {
			calls++
			return strconv.Atoi(s)
		}).
		ToSlice(context.Background())

	if err == nil {
		t.Fatal("err = nil, want a strconv error")
	}
	if got != nil {
		t.Errorf("got %v on error, want nil", got)
	}
	// Stopped at "x": the fourth element was never visited.
	if calls != 3 {
		t.Errorf("selector called %d times, want 3", calls)
	}
	// Callback errors are returned verbatim, so the caller's own checks work.
	var numErr *strconv.NumError
	if !errors.As(err, &numErr) {
		t.Errorf("err = %v; want it to unwrap to *strconv.NumError", err)
	}
}

func TestSelectPassesErrorsThrough(t *testing.T) {
	t.Parallel()
	_, err := goq.From([]string{"x"}).
		SelectErr(func(_ string) (int, error) { return 0, errBoom }).
		Select(func(i int) int { return i * 2 }). // pure stage after a failure
		ToSlice(context.Background())
	if !errors.Is(err, errBoom) {
		t.Errorf("err = %v, want errBoom", err)
	}
}

func TestWhereErr(t *testing.T) {
	t.Parallel()
	got, err := goq.From([]int{1, 2, 3, 4}).
		WhereErr(func(i int) (bool, error) { return i%2 == 0, nil }).
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d := cmp.Diff([]int{2, 4}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}

	if _, err := goq.From([]int{1}).
		WhereErr(func(int) (bool, error) { return false, errBoom }).
		ToSlice(context.Background()); !errors.Is(err, errBoom) {
		t.Errorf("err = %v, want errBoom", err)
	}
}

// Where is the pure counterpart to WhereErr and has no test of its own
// elsewhere; this exercises it directly rather than only through WhereErr.
func TestTryQueryWhere(t *testing.T) {
	t.Parallel()
	got, err := goq.From([]int{1, 2, 3, 4}).AsTry().
		Where(func(i int) bool { return i%2 == 0 }).
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d := cmp.Diff([]int{2, 4}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}

	// Errors from an earlier stage pass through Where untouched.
	if _, err := goq.From([]string{"x"}).
		SelectErr(func(_ string) (int, error) { return 0, errBoom }).
		Where(func(int) bool { return true }).
		ToSlice(context.Background()); !errors.Is(err, errBoom) {
		t.Errorf("err = %v, want errBoom", err)
	}
}

func TestSelectCtxReceivesTerminalContext(t *testing.T) {
	t.Parallel()
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "v")
	got, err := goq.From([]int{1}).AsTry().
		SelectCtx(func(c context.Context, _ int) (string, error) {
			s, _ := c.Value(key{}).(string)
			return s, nil
		}).
		ToSlice(ctx)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d := cmp.Diff([]string{"v"}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
}

func TestTryTakeAndSkip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got, err := goq.Range(0, 10).AsTry().Skip(2).Take(3).ToSlice(ctx)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d := cmp.Diff([]int{2, 3, 4}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}
	got2, err := goq.Range(0, 10).AsTry().TakeWhile(func(i int) bool { return i < 3 }).ToSlice(ctx)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d := cmp.Diff([]int{0, 1, 2}, got2); d != "" {
		t.Errorf("TakeWhile mismatch (-want +got):\n%s", d)
	}
}

func TestTryAggregateAndForEach(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sum, err := goq.Range(1, 3).AsTry().Aggregate(ctx, 0, func(a, v int) int { return a + v })
	if err != nil || sum != 6 {
		t.Errorf("Aggregate = (%v, %v), want (6, nil)", sum, err)
	}

	seen := 0
	if err := goq.Range(0, 5).AsTry().ForEach(ctx, func(int) error {
		seen++
		if seen == 2 {
			return errBoom
		}
		return nil
	}); !errors.Is(err, errBoom) {
		t.Errorf("ForEach err = %v, want errBoom", err)
	}
	if seen != 2 {
		t.Errorf("ForEach visited %d elements, want 2", seen)
	}
}

func TestTrySelectMany(t *testing.T) {
	t.Parallel()
	got, err := goq.From([]int{1, 2, 3}).AsTry().
		SelectMany(func(i int) []int { return []int{i, i * 10} }).
		ToSlice(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d := cmp.Diff([]int{1, 10, 2, 20, 3, 30}, got); d != "" {
		t.Errorf("mismatch (-want +got):\n%s", d)
	}

	_, err = goq.From([]string{"x"}).
		SelectErr(func(_ string) (int, error) { return 0, errBoom }).
		SelectMany(func(i int) []int { return []int{i, i} }). // stage after a failure
		ToSlice(context.Background())
	if !errors.Is(err, errBoom) {
		t.Errorf("err = %v, want errBoom", err)
	}
}

// A zero-value TryQuery is constructible from outside the package. Seq
// tolerates it; operators built through lift must too.
func TestZeroValueTryQueryOperatorsDoNotPanic(t *testing.T) {
	t.Parallel()
	var q goq.TryQuery[int]

	got, err := q.Select(func(i int) int { return i * 2 }).ToSlice(context.Background())
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// lift must carry the single-shot guard forward, so ErrConsumed still reaches
// the terminal through intermediate operators. Without the propagation this
// second enumeration would silently yield nothing instead of erroring.
func TestGuardPropagatesThroughLift(t *testing.T) {
	t.Parallel()
	ch := make(chan int, 1)
	ch <- 1
	close(ch)

	q := goq.FromChan(ch).Select(func(i int) int { return i * 2 })
	ctx := context.Background()

	if _, err := q.ToSlice(ctx); err != nil {
		t.Fatalf("first enumeration err = %v, want nil", err)
	}
	if _, err := q.ToSlice(ctx); !errors.Is(err, goq.ErrConsumed) {
		t.Errorf("second enumeration err = %v, want ErrConsumed — "+
			"lift is not propagating the single-shot guard", err)
	}
}
