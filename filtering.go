package goq

import "github.com/oleexo/goq/internal/seqcore"

// Where yields the elements for which pred returns true. It streams.
func (q Query[T]) Where(pred func(T) bool) Query[T] {
	return Query[T]{seq: seqcore.Filter(q.Seq(), pred)}
}

// Take yields at most the first n elements. A non-positive n yields nothing.
// It streams, and pulls no more than n elements from the source.
func (q Query[T]) Take(n int) Query[T] {
	return Query[T]{seq: seqcore.Take(q.Seq(), n)}
}

// TakeWhile yields leading elements while pred holds, stopping at the first
// element for which it does not. That element is not yielded. It streams.
func (q Query[T]) TakeWhile(pred func(T) bool) Query[T] {
	return Query[T]{seq: seqcore.TakeWhile(q.Seq(), pred)}
}

// TakeLast yields the final n elements.
//
// It must read the source to the end, retaining at most n elements, so it never
// yields on an unbounded source such as one from FromChan.
func (q Query[T]) TakeLast(n int) Query[T] {
	return Query[T]{seq: seqcore.TakeLast(q.Seq(), n)}
}

// Skip discards the first n elements and yields the rest. It streams.
func (q Query[T]) Skip(n int) Query[T] {
	return Query[T]{seq: seqcore.Skip(q.Seq(), n)}
}

// SkipWhile discards leading elements while pred holds, then yields every
// remaining element including the first for which pred failed. It streams.
func (q Query[T]) SkipWhile(pred func(T) bool) Query[T] {
	return Query[T]{seq: seqcore.SkipWhile(q.Seq(), pred)}
}

// SkipLast yields every element except the final n, retaining at most n
// elements at a time. It streams with a bounded buffer.
func (q Query[T]) SkipLast(n int) Query[T] {
	return Query[T]{seq: seqcore.SkipLast(q.Seq(), n)}
}
