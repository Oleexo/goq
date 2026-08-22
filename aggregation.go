package goq

import "cmp"

// Numeric is the set of types the arithmetic aggregations accept.
type Numeric interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// Aggregate folds the source into a single value, starting from seed. It
// returns seed unchanged for an empty source. The accumulator type may differ
// from the element type.
//
// It streams in O(1) memory, holding only the running accumulator, but it
// must reach the end of the source to produce a final value, so it never
// returns on an unbounded source.
func (q Query[T]) Aggregate[A any](seed A, fn func(A, T) A) A {
	acc := seed
	for v := range q.Seq() {
		acc = fn(acc, v)
	}
	return acc
}

// Count reports the number of elements. It enumerates the whole source.
func (q Query[T]) Count() int {
	n := 0
	for range q.Seq() {
		n++
	}
	return n
}

// Sum adds sel applied to each element, mirroring C#'s Sum(x => x.Price). It
// returns the zero value for an empty source. Integer overflow wraps rather than
// erroring.
//
// It streams in O(1) memory, holding only the running total, but it must
// reach the end of the source to produce a final value, so it never returns
// on an unbounded source.
//
// To sum a Query whose elements are themselves numeric, use the package-level
// Sum: a method cannot constrain its receiver's element type.
func (q Query[T]) Sum[N Numeric](sel func(T) N) N {
	var total N
	for v := range q.Seq() {
		total += sel(v)
	}
	return total
}

// Average returns the mean of sel applied to each element, and false if the
// source is empty. For numeric types where the sum exceeds float precision,
// the result may lose precision.
//
// It streams in O(1) memory, holding only the running total and count, but it
// must reach the end of the source to produce a final value, so it never
// returns on an unbounded source.
//
// To average a Query whose elements are themselves numeric, use the package-level
// Average: a method cannot constrain its receiver's element type.
func (q Query[T]) Average[N Numeric](sel func(T) N) (float64, bool) {
	var total N
	n := 0
	for v := range q.Seq() {
		total += sel(v)
		n++
	}
	if n == 0 {
		return 0, false
	}
	return float64(total) / float64(n), true
}

// MinBy returns the element with the smallest key, and false if the source is
// empty. Ties resolve to the first element encountered.
//
// It streams in O(1) memory, holding only the best element seen so far, but it
// must reach the end of the source to confirm nothing smaller follows, so it
// never returns on an unbounded source.
//
// To find the minimum of a Query whose elements are themselves ordered, use
// the package-level Min: a method cannot constrain its receiver's element type.
func (q Query[T]) MinBy[K cmp.Ordered](key func(T) K) (T, bool) {
	var (
		best  T
		bestK K
		found bool
	)
	for v := range q.Seq() {
		k := key(v)
		if !found || k < bestK {
			best, bestK, found = v, k, true
		}
	}
	return best, found
}

// MaxBy returns the element with the largest key, and false if the source is
// empty. Ties resolve to the first element encountered.
//
// It streams in O(1) memory, holding only the best element seen so far, but it
// must reach the end of the source to confirm nothing larger follows, so it
// never returns on an unbounded source.
//
// To find the maximum of a Query whose elements are themselves ordered, use
// the package-level Max: a method cannot constrain its receiver's element type.
func (q Query[T]) MaxBy[K cmp.Ordered](key func(T) K) (T, bool) {
	var (
		best  T
		bestK K
		found bool
	)
	for v := range q.Seq() {
		k := key(v)
		if !found || k > bestK {
			best, bestK, found = v, k, true
		}
	}
	return best, found
}

// Sum adds every element, returning the zero value for an empty source.
// Integer overflow wraps rather than erroring.
//
// It streams in O(1) memory, holding only the running total, but it must
// reach the end of the source to produce a final value, so it never returns
// on an unbounded source.
//
// To sum a Query of structs where a field is numeric, use the method form
// which takes a selector: q.Sum(func(x) x.Price).
func Sum[N Numeric](q Query[N]) N {
	var total N
	for v := range q.Seq() {
		total += v
	}
	return total
}

// Average returns the mean of the elements, and false if the source is empty.
// For numeric types where the sum exceeds float precision, the result may lose precision.
//
// It streams in O(1) memory, holding only the running total and count, but it
// must reach the end of the source to produce a final value, so it never
// returns on an unbounded source.
//
// To average a Query of structs where a field is numeric, use the method form
// which takes a selector: q.Average(func(x) x.Price).
func Average[N Numeric](q Query[N]) (float64, bool) {
	var total N
	n := 0
	for v := range q.Seq() {
		total += v
		n++
	}
	if n == 0 {
		return 0, false
	}
	return float64(total) / float64(n), true
}

// Min returns the smallest element, and false if the source is empty.
//
// It streams in O(1) memory, holding only the smallest element seen so far,
// but it must reach the end of the source to confirm nothing smaller follows,
// so it never returns on an unbounded source.
//
// To find the minimum of a Query of structs based on a field, use the method form
// MinBy which takes a key selector: q.MinBy(func(x) x.Price).
func Min[T cmp.Ordered](q Query[T]) (T, bool) {
	var (
		best  T
		found bool
	)
	for v := range q.Seq() {
		if !found || v < best {
			best, found = v, true
		}
	}
	return best, found
}

// Max returns the largest element, and false if the source is empty.
//
// It streams in O(1) memory, holding only the largest element seen so far,
// but it must reach the end of the source to confirm nothing larger follows,
// so it never returns on an unbounded source.
//
// To find the maximum of a Query of structs based on a field, use the method form
// MaxBy which takes a key selector: q.MaxBy(func(x) x.Price).
func Max[T cmp.Ordered](q Query[T]) (T, bool) {
	var (
		best  T
		found bool
	)
	for v := range q.Seq() {
		if !found || v > best {
			best, found = v, true
		}
	}
	return best, found
}
