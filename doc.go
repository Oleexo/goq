// Package goq provides a lazy, type-safe query pipeline over in-memory
// collections, modelled on LINQ-to-Objects.
//
// A query is built by chaining operators onto a source and is not executed
// until a terminal operator consumes it:
//
//	names := goq.From(people).
//		Where(func(p Person) bool { return p.Age >= 18 }).
//		Select(func(p Person) string { return p.Name }).
//		ToSlice()
//
// # Pipeline types
//
// Query is infallible: selectors are pure and terminals cannot report a
// pipeline error. TryQuery adds error propagation and context cancellation,
// and is produced by fallible operators such as SelectErr and by concurrent
// sources such as FromChan. ParQuery runs element-wise operators across a
// bounded worker pool.
//
// # Laziness
//
// Most operators stream in constant memory. Some must materialise their entire
// source before yielding anything: see the package documentation on individual
// operators. On an unbounded source such as FromChan, a buffering operator
// never yields.
//
// This package requires Go 1.27 or later: it depends on method type parameters.
package goq
