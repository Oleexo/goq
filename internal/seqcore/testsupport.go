package seqcore

import "iter"

// Counter is a source that records how many elements were pulled from it.
// Tests use it to assert that an operator is lazy.
type Counter struct{ pulls int }

// Seq returns an iterator over 0..n-1 that counts each element it yields.
func (c *Counter) Seq(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := range n {
			c.pulls++
			if !yield(i) {
				return
			}
		}
	}
}

// Pulls reports how many elements have been pulled from sequences created by
// Seq. It is not safe for concurrent use.
func (c *Counter) Pulls() int { return c.pulls }

// Infinite returns an unbounded iterator over 0, 1, 2, ... A consumer that
// fails to stop will hang, which is the intended signal: an operator asserted
// to be lazy must terminate against this source.
func Infinite() iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := 0; ; i++ {
			if !yield(i) {
				return
			}
		}
	}
}
