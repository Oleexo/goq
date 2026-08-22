package goq

import (
	"context"
	"iter"
	"maps"
	"slices"
)

// Pair is a key/value pair, the element type produced by FromMap.
type Pair[K comparable, V any] struct {
	Key   K
	Value V
}

// From returns a Query over the elements of s. A nil slice yields nothing.
//
// The slice is not copied: mutating it after From returns affects later
// enumerations.
func From[T any](s []T) Query[T] {
	return Query[T]{seq: slices.Values(s)}
}

// FromSeq returns a Query over an existing iterator.
//
// The caller guarantees that s may be enumerated more than once. A single-shot
// iterator — one reading from a channel or a network stream — will silently
// yield nothing on a second enumeration, because Query's terminals have no
// error return with which to report it. Use FromSeqTry for such sources.
func FromSeq[T any](s iter.Seq[T]) Query[T] {
	return Query[T]{seq: s}
}

// FromMap returns a Query over a map's key/value pairs. Order is unspecified,
// matching Go's map iteration.
func FromMap[K comparable, V any](m map[K]V) Query[Pair[K, V]] {
	return Query[Pair[K, V]]{seq: func(yield func(Pair[K, V]) bool) {
		for k, v := range maps.All(m) {
			if !yield(Pair[K, V]{Key: k, Value: v}) {
				return
			}
		}
	}}
}

// Range returns a Query over count consecutive integers beginning at start.
// A count of zero or less yields nothing.
func Range(start, count int) Query[int] {
	return Query[int]{seq: func(yield func(int) bool) {
		for i := range count {
			if !yield(start + i) {
				return
			}
		}
	}}
}

// Repeat returns a Query yielding v count times. A count of zero or less
// yields nothing.
func Repeat[T any](v T, count int) Query[T] {
	return Query[T]{seq: func(yield func(T) bool) {
		for range count {
			if !yield(v) {
				return
			}
		}
	}}
}

// Empty returns a Query that yields no elements.
func Empty[T any]() Query[T] {
	return Query[T]{seq: func(func(T) bool) {}}
}

// FromChan returns a single-shot TryQuery over a channel.
//
// Enumeration ends when the channel is closed, and yields the context's error
// if the context is cancelled first. The channel is not drained on
// cancellation, so a producer blocked on an unbuffered send stays blocked
// unless it also selects on the same context.
//
// The result is single-shot: a second enumeration returns ErrConsumed rather
// than silently yielding nothing. Call Memoize to make it re-enumerable.
func FromChan[T any](ch <-chan T) TryQuery[T] {
	return TryQuery[T]{
		guard: &singleShot{},
		plan: func(ctx context.Context) iter.Seq2[T, error] {
			return func(yield func(T, error) bool) {
				for {
					select {
					case <-ctx.Done():
						var zero T
						yield(zero, ctx.Err())
						return
					case v, open := <-ch:
						if !open {
							return
						}
						if !yield(v, nil) {
							return
						}
					}
				}
			}
		},
	}
}

// FromSeqTry returns a single-shot TryQuery over an existing iterator.
//
// Use it in preference to FromSeq when the iterator can only be enumerated
// once: a second enumeration returns ErrConsumed rather than silently yielding
// nothing, which FromSeq cannot report because Query's terminals have no error
// return.
func FromSeqTry[T any](s iter.Seq[T]) TryQuery[T] {
	return TryQuery[T]{
		guard: &singleShot{},
		plan: func(ctx context.Context) iter.Seq2[T, error] {
			return func(yield func(T, error) bool) {
				for v := range s {
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
		},
	}
}
