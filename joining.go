package goq

// Join correlates elements of two pipelines on equal keys, yielding sel applied
// to each matching pair. It is an inner join: outer elements with no match, and
// inner elements never matched, are both dropped.
//
// The inner pipeline is fully buffered into a lookup before the first result is
// yielded; the outer pipeline streams. An outer element matching several inner
// elements yields one result per pair, in inner-source order.
func (q Query[T]) Join[U any, K comparable, R any](
	inner Query[U],
	outerKey func(T) K,
	innerKey func(U) K,
	sel func(T, U) R,
) Query[R] {
	outer := q.Seq()
	return Query[R]{seq: func(yield func(R) bool) {
		lookup := inner.ToLookup(innerKey)
		for o := range outer {
			for _, i := range lookup[outerKey(o)] {
				if !yield(sel(o, i)) {
					return
				}
			}
		}
	}}
}

// GroupJoin correlates elements of two pipelines on equal keys, yielding sel
// applied to each outer element together with all of its matches.
//
// It is a left outer join: an outer element with no matches is still yielded,
// with an empty (possibly nil) slice. The inner pipeline is fully buffered; the outer streams.
// The slice passed to sel must not be retained or mutated by the caller: it is backed by
// the lookup's internal storage, and all outer rows sharing the same key receive the same
// backing array, so mutations are visible across them.
func (q Query[T]) GroupJoin[U any, K comparable, R any](
	inner Query[U],
	outerKey func(T) K,
	innerKey func(U) K,
	sel func(T, []U) R,
) Query[R] {
	outer := q.Seq()
	return Query[R]{seq: func(yield func(R) bool) {
		lookup := inner.ToLookup(innerKey)
		for o := range outer {
			if !yield(sel(o, lookup[outerKey(o)])) {
				return
			}
		}
	}}
}
