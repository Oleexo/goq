// Package seqcore holds goq's infallible operator logic as free functions
// over iter.Seq. The fallible pipeline (TryQuery) builds its own iter.Seq2
// stages rather than sharing this package.
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
//
// The n most recent elements are tracked in a fixed-size ring buffer, so each
// incoming element costs O(1) rather than shifting the whole buffer: filling
// and draining the buffer is O(n) total, not O(n) per element.
func TakeLast[T any](s iter.Seq[T], n int) iter.Seq[T] {
	return func(yield func(T) bool) {
		if n <= 0 {
			return
		}
		buf := make([]T, n)
		count := 0 // number of elements written, capped at n
		next := 0  // ring index the next element will be written to
		for v := range s {
			buf[next] = v
			next = (next + 1) % n
			if count < n {
				count++
			}
		}
		// count < n: the buffer never wrapped, so the oldest retained element
		// is buf[0]. count == n: it wrapped at least once, so the oldest
		// retained element is the one about to be overwritten next, at
		// buf[next].
		start := 0
		if count == n {
			start = next
		}
		for i := range count {
			if !yield(buf[(start+i)%n]) {
				return
			}
		}
	}
}

// SkipLast yields every element of s except the final n. It retains at most n
// elements at a time.
//
// It is a delay line implemented as a fixed-size ring buffer: each incoming
// element costs O(1) — one slot read (if the buffer is already full) followed
// by one slot write — rather than shifting the whole buffer.
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
		buf := make([]T, n)
		count := 0 // number of elements written so far
		idx := 0   // ring index the next element will be written to
		for v := range s {
			if count >= n {
				// The buffer is full: the slot about to be overwritten holds
				// the element from n steps ago, which is now safe to yield.
				if !yield(buf[idx]) {
					return
				}
			}
			buf[idx] = v
			idx = (idx + 1) % n
			count++
		}
	}
}

// FlatMap yields every element of every sequence produced by f, lazily.
func FlatMap[T, R any](s iter.Seq[T], f func(T) iter.Seq[R]) iter.Seq[R] {
	return func(yield func(R) bool) {
		for v := range s {
			for r := range f(v) {
				if !yield(r) {
					return
				}
			}
		}
	}
}

// MapIndex is Map with a zero-based element index.
func MapIndex[T, R any](s iter.Seq[T], f func(int, T) R) iter.Seq[R] {
	return func(yield func(R) bool) {
		i := 0
		for v := range s {
			if !yield(f(i, v)) {
				return
			}
			i++
		}
	}
}

// Zip yields f applied to positionally paired elements of a and b, stopping
// when either is exhausted.
//
// It pulls from b using iter.Pull and always calls the returned stop function,
// including when the consumer breaks early; failing to do so leaks a coroutine.
func Zip[A, B, R any](a iter.Seq[A], b iter.Seq[B], f func(A, B) R) iter.Seq[R] {
	return func(yield func(R) bool) {
		next, stop := iter.Pull(b)
		defer stop()
		for va := range a {
			vb, ok := next()
			if !ok {
				return
			}
			if !yield(f(va, vb)) {
				return
			}
		}
	}
}
