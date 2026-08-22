// Package seqcore holds goq's operator logic as free functions over iter.Seq
// and iter.Seq2.
//
// It exists so that each operator is implemented once and shared by every
// pipeline type. It is internal: nothing here is part of goq's public API.
package seqcore

import "iter"

// Map yields f applied to each element of s, lazily.
func Map[T, R any](s iter.Seq[T], f func(T) R) iter.Seq[R] {
	return func(yield func(R) bool) {
		for v := range s {
			if !yield(f(v)) {
				return
			}
		}
	}
}

// Filter yields the elements of s for which pred returns true, lazily.
func Filter[T any](s iter.Seq[T], pred func(T) bool) iter.Seq[T] {
	return func(yield func(T) bool) {
		for v := range s {
			if pred(v) && !yield(v) {
				return
			}
		}
	}
}
