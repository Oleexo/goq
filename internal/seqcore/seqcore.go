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

// Take yields at most n leading elements of s. A non-positive n yields nothing.
func Take[T any](s iter.Seq[T], n int) iter.Seq[T] {
	return func(yield func(T) bool) {
		if n <= 0 {
			return
		}
		i := 0
		for v := range s {
			if !yield(v) {
				return
			}
			i++
			if i >= n {
				return
			}
		}
	}
}

// TakeWhile yields leading elements of s while pred holds, stopping at the
// first element for which it does not.
func TakeWhile[T any](s iter.Seq[T], pred func(T) bool) iter.Seq[T] {
	return func(yield func(T) bool) {
		for v := range s {
			if !pred(v) || !yield(v) {
				return
			}
		}
	}
}

// Skip discards at most n leading elements of s and yields the rest.
func Skip[T any](s iter.Seq[T], n int) iter.Seq[T] {
	return func(yield func(T) bool) {
		i := 0
		for v := range s {
			if i < n {
				i++
				continue
			}
			if !yield(v) {
				return
			}
		}
	}
}

// SkipWhile discards leading elements of s while pred holds, then yields every
// remaining element including the first failure.
func SkipWhile[T any](s iter.Seq[T], pred func(T) bool) iter.Seq[T] {
	return func(yield func(T) bool) {
		skipping := true
		for v := range s {
			if skipping {
				if pred(v) {
					continue
				}
				skipping = false
			}
			if !yield(v) {
				return
			}
		}
	}
}

// TakeLast yields the final n elements of s. It reads s to the end but retains
// at most n elements, so it never returns on an unbounded source.
func TakeLast[T any](s iter.Seq[T], n int) iter.Seq[T] {
	return func(yield func(T) bool) {
		if n <= 0 {
			return
		}
		buf := make([]T, 0, n)
		for v := range s {
			if len(buf) == n {
				buf = append(buf[:0], buf[1:]...)
			}
			buf = append(buf, v)
		}
		for _, v := range buf {
			if !yield(v) {
				return
			}
		}
	}
}

// SkipLast yields every element of s except the final n. It retains at most n
// elements at a time.
func SkipLast[T any](s iter.Seq[T], n int) iter.Seq[T] {
	return func(yield func(T) bool) {
		if n <= 0 {
			for v := range s {
				if !yield(v) {
					return
				}
			}
			return
		}
		buf := make([]T, 0, n+1)
		for v := range s {
			buf = append(buf, v)
			if len(buf) > n {
				if !yield(buf[0]) {
					return
				}
				buf = append(buf[:0], buf[1:]...)
			}
		}
	}
}
