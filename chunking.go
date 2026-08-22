package goq

import (
	"iter"
	"slices"
)

// ChunkQuery is a pipeline of fixed-size batches, as produced by Chunk.
//
// It is a distinct type rather than a Query[[]T] because Query[[]T] would need
// Query[[][]T] and so on without limit, which Go rejects as an instantiation
// cycle. It therefore has no AsQuery method: leave it through Select, which
// introduces a fresh element type, or through a terminal such as ToSlice.
type ChunkQuery[T any] struct {
	src iter.Seq[[]T]
}

// Chunk batches the elements into slices of at most size elements. The final
// batch is shorter if the source does not divide evenly. A size of zero or less
// yields nothing.
//
// Chunk streams: each batch is yielded as soon as it fills, so it works on an
// unbounded source. Every batch has its own backing array.
func (q Query[T]) Chunk(size int) ChunkQuery[T] {
	seq := q.Seq()
	return ChunkQuery[T]{src: func(yield func([]T) bool) {
		if size <= 0 {
			return
		}
		batch := make([]T, 0, size)
		for v := range seq {
			batch = append(batch, v)
			if len(batch) == size {
				if !yield(batch) {
					return
				}
				batch = make([]T, 0, size)
			}
		}
		if len(batch) > 0 {
			yield(batch)
		}
	}}
}

// Where yields the batches for which pred returns true.
func (c ChunkQuery[T]) Where(pred func([]T) bool) ChunkQuery[T] {
	src := c.src
	return ChunkQuery[T]{src: func(yield func([]T) bool) {
		for batch := range src {
			if pred(batch) && !yield(batch) {
				return
			}
		}
	}}
}

// Select yields f applied to each batch, returning a Query and so re-entering
// the full operator set.
func (c ChunkQuery[T]) Select[R any](f func([]T) R) Query[R] {
	return Query[R]{seq: func(yield func(R) bool) {
		for batch := range c.src {
			if !yield(f(batch)) {
				return
			}
		}
	}}
}

// Seq returns the batches as an iterator.
func (c ChunkQuery[T]) Seq() iter.Seq[[]T] { return c.src }

// ToSlice materialises the batches into a new slice of slices.
func (c ChunkQuery[T]) ToSlice() [][]T {
	out := slices.Collect(c.src)
	if out == nil {
		out = [][]T{}
	}
	return out
}

// Count reports the number of batches.
func (c ChunkQuery[T]) Count() int {
	n := 0
	for range c.src {
		n++
	}
	return n
}
