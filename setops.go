package goq

import "iter"

// Concat yields every element of q followed by every element of other. It does
// not remove duplicates; use Union for that. It streams.
func (q Query[T]) Concat(other Query[T]) Query[T] {
	first, second := q.Seq(), other.Seq()
	return Query[T]{seq: func(yield func(T) bool) {
		for v := range first {
			if !yield(v) {
				return
			}
		}
		for v := range second {
			if !yield(v) {
				return
			}
		}
	}}
}

// DistinctBy yields the first element for each distinct key, in first-appearance
// order. It streams, retaining only the keys seen so far.
//
// To deduplicate a Query whose elements are themselves comparable, use the
// package-level Distinct: a method cannot constrain its receiver's element type.
func (q Query[T]) DistinctBy[K comparable](key func(T) K) Query[T] {
	return Query[T]{seq: distinctSeq(q.Seq(), key)}
}

// UnionBy yields the distinct elements of q followed by those of other whose
// keys did not already appear, in first-appearance order. It streams,
// retaining only a key set rather than the source.
//
// To deduplicate a Query of comparable elements, use the package-level Union:
// a method cannot constrain its receiver's element type.
func (q Query[T]) UnionBy[K comparable](other Query[T], key func(T) K) Query[T] {
	return q.Concat(other).DistinctBy(key)
}

// IntersectBy yields the distinct elements of q whose key also appears in other.
//
// It buffers other's keys before yielding, then streams q.
//
// To intersect a Query of comparable elements, use the package-level Intersect:
// a method cannot constrain its receiver's element type.
func (q Query[T]) IntersectBy[K comparable](other Query[T], key func(T) K) Query[T] {
	src, otherSeq := q.Seq(), other.Seq()
	return Query[T]{seq: func(yield func(T) bool) {
		keep := keySet(otherSeq, key)
		seen := make(map[K]struct{})
		for v := range src {
			k := key(v)
			if _, ok := keep[k]; !ok {
				continue
			}
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			if !yield(v) {
				return
			}
		}
	}}
}

// ExceptBy yields the distinct elements of q whose key does not appear in other.
//
// It buffers other's keys before yielding, then streams q.
//
// To compute the set difference of comparable elements, use the package-level Except:
// a method cannot constrain its receiver's element type.
func (q Query[T]) ExceptBy[K comparable](other Query[T], key func(T) K) Query[T] {
	src, otherSeq := q.Seq(), other.Seq()
	return Query[T]{seq: func(yield func(T) bool) {
		drop := keySet(otherSeq, key)
		seen := make(map[K]struct{})
		for v := range src {
			k := key(v)
			if _, skip := drop[k]; skip {
				continue
			}
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			if !yield(v) {
				return
			}
		}
	}}
}

// Distinct yields the distinct elements in first-appearance order. It streams,
// retaining only the elements seen so far.
//
// To deduplicate a Query of structs by a field or computed key, use the method
// DistinctBy: a method cannot constrain its receiver's element type.
func Distinct[T comparable](q Query[T]) Query[T] {
	return Query[T]{seq: distinctSeq(q.Seq(), func(v T) T { return v })}
}

// Union yields the distinct elements of a followed by those of b that did not
// already appear. It streams, retaining only a key set rather than the source.
//
// To union with a custom key function, use the method UnionBy: a method cannot
// constrain its receiver's element type.
func Union[T comparable](a, b Query[T]) Query[T] {
	return Distinct(a.Concat(b))
}

// Intersect yields the distinct elements of a that also appear in b. It buffers
// b, then streams a.
//
// To intersect with a custom key function, use the method IntersectBy: a method
// cannot constrain its receiver's element type.
func Intersect[T comparable](a, b Query[T]) Query[T] {
	return a.IntersectBy(b, func(v T) T { return v })
}

// Except yields the distinct elements of a that do not appear in b. It buffers
// b, then streams a.
//
// To compute set difference with a custom key function, use the method ExceptBy:
// a method cannot constrain its receiver's element type.
func Except[T comparable](a, b Query[T]) Query[T] {
	return a.ExceptBy(b, func(v T) T { return v })
}

func distinctSeq[T any, K comparable](s iter.Seq[T], key func(T) K) iter.Seq[T] {
	return func(yield func(T) bool) {
		seen := make(map[K]struct{})
		for v := range s {
			k := key(v)
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			if !yield(v) {
				return
			}
		}
	}
}

func keySet[T any, K comparable](s iter.Seq[T], key func(T) K) map[K]struct{} {
	out := make(map[K]struct{})
	for v := range s {
		out[key(v)] = struct{}{}
	}
	return out
}
