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
//
// The zero OrderedQuery yields no elements.
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
// whose ordering is not expressible as a cmp.Ordered key. Sorting is stable:
// elements comparing equal keep their source order.
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

// Seq returns the sorted pipeline as an iterator. Each call to Seq re-collects
// and re-sorts the source, so enumerate only once or use Memoize if the result
// must be reused.
//
// The zero OrderedQuery has a nil source and yields no elements, matching
// Query's zero value.
func (o OrderedQuery[T]) Seq() iter.Seq[T] {
	return func(yield func(T) bool) {
		src := o.src
		if src == nil {
			src = func(func(T) bool) {}
		}
		buf := slices.Collect(src)
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

// appendCmp returns a new comparator slice with add appended, copying rather
// than appending in place so that two ThenBy chains branched from the same
// OrderedQuery cannot alias each other's comparators.
//
// Today that copy is belt-and-braces: every slice this produces has
// len == cap, and OrderBy seeds one with len == cap == 1, so an in-place
// append would reallocate anyway. Keep the copy — it is what makes the
// guarantee hold if a future change ever introduces spare capacity.
func appendCmp[T any](existing []func(a, b T) int, add func(a, b T) int) []func(a, b T) int {
	out := make([]func(a, b T) int, 0, len(existing)+1)
	out = append(out, existing...)
	return append(out, add)
}
