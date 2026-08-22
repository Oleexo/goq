package goq

import (
	"iter"
	"slices"
)

// Query is a lazy, infallible query pipeline over elements of type T.
//
// Operators return a new Query wrapping the previous one; no element is pulled
// from the source until a terminal operator such as ToSlice consumes the
// pipeline. A Query is re-enumerable if and only if its source is: one built
// from a slice re-executes on every enumeration, while one built from a channel
// is single-shot. See FromChan and Memoize.
//
// The zero Query yields no elements.
type Query[T any] struct {
	seq iter.Seq[T]
}

// Seq returns the pipeline as an iterator, for use with range or any
// iter.Seq-based API such as slices.Collect.
//
// Together with FromSeq this is goq's extension point: Query is a concrete
// type and cannot be implemented by other packages, because Go does not permit
// type parameters on interface methods.
func (q Query[T]) Seq() iter.Seq[T] {
	if q.seq == nil {
		return func(func(T) bool) {}
	}
	return q.seq
}

// ToSlice materialises the pipeline into a new slice. It returns an empty
// slice, never nil, when the pipeline yields nothing.
func (q Query[T]) ToSlice() []T {
	out := slices.Collect(q.Seq())
	if out == nil {
		out = []T{}
	}
	return out
}

// iterPull adapts a push iterator to a pull iterator. Callers must call the
// returned stop function; iter.Pull's coroutine is otherwise leaked.
func iterPull[T any](s iter.Seq[T]) (next func() (T, bool), stop func()) {
	return iter.Pull(s)
}
