package goq

import (
	"context"
	"iter"
	"sync"
	"sync/atomic"
)

// TryQuery is a lazy query pipeline whose elements may fail to be produced and
// whose execution can be cancelled.
//
// It serves two cases with one engine: a fallible synchronous pipeline, as
// produced by Query.SelectErr, and a concurrent streaming pipeline, as produced
// by FromChan. From the operators' point of view those differ only in the
// source, so they share a type.
//
// A TryQuery holds a plan rather than a built iterator, so the context is
// supplied to the terminal operator that actually blocks, and one TryQuery
// value can be executed under different contexts.
//
// The first error stops the pipeline: no further elements are pulled, upstream
// producers are cancelled, and collecting terminals return a nil result rather
// than a truncated one.
type TryQuery[T any] struct {
	plan  func(context.Context) iter.Seq2[T, error]
	guard *singleShot
}

// singleShot marks a source that can only be enumerated once.
type singleShot struct{ consumed atomic.Bool }

// Seq returns the pipeline as an iterator of value/error pairs.
//
// If the source is single-shot and has already been enumerated, the returned
// iterator yields exactly one pair carrying ErrConsumed.
func (q TryQuery[T]) Seq(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		if q.guard != nil && !q.guard.consumed.CompareAndSwap(false, true) {
			var zero T
			yield(zero, ErrConsumed)
			return
		}
		if q.plan == nil {
			return
		}
		for v, err := range q.plan(ctx) {
			if !yield(v, err) {
				return
			}
		}
	}
}

// ToSlice materialises the pipeline. On error it returns a nil slice, never a
// partial one, so a truncated result cannot be mistaken for a complete one.
func (q TryQuery[T]) ToSlice(ctx context.Context) ([]T, error) {
	out := []T{}
	for v, err := range q.Seq(ctx) {
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// First returns the first element, whether one was found, and any pipeline
// error. An empty source is reported by the bool, not by the error.
func (q TryQuery[T]) First(ctx context.Context) (T, bool, error) {
	for v, err := range q.Seq(ctx) {
		var zero T
		if err != nil {
			return zero, false, err
		}
		return v, true, nil
	}
	var zero T
	return zero, false, nil
}

// Last returns the final element, whether one was found, and any pipeline
// error. It enumerates the whole source.
func (q TryQuery[T]) Last(ctx context.Context) (T, bool, error) {
	var (
		last  T
		found bool
	)
	for v, err := range q.Seq(ctx) {
		if err != nil {
			var zero T
			return zero, false, err
		}
		last, found = v, true
	}
	return last, found, nil
}

// Single returns the only element, ErrEmpty if there are none, or ErrMultiple
// if there are two or more. A pipeline error takes precedence over both.
func (q TryQuery[T]) Single(ctx context.Context) (T, error) {
	var (
		result T
		found  bool
	)
	for v, err := range q.Seq(ctx) {
		var zero T
		if err != nil {
			return zero, err
		}
		if found {
			return zero, ErrMultiple
		}
		result, found = v, true
	}
	if !found {
		var zero T
		return zero, ErrEmpty
	}
	return result, nil
}

// Count reports the number of elements, or the first pipeline error.
func (q TryQuery[T]) Count(ctx context.Context) (int, error) {
	n := 0
	for _, err := range q.Seq(ctx) {
		if err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}

// Memoize returns a TryQuery that caches the elements and the terminal error on
// first enumeration and replays both afterwards.
//
// The result is re-enumerable by construction, so it never returns ErrConsumed
// even when the original source was single-shot. Every element is retained, so
// it must not be used on an unbounded source. The context of the first
// enumeration is the one that governs; later enumerations replay the cached
// outcome without consulting their own.
func (q TryQuery[T]) Memoize() TryQuery[T] {
	var (
		once   sync.Once
		cached []T
		cerr   error
	)
	return TryQuery[T]{plan: func(ctx context.Context) iter.Seq2[T, error] {
		return func(yield func(T, error) bool) {
			once.Do(func() { cached, cerr = q.ToSlice(ctx) })
			for _, v := range cached {
				if !yield(v, nil) {
					return
				}
			}
			if cerr != nil {
				var zero T
				yield(zero, cerr)
			}
		}
	}}
}

// AsTry lifts an infallible pipeline into a fallible one, so that fallible
// operators and cancellable terminals become available. It introduces no
// errors of its own.
func (q Query[T]) AsTry() TryQuery[T] {
	seq := q.Seq()
	return TryQuery[T]{plan: func(ctx context.Context) iter.Seq2[T, error] {
		return func(yield func(T, error) bool) {
			for v := range seq {
				if err := ctx.Err(); err != nil {
					var zero T
					yield(zero, err)
					return
				}
				if !yield(v, nil) {
					return
				}
			}
		}
	}}
}
