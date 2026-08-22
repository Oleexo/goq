package goq

import (
	"context"
	"iter"
)

// lift builds a TryQuery from a stage function, preserving the single-shot
// guard so that ErrConsumed still reaches the terminal through any number of
// intermediate operators.
func lift[T, R any](
	q TryQuery[T],
	stage func(context.Context, iter.Seq2[T, error]) iter.Seq2[R, error],
) TryQuery[R] {
	return TryQuery[R]{
		guard: q.guard,
		plan: func(ctx context.Context) iter.Seq2[R, error] {
			return stage(ctx, q.plan(ctx))
		},
	}
}

// Select yields f applied to each element. It is the pure counterpart to
// SelectErr: reach for Select when the projection cannot fail. Errors from
// earlier stages pass through untouched and stop the pipeline.
func (q TryQuery[T]) Select[R any](f func(T) R) TryQuery[R] {
	return lift(q, func(_ context.Context, src iter.Seq2[T, error]) iter.Seq2[R, error] {
		return func(yield func(R, error) bool) {
			for v, err := range src {
				var zero R
				if err != nil {
					yield(zero, err)
					return
				}
				if !yield(f(v), nil) {
					return
				}
			}
		}
	})
}

// SelectErr yields f applied to each element, stopping at the first error. It
// is the fallible counterpart to Select: reach for SelectErr when the
// projection itself can fail. The error is returned verbatim, so errors.Is and
// errors.As work against the caller's own error types.
func (q TryQuery[T]) SelectErr[R any](f func(T) (R, error)) TryQuery[R] {
	return q.SelectCtx(func(_ context.Context, v T) (R, error) { return f(v) })
}

// SelectCtx is SelectErr with the terminal operator's context passed to f, for
// projections that perform cancellable work.
func (q TryQuery[T]) SelectCtx[R any](f func(context.Context, T) (R, error)) TryQuery[R] {
	return lift(q, func(ctx context.Context, src iter.Seq2[T, error]) iter.Seq2[R, error] {
		return func(yield func(R, error) bool) {
			for v, err := range src {
				var zero R
				if err != nil {
					yield(zero, err)
					return
				}
				r, ferr := f(ctx, v)
				if ferr != nil {
					yield(zero, ferr)
					return
				}
				if !yield(r, nil) {
					return
				}
			}
		}
	})
}

// SelectMany flattens the slice each element projects to. Errors from earlier
// stages pass through untouched.
func (q TryQuery[T]) SelectMany[R any](f func(T) []R) TryQuery[R] {
	return lift(q, func(_ context.Context, src iter.Seq2[T, error]) iter.Seq2[R, error] {
		return func(yield func(R, error) bool) {
			for v, err := range src {
				var zero R
				if err != nil {
					yield(zero, err)
					return
				}
				for _, r := range f(v) {
					if !yield(r, nil) {
						return
					}
				}
			}
		}
	})
}

// Where yields the elements for which pred returns true. It is the pure
// counterpart to WhereErr: reach for Where when the predicate cannot fail.
func (q TryQuery[T]) Where(pred func(T) bool) TryQuery[T] {
	return q.WhereErr(func(v T) (bool, error) { return pred(v), nil })
}

// WhereErr yields the elements for which pred returns true, stopping at the
// first error from pred. It is the fallible counterpart to Where: reach for
// WhereErr when the predicate itself can fail.
func (q TryQuery[T]) WhereErr(pred func(T) (bool, error)) TryQuery[T] {
	return lift(q, func(_ context.Context, src iter.Seq2[T, error]) iter.Seq2[T, error] {
		return func(yield func(T, error) bool) {
			for v, err := range src {
				var zero T
				if err != nil {
					yield(zero, err)
					return
				}
				keep, perr := pred(v)
				if perr != nil {
					yield(zero, perr)
					return
				}
				if keep && !yield(v, nil) {
					return
				}
			}
		}
	})
}

// Take yields at most the first n elements, stopping at n elements or the
// first error, whichever comes first. A non-positive n yields nothing.
func (q TryQuery[T]) Take(n int) TryQuery[T] {
	return lift(q, func(_ context.Context, src iter.Seq2[T, error]) iter.Seq2[T, error] {
		return func(yield func(T, error) bool) {
			if n <= 0 {
				return
			}
			i := 0
			for v, err := range src {
				if !yield(v, err) || err != nil {
					return
				}
				i++
				if i >= n {
					return
				}
			}
		}
	})
}

// TakeWhile yields leading elements while pred holds.
func (q TryQuery[T]) TakeWhile(pred func(T) bool) TryQuery[T] {
	return lift(q, func(_ context.Context, src iter.Seq2[T, error]) iter.Seq2[T, error] {
		return func(yield func(T, error) bool) {
			for v, err := range src {
				if err != nil {
					yield(v, err)
					return
				}
				if !pred(v) || !yield(v, nil) {
					return
				}
			}
		}
	})
}

// Skip discards the first n elements and yields the rest.
func (q TryQuery[T]) Skip(n int) TryQuery[T] {
	return lift(q, func(_ context.Context, src iter.Seq2[T, error]) iter.Seq2[T, error] {
		return func(yield func(T, error) bool) {
			i := 0
			for v, err := range src {
				if err != nil {
					yield(v, err)
					return
				}
				if i < n {
					i++
					continue
				}
				if !yield(v, nil) {
					return
				}
			}
		}
	})
}

// Aggregate folds the pipeline into a single value using fn, starting from
// seed, or returns the first error encountered. It is a terminal operator.
func (q TryQuery[T]) Aggregate[A any](
	ctx context.Context, seed A, fn func(A, T) A,
) (A, error) {
	acc := seed
	for v, err := range q.Seq(ctx) {
		if err != nil {
			var zero A
			return zero, err
		}
		acc = fn(acc, v)
	}
	return acc, nil
}

// ForEach calls fn for each element, stopping at the first error from either
// the pipeline or fn. It is the terminal for side-effecting work.
func (q TryQuery[T]) ForEach(ctx context.Context, fn func(T) error) error {
	for v, err := range q.Seq(ctx) {
		if err != nil {
			return err
		}
		if ferr := fn(v); ferr != nil {
			return ferr
		}
	}
	return nil
}

// SelectErr yields f applied to each element, producing a fallible pipeline
// that stops at the first error. It is the usual entry point from an
// infallible Query into a fallible one, equivalent to q.AsTry().SelectErr(f).
func (q Query[T]) SelectErr[R any](f func(T) (R, error)) TryQuery[R] {
	return q.AsTry().SelectErr(f)
}

// WhereErr yields the elements for which pred returns true, producing a
// fallible pipeline that stops at the first error from pred. It is an entry
// point from an infallible Query into a fallible one, equivalent to
// q.AsTry().WhereErr(pred).
func (q Query[T]) WhereErr(pred func(T) (bool, error)) TryQuery[T] {
	return q.AsTry().WhereErr(pred)
}
