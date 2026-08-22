package goq

import (
	"context"
	"fmt"
	"iter"
	"runtime/debug"
	"sync"
)

// PanicValue carries a panic that escaped a caller-supplied callback running on
// a parallel worker.
//
// The parallel engine recovers such panics, cancels and joins its workers, and
// then re-panics with a PanicValue on the goroutine that called the terminal
// operator. A panic is not converted into an error: a panic reports a bug,
// while an error reports an expected condition.
//
// Callers who recover must type-assert PanicValue and inspect Value to reach
// the original panic argument:
//
//	defer func() {
//		if r := recover(); r != nil {
//			if pv, ok := r.(goq.PanicValue); ok {
//				log.Printf("callback panicked: %v\n%s", pv.Value, pv.Stack)
//			}
//		}
//	}()
type PanicValue struct {
	// Value is the original argument passed to panic.
	Value any
	// Stack is the stack trace captured on the worker goroutine.
	Stack []byte
}

// String renders the original panic value followed by the worker stack.
func (p PanicValue) String() string {
	return fmt.Sprintf("goq: panic in parallel callback: %v\n%s", p.Value, p.Stack)
}

// ParQuery is a query pipeline whose element-wise operators run across a
// bounded pool of workers.
//
// Only element-wise operators are available: Select, SelectErr, SelectCtx and
// Where, plus the terminals. Ordering, grouping, and the set operators require
// materialising the stream, so they are reached through AsSequential, which
// makes the barrier visible in the chain rather than hiding it behind a
// fluent call.
//
// Results arrive in completion order unless AsOrdered is used.
// ParQuery holds a builder rather than a built pipeline, so that options
// affecting execution — AsOrdered above all — can be written after the operator
// they apply to and still reach the engine. resolve applies the accumulated
// options and materialises the stage chain.
type ParQuery[T any] struct {
	build func(parOptions) TryQuery[T]
	opts  parOptions
}

func (p ParQuery[T]) resolve() TryQuery[T] { return p.build(p.opts) }

// AsParallel runs subsequent element-wise operators across a worker pool.
func (q Query[T]) AsParallel(opts ...Option) ParQuery[T] {
	src := q.AsTry()
	return ParQuery[T]{
		opts:  newParOptions(opts),
		build: func(parOptions) TryQuery[T] { return src },
	}
}

// AsParallel runs subsequent element-wise operators across a worker pool,
// preserving the pipeline's existing error and cancellation behaviour.
func (q TryQuery[T]) AsParallel(opts ...Option) ParQuery[T] {
	return ParQuery[T]{
		opts:  newParOptions(opts),
		build: func(parOptions) TryQuery[T] { return q },
	}
}

// AsSequential returns the pipeline as a sequential TryQuery, ending parallel
// execution. It is the explicit barrier required before ordering, grouping, or
// set operations.
func (p ParQuery[T]) AsSequential() TryQuery[T] { return p.resolve() }

// Select applies f to each element across the worker pool.
func (p ParQuery[T]) Select[R any](f func(T) R) ParQuery[R] {
	return p.SelectCtx(func(_ context.Context, v T) (R, error) { return f(v), nil })
}

// SelectErr applies f to each element across the worker pool, stopping the
// pipeline at the first error. The error is returned verbatim.
func (p ParQuery[T]) SelectErr[R any](f func(T) (R, error)) ParQuery[R] {
	return p.SelectCtx(func(_ context.Context, v T) (R, error) { return f(v) })
}

// SelectCtx applies f to each element across the worker pool, passing the
// terminal operator's context so that in-flight work can be cancelled.
func (p ParQuery[T]) SelectCtx[R any](f func(context.Context, T) (R, error)) ParQuery[R] {
	build := p.build
	return ParQuery[R]{opts: p.opts, build: func(o parOptions) TryQuery[R] {
		return parMap(build(o), o, f)
	}}
}

// SelectMany applies f across the worker pool and flattens the slices it
// returns. Elements from one input's slice stay contiguous and in order; the
// slices themselves interleave unless AsOrdered is used.
func (p ParQuery[T]) SelectMany[R any](f func(T) []R) ParQuery[R] {
	build := p.build
	return ParQuery[R]{opts: p.opts, build: func(o parOptions) TryQuery[R] {
		mapped := parMap(build(o), o, func(_ context.Context, v T) ([]R, error) {
			return f(v), nil
		})
		return TryQuery[R]{guard: mapped.guard, plan: func(ctx context.Context) iter.Seq2[R, error] {
			return func(yield func(R, error) bool) {
				for batch, err := range planOf(mapped)(ctx) {
					if err != nil {
						var zero R
						yield(zero, err)
						return
					}
					for _, r := range batch {
						if !yield(r, nil) {
							return
						}
					}
				}
			}
		}}
	}}
}

// Where evaluates pred across the worker pool and yields the elements for which
// it returns true.
func (p ParQuery[T]) Where(pred func(T) bool) ParQuery[T] {
	build := p.build
	return ParQuery[T]{opts: p.opts, build: func(o parOptions) TryQuery[T] {
		mapped := parMap(build(o), o, func(_ context.Context, v T) (parKeep, error) {
			return parKeep{val: v, ok: pred(v)}, nil
		})
		return TryQuery[T]{guard: mapped.guard, plan: func(ctx context.Context) iter.Seq2[T, error] {
			return func(yield func(T, error) bool) {
				for k, err := range planOf(mapped)(ctx) {
					if err != nil {
						var zero T
						yield(zero, err)
						return
					}
					if k.ok && !yield(k.val.(T), nil) {
						return
					}
				}
			}
		}}
	}}
}

// ToSlice materialises the pipeline, returning a nil slice on error.
func (p ParQuery[T]) ToSlice(ctx context.Context) ([]T, error) {
	return p.resolve().ToSlice(ctx)
}

// Count reports the number of elements, or the first error.
func (p ParQuery[T]) Count(ctx context.Context) (int, error) { return p.resolve().Count(ctx) }

// ForEach calls fn for each element as it completes, stopping at the first
// error from either the pipeline or fn. fn runs on the caller's goroutine and
// is never invoked concurrently.
func (p ParQuery[T]) ForEach(ctx context.Context, fn func(T) error) error {
	return p.resolve().ForEach(ctx, fn)
}

// parResult carries one worker outcome. idx is the source position, used only
// by the ordered sink.
type parResult[R any] struct {
	idx int
	val R
	err error
}

// parKeep carries a Where predicate's verdict alongside the original element,
// so parMap's uniform worker/result plumbing can be reused for filtering.
//
// val is boxed as any, rather than parameterised by the element type, because
// instantiating parMap's second type parameter with a type built from Where's
// own (identical) type parameter triggers a generic-instantiation cycle in the
// compiler; boxing breaks that self-referential dependency.
type parKeep struct {
	val any
	ok  bool
}

type panicInfo struct {
	value any
	stack []byte
}

// parMap is the parallel engine: it runs f across opts.workers goroutines and
// returns a TryQuery over the results.
//
// Contract, all of which is tested:
//   - the input channel is unbuffered, so a slow consumer backpressures the
//     producer instead of accumulating;
//   - the first error cancels the derived context and stops the pipeline;
//   - a panic in f is recovered, workers are joined, and the panic is re-raised
//     on the consumer's goroutine as a PanicValue;
//   - every exit path — completion, consumer break, error, cancellation —
//     cancels and joins before returning, so no goroutine leaks;
//   - cancellation is reported as an error, never as a short result.
func parMap[T, R any](
	src TryQuery[T],
	opts parOptions,
	f func(context.Context, T) (R, error),
) TryQuery[R] {
	return TryQuery[R]{
		guard: src.guard,
		plan: func(parent context.Context) iter.Seq2[R, error] {
			return func(yield func(R, error) bool) {
				ctx, cancel := context.WithCancel(parent)
				defer cancel() // guarantees unwinding on every return below

				in := make(chan parResult[T]) // unbuffered: backpressure
				out := make(chan parResult[R], opts.workers)
				panics := make(chan panicInfo, opts.workers)

				var producer sync.WaitGroup
				producer.Add(1)
				go func() {
					defer producer.Done()
					defer close(in)
					i := 0
					for v, err := range planOf(src)(ctx) {
						select {
						case in <- parResult[T]{idx: i, val: v, err: err}:
						case <-ctx.Done():
							return
						}
						i++
						if err != nil {
							return // upstream failed; stop feeding workers
						}
					}
				}()

				var workers sync.WaitGroup
				for range opts.workers {
					workers.Add(1)
					go func() {
						defer workers.Done()
						defer func() {
							if r := recover(); r != nil {
								select {
								case panics <- panicInfo{value: r, stack: debug.Stack()}:
								default:
								}
								cancel()
							}
						}()
						for item := range in {
							res := parResult[R]{idx: item.idx}
							if item.err != nil {
								res.err = item.err
							} else {
								res.val, res.err = f(ctx, item.val)
							}
							select {
							case out <- res:
							case <-ctx.Done():
								return
							}
						}
					}()
				}

				go func() {
					workers.Wait()
					close(out)
				}()

				// stop cancels and joins everything, then re-raises a worker
				// panic if there was one. It must run before any return.
				stop := func() {
					cancel()
					producer.Wait()
					workers.Wait()
					select {
					case p := <-panics:
						panic(PanicValue{Value: p.value, Stack: p.stack})
					default:
					}
				}

				for res := range out {
					if res.err != nil {
						var zero R
						yield(zero, res.err)
						stop()
						return
					}
					if !yield(res.val, nil) {
						stop() // consumer abandoned the pipeline
						return
					}
				}

				stop()

				// Post-drain check. Workers that exited via ctx.Done() closed
				// `out` normally, which is indistinguishable from success up to
				// this point. Without this, cancellation returns a truncated
				// result and a nil error.
				//
				// This checks parent, not the derived ctx: stop() above always
				// calls cancel() as part of joining, even on ordinary
				// completion, so the derived ctx is unconditionally cancelled by
				// the time we get here and would misreport every exit as
				// cancellation. parent reflects only real caller-driven
				// cancellation (an explicit cancel or an expired deadline).
				if cerr := parent.Err(); cerr != nil {
					var zero R
					yield(zero, cerr)
				}
			}
		},
	}
}
