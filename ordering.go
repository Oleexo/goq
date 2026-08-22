package goq

import (
	"cmp"
	"iter"
	"slices"
)

// OrderedQuery is a Query with a pending sort. It exists so that ThenBy is
// reachable only after OrderBy, mirroring C#'s IOrderedEnumerable: calling
// ThenBy on an unordered Query is a compile error.
//
// Ordering is a buffering operation. The source is fully consumed and sorted
// when the pipeline is first enumerated, so an OrderedQuery over an unbounded
// source never yields.
type OrderedQuery[T any] struct {
	src  iter.Seq[T]
	cmps []func(a, b T) int
}

// OrderBy sorts ascending by key. Sorting is stable: elements with equal keys
// keep their source order.
func (q Query[T]) OrderBy[K cmp.Ordered](key func(T) K) OrderedQuery[T] {
	return OrderedQuery[T]{src: q.Seq(), cmps: []func(a, b T) int{ascending(key)}}
}

// OrderByDesc sorts descending by key. Sorting is stable.
func (q Query[T]) OrderByDesc[K cmp.Ordered](key func(T) K) OrderedQuery[T] {
	return OrderedQuery[T]{src: q.Seq(), cmps: []func(a, b T) int{descending(key)}}
}

// OrderByFunc sorts using an explicit three-way comparison, for element types
// whose ordering is not expressible as a cmp.Ordered key.
func (q Query[T]) OrderByFunc(cmpFn func(a, b T) int) OrderedQuery[T] {
	return OrderedQuery[T]{src: q.Seq(), cmps: []func(a, b T) int{cmpFn}}
}

// Reverse yields the elements in reverse order. It buffers the entire source.
func (q Query[T]) Reverse() Query[T] {
	return Query[T]{seq: func(yield func(T) bool) {
		buf := slices.Collect(q.Seq())
		for i := len(buf) - 1; i >= 0; i-- {
			if !yield(buf[i]) {
				return
			}
		}
	}}
}

// ThenBy adds an ascending tie-breaking key, applied only where all previous
// keys compare equal.
func (o OrderedQuery[T]) ThenBy[K cmp.Ordered](key func(T) K) OrderedQuery[T] {
	return OrderedQuery[T]{src: o.src, cmps: appendCmp(o.cmps, ascending(key))}
}

// ThenByDesc adds a descending tie-breaking key.
func (o OrderedQuery[T]) ThenByDesc[K cmp.Ordered](key func(T) K) OrderedQuery[T] {
	return OrderedQuery[T]{src: o.src, cmps: appendCmp(o.cmps, descending(key))}
}

// Seq returns the sorted pipeline as an iterator.
func (o OrderedQuery[T]) Seq() iter.Seq[T] {
	return func(yield func(T) bool) {
		buf := slices.Collect(o.src)
		slices.SortStableFunc(buf, o.compare)
		for _, v := range buf {
			if !yield(v) {
				return
			}
		}
	}
}

// AsQuery returns the sorted pipeline as a Query, giving access to every
// operator not defined on OrderedQuery.
func (o OrderedQuery[T]) AsQuery() Query[T] { return Query[T]{seq: o.Seq()} }

// ToSlice materialises the sorted pipeline into a new slice.
func (o OrderedQuery[T]) ToSlice() []T { return o.AsQuery().ToSlice() }

// Select yields f applied to each element in sorted order.
func (o OrderedQuery[T]) Select[R any](f func(T) R) Query[R] {
	return o.AsQuery().Select(f)
}

func (o OrderedQuery[T]) compare(a, b T) int {
	for _, c := range o.cmps {
		if r := c(a, b); r != 0 {
			return r
		}
	}
	return 0
}

func ascending[T any, K cmp.Ordered](key func(T) K) func(a, b T) int {
	return func(a, b T) int { return cmp.Compare(key(a), key(b)) }
}

func descending[T any, K cmp.Ordered](key func(T) K) func(a, b T) int {
	return func(a, b T) int { return cmp.Compare(key(b), key(a)) }
}

// appendCmp copies before appending so that branching a chain — building two
// different ThenBy chains from one OrderedQuery — cannot alias the slice.
func appendCmp[T any](existing []func(a, b T) int, add func(a, b T) int) []func(a, b T) int {
	out := make([]func(a, b T) int, 0, len(existing)+1)
	out = append(out, existing...)
	return append(out, add)
}
