package goq

import (
	"slices"
	"sync"
)

// ToMap indexes the elements by key.
//
// It returns ErrDuplicateKey, and a nil map, if two elements produce the same
// key: a collision means the caller's uniqueness assumption is wrong, which is
// worth reporting rather than silently discarding a row. Use ToMapLast to
// overwrite deliberately.
//
// ToMap enumerates the whole source.
func (q Query[T]) ToMap[K comparable](key func(T) K) (map[K]T, error) {
	out := make(map[K]T)
	for v := range q.Seq() {
		k := key(v)
		if _, exists := out[k]; exists {
			return nil, ErrDuplicateKey
		}
		out[k] = v
	}
	return out, nil
}

// ToMapLast indexes the elements by key, with later elements overwriting
// earlier ones on collision. Use ToMap to detect collisions instead.
//
// It fully materialises the source into the returned map, so it never returns
// on an unbounded source.
func (q Query[T]) ToMapLast[K comparable](key func(T) K) map[K]T {
	out := make(map[K]T)
	for v := range q.Seq() {
		out[key(v)] = v
	}
	return out
}

// Memoize returns a Query that caches the elements on first enumeration and
// replays them afterwards.
//
// It makes any query re-enumerable, including one over a single-shot source
// such as FromChan. The cost is that every element is retained, so it must not
// be used on an unbounded source. The underlying source is enumerated at most
// once, even under concurrent use.
//
// If the source panics during the first enumeration, the panic propagates to
// that caller, but the cache is left empty and considered populated: every
// later enumeration of this Query then yields nothing rather than retrying.
func (q Query[T]) Memoize() Query[T] {
	var (
		once   sync.Once
		cached []T
	)
	return Query[T]{seq: func(yield func(T) bool) {
		once.Do(func() { cached = slices.Collect(q.Seq()) })
		for _, v := range cached {
			if !yield(v) {
				return
			}
		}
	}}
}

// ToSet collects the distinct elements into a set.
//
// It fully materialises the source into the returned map, so it never returns
// on an unbounded source.
//
// It is a function rather than a method because a method cannot require that
// Query's element type be comparable.
func ToSet[T comparable](q Query[T]) map[T]struct{} {
	out := make(map[T]struct{})
	for v := range q.Seq() {
		out[v] = struct{}{}
	}
	return out
}
