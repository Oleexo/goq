package goq

// First returns the first element and true, or the zero value and false if the
// source is empty. It pulls exactly one element.
//
// C#'s FirstOrDefault is the same call with the bool discarded:
//
//	v, _ := q.First()
func (q Query[T]) First() (T, bool) {
	for v := range q.Seq() {
		return v, true
	}
	var zero T
	return zero, false
}

// Last returns the final element and true, or the zero value and false if the
// source is empty. It enumerates the whole source and therefore never returns
// on an unbounded one.
func (q Query[T]) Last() (T, bool) {
	var (
		last  T
		found bool
	)
	for v := range q.Seq() {
		last, found = v, true
	}
	return last, found
}

// Single returns the only element of the source.
//
// It returns ErrEmpty if there are none and ErrMultiple if there are two or
// more; unlike C#, which throws a different exception for each, the two cases
// are distinguished by the sentinel rather than by type. Single stops pulling
// as soon as a second element appears.
func (q Query[T]) Single() (T, error) {
	var (
		result T
		found  bool
	)
	for v := range q.Seq() {
		if found {
			var zero T
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

// ElementAt returns the element at the given zero-based index and true, or the
// zero value and false if the index is negative or beyond the end.
func (q Query[T]) ElementAt(i int) (T, bool) {
	if i < 0 {
		var zero T
		return zero, false
	}
	n := 0
	for v := range q.Seq() {
		if n == i {
			return v, true
		}
		n++
	}
	var zero T
	return zero, false
}

// Any reports whether the source yields at least one element. It pulls at most
// one.
func (q Query[T]) Any() bool {
	for range q.Seq() {
		return true
	}
	return false
}

// AnyWhere reports whether any element satisfies pred, stopping at the first
// match.
func (q Query[T]) AnyWhere(pred func(T) bool) bool {
	for v := range q.Seq() {
		if pred(v) {
			return true
		}
	}
	return false
}

// All reports whether every element satisfies pred, stopping at the first that
// does not. It returns true for an empty source, matching C#.
func (q Query[T]) All(pred func(T) bool) bool {
	for v := range q.Seq() {
		if !pred(v) {
			return false
		}
	}
	return true
}

// Contains reports whether q yields v, stopping at the first match.
//
// It is a function rather than a method because a method cannot require that
// Query's element type be comparable.
func Contains[T comparable](q Query[T], v T) bool {
	for got := range q.Seq() {
		if got == v {
			return true
		}
	}
	return false
}

// SequenceEqual reports whether a and b yield equal elements in the same order
// and have the same length.
//
// It is a function rather than a method because a method cannot require that
// Query's element type be comparable.
func SequenceEqual[T comparable](a, b Query[T]) bool {
	next, stop := iterPull(b.Seq())
	defer stop()
	for va := range a.Seq() {
		vb, ok := next()
		if !ok || va != vb {
			return false
		}
	}
	_, extra := next()
	return !extra
}
