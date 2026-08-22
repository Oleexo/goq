package goq

import (
	"iter"

	"github.com/oleexo/goq/internal/seqcore"
)

// Select yields f applied to each element, changing the element type. It
// streams, calling f once per element actually consumed.
func (q Query[T]) Select[R any](f func(T) R) Query[R] {
	return Query[R]{seq: seqcore.Map(q.Seq(), f)}
}

// SelectIndex is Select with a zero-based element index.
func (q Query[T]) SelectIndex[R any](f func(int, T) R) Query[R] {
	return Query[R]{seq: seqcore.MapIndex(q.Seq(), f)}
}

// SelectMany flattens the slice each element projects to. It streams: the slice
// for one element is produced only when that element is reached.
func (q Query[T]) SelectMany[R any](f func(T) []R) Query[R] {
	return Query[R]{seq: seqcore.FlatMap(q.Seq(), func(v T) iter.Seq[R] {
		items := f(v)
		return func(yield func(R) bool) {
			for _, r := range items {
				if !yield(r) {
					return
				}
			}
		}
	})}
}

// SelectManySeq flattens the iterator each element projects to, for callers
// whose inner sequence is itself lazy or unbounded.
func (q Query[T]) SelectManySeq[R any](f func(T) iter.Seq[R]) Query[R] {
	return Query[R]{seq: seqcore.FlatMap(q.Seq(), f)}
}

// Zip pairs elements positionally with other and yields f applied to each pair,
// stopping as soon as either sequence is exhausted. It streams.
func (q Query[T]) Zip[U, R any](other Query[U], f func(T, U) R) Query[R] {
	return Query[R]{seq: seqcore.Zip(q.Seq(), other.Seq(), f)}
}
