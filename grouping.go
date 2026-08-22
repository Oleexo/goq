package goq

import (
	"cmp"
	"iter"
	"slices"
)

// Group is a key and the elements that share it, as produced by GroupBy.
type Group[K comparable, V any] struct {
	// Key is the value the grouping function returned for every item.
	Key K
	// Items are the source elements in the group, in source order. The slice is
	// the caller's to read and mutate; mutations affect only that caller's copy.
	Items []V
}

// GroupQuery is a pipeline of groups.
//
// It is a distinct type rather than a Query[Group[K,T]] because Query has
// GroupBy, and a Query of groups would need a Query of groups of groups,
// without limit; Go rejects that as an instantiation cycle. GroupQuery
// therefore has no AsQuery method. Leave it through Select, which introduces a
// fresh element type, or through a terminal such as ToSlice. To group an
// already-grouped pipeline, use the package-level GroupBy function.
//
// Grouping is a buffering operation: the source is fully consumed before the
// first group is yielded.
//
// The zero GroupQuery yields no groups.
type GroupQuery[K comparable, T any] struct {
	src  iter.Seq[Group[K, T]]
	cmps []func(a, b Group[K, T]) int
}

// srcSeq returns g's source, treating a nil source — as found in the zero
// GroupQuery — as an empty sequence rather than a nil iter.Seq, which would
// panic when ranged over.
func (g GroupQuery[K, T]) srcSeq() iter.Seq[Group[K, T]] {
	if g.src == nil {
		return func(func(Group[K, T]) bool) {}
	}
	return g.src
}

// GroupBy groups the elements by key. Groups are yielded in order of first
// appearance, and the items within each group keep their source order.
//
// It buffers the entire source, so it never yields on an unbounded source.
func (q Query[T]) GroupBy[K comparable](key func(T) K) GroupQuery[K, T] {
	return GroupQuery[K, T]{src: groupSeq(q.Seq(), key)}
}

// GroupBySelect groups the elements by key and projects each group, returning a
// Query so the chain continues normally. It buffers the entire source, so it
// never yields on an unbounded source.
func (q Query[T]) GroupBySelect[K comparable, R any](
	key func(T) K, sel func(Group[K, T]) R,
) Query[R] {
	return Query[R]{seq: func(yield func(R) bool) {
		for g := range groupSeq(q.Seq(), key) {
			if !yield(sel(g)) {
				return
			}
		}
	}}
}

// ToLookup indexes every element by key, keeping all elements per key. Unlike
// ToMap it cannot fail, because duplicate keys are the point.
//
// It fully materialises the source into the returned map, so it never returns
// on an unbounded source.
func (q Query[T]) ToLookup[K comparable](key func(T) K) map[K][]T {
	out := make(map[K][]T)
	for v := range q.Seq() {
		k := key(v)
		out[k] = append(out[k], v)
	}
	return out
}

// Where yields the groups for which pred returns true.
func (g GroupQuery[K, T]) Where(pred func(Group[K, T]) bool) GroupQuery[K, T] {
	src := g.srcSeq()
	return GroupQuery[K, T]{cmps: g.cmps, src: func(yield func(Group[K, T]) bool) {
		for v := range src {
			if pred(v) && !yield(v) {
				return
			}
		}
	}}
}

// OrderBy sorts the groups ascending by key. It returns a GroupQuery rather
// than an OrderedQuery: the latter would reintroduce the instantiation cycle
// GroupQuery exists to avoid. Chain ThenBy for tie-breakers.
//
// OrderBy resets any comparators accumulated by earlier ThenBy/ThenByDesc
// calls rather than appending to them — "sort by this instead," matching
// Query.OrderBy. A later OrderBy call does not become a tie-breaker for an
// earlier one; call ThenBy for that.
func (g GroupQuery[K, T]) OrderBy[K2 cmp.Ordered](key func(Group[K, T]) K2) GroupQuery[K, T] {
	return GroupQuery[K, T]{src: g.src, cmps: []func(a, b Group[K, T]) int{ascending(key)}}
}

// OrderByDesc sorts the groups descending by key.
func (g GroupQuery[K, T]) OrderByDesc[K2 cmp.Ordered](key func(Group[K, T]) K2) GroupQuery[K, T] {
	return GroupQuery[K, T]{src: g.src, cmps: []func(a, b Group[K, T]) int{descending(key)}}
}

// ThenBy adds an ascending tie-breaking key for the group ordering.
//
// Unlike Query, where ThenBy exists only on OrderedQuery and so is
// unreachable without a preceding OrderBy, GroupQuery has no separate ordered
// type: ThenBy is a plain method here, so From(xs).GroupBy(k).ThenBy(f)
// compiles even with no preceding OrderBy. Called with no preceding OrderBy,
// ThenBy silently behaves as OrderBy: there are no earlier comparators for it
// to break ties on.
func (g GroupQuery[K, T]) ThenBy[K2 cmp.Ordered](key func(Group[K, T]) K2) GroupQuery[K, T] {
	return GroupQuery[K, T]{src: g.src, cmps: appendCmp(g.cmps, ascending(key))}
}

// ThenByDesc adds a descending tie-breaking key for the group ordering.
func (g GroupQuery[K, T]) ThenByDesc[K2 cmp.Ordered](key func(Group[K, T]) K2) GroupQuery[K, T] {
	return GroupQuery[K, T]{src: g.src, cmps: appendCmp(g.cmps, descending(key))}
}

// Seq returns the groups as an iterator, applying any pending ordering.
//
// The zero GroupQuery has a nil source and yields no groups, matching
// Query's zero value.
func (g GroupQuery[K, T]) Seq() iter.Seq[Group[K, T]] {
	if len(g.cmps) == 0 {
		return g.srcSeq()
	}
	return func(yield func(Group[K, T]) bool) {
		buf := slices.Collect(g.srcSeq())
		slices.SortStableFunc(buf, func(a, b Group[K, T]) int {
			for _, c := range g.cmps {
				if r := c(a, b); r != 0 {
					return r
				}
			}
			return 0
		})
		for _, v := range buf {
			if !yield(v) {
				return
			}
		}
	}
}

// Select yields f applied to each group, returning a Query and so re-entering
// the full operator set.
func (g GroupQuery[K, T]) Select[R any](f func(Group[K, T]) R) Query[R] {
	return Query[R]{seq: func(yield func(R) bool) {
		for v := range g.Seq() {
			if !yield(f(v)) {
				return
			}
		}
	}}
}

// ToSlice materialises the groups into a new slice.
func (g GroupQuery[K, T]) ToSlice() []Group[K, T] {
	out := slices.Collect(g.Seq())
	if out == nil {
		out = []Group[K, T]{}
	}
	return out
}

// Count reports the number of groups.
func (g GroupQuery[K, T]) Count() int {
	n := 0
	for range g.Seq() {
		n++
	}
	return n
}

// GroupBy groups an already-grouped pipeline by a key derived from each group.
//
// It is a function rather than a method on GroupQuery because a method whose
// result element type derives from the receiver's is an instantiation cycle;
// a function is instantiated per call site and so terminates.
//
// It buffers the entire source, so it never yields on an unbounded source.
func GroupBy[K comparable, T any, K2 comparable](
	g GroupQuery[K, T], key func(Group[K, T]) K2,
) GroupQuery[K2, Group[K, T]] {
	return GroupQuery[K2, Group[K, T]]{src: groupSeq(g.Seq(), key)}
}

// groupSeq is the shared grouping engine: it buffers the source, then yields
// one group per distinct key in order of first appearance.
func groupSeq[T any, K comparable](s iter.Seq[T], key func(T) K) iter.Seq[Group[K, T]] {
	return func(yield func(Group[K, T]) bool) {
		var order []K
		byKey := make(map[K][]T)
		for v := range s {
			k := key(v)
			if _, seen := byKey[k]; !seen {
				order = append(order, k)
			}
			byKey[k] = append(byKey[k], v)
		}
		for _, k := range order {
			if !yield(Group[K, T]{Key: k, Items: byKey[k]}) {
				return
			}
		}
	}
}
