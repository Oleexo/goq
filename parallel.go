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

// resolve tolerates the zero ParQuery, matching Query and TryQuery: a zero
// value yields no elements rather than panicking on a nil build func.
func (p ParQuery[T]) resolve() TryQuery[T] {
	if p.build == nil {
		return TryQuery[T]{}
	}
	return p.build(p.opts)
}

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

// AsOrdered makes results arrive in source order rather than completion order.
//
// The engine tags each element with its source position and reassembles the
// sequence in a sink buffer holding at most Window-1 out-of-order results.
// Ordering costs latency and memory: one slow element delays every later one,
// and up to Window-1 results are retained meanwhile. Unordered is the default
// because it is strictly faster.
//
// Window also caps how many items may be in flight at once — the engine will
// not compute an item more than Window-1 positions ahead of the next one still
// awaited — so effective parallelism is at most min(Workers, Window). The
// default Window (four times Workers) never throttles; an explicitly small
// Window does, down to full serialisation at Window(1).
//
// Because the option is applied when the pipeline is built, AsOrdered may be
// written after the operators whose output it orders.
func (p ParQuery[T]) AsOrdered() ParQuery[T] {
	opts := p.opts
	opts.ordered = true
	return ParQuery[T]{build: p.build, opts: opts}
}

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
					// Comma-ok, never a plain assertion: for an interface T with
					// a nil element, k.val is a nil any and k.val.(T) would
					// panic. The comma-ok form yields T's zero value instead,
					// which for an interface T is correctly nil.
					val, _ := k.val.(T)
					if k.ok && !yield(val, nil) {
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
//   - a panic in f, or in any caller-supplied stage upstream of src (which
//     runs on the producer goroutine, not the caller's), is recovered,
//     workers and the producer are joined, and the panic is re-raised on the
//     consumer's goroutine as a PanicValue. A panic already wrapped in a
//     PanicValue (from a nested AsParallel upstream) is unwrapped rather than
//     wrapped again;
//   - every exit path from the *consumer's* side — completion, consumer
//     break, error, cancellation, and a panic inside the consumer's own fn
//     (e.g. ForEach) — cancels the workers and producer before returning.
//     Only the first four are joined (via stop()); a panic propagating out of
//     the yield callback itself is not caught here, so that path cancels but
//     does not join — see the note below;
//   - cancellation is reported as an error, never as a short result;
//   - the terminal blocks until every in-flight call to f has returned, since
//     stop() joins both goroutine groups before any exit path returns. A
//     callback that ignores its ctx argument will keep the terminal blocked
//     past cancellation until that call finishes.
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

				// opts.workers is normally >= 1: every live path reaches
				// parMap through newParOptions, which enforces the floor, and
				// resolve now guards build == nil before it ever gets here.
				// So a zero-value parOptions should be unreachable — but if
				// that ever stops being true, spawning zero workers would
				// leave the producer's send permanently unmatched and
				// deadlock stop()'s producer.Wait(). Keep the clamp as
				// defence in depth; it costs nothing on the live path.
				workers := opts.workers
				if workers < 1 {
					workers = 1
				}

				// opts.window is normally >= 1 for the same reason (see
				// workers above) and should likewise be unreachable at zero.
				// This clamp still matters if that ever stops holding: a
				// zero-capacity admit channel (below) would make the
				// producer's first admit send block forever (0 < 0+0 is never
				// true), so in would never close, workers would never exit,
				// out would never close, and the consumer would hang on
				// `range out` indefinitely. Keep it — the cost of checking is
				// far lower than the cost of a hang.
				window := opts.window
				if window < 1 {
					window = 1
				}

				in := make(chan parResult[T]) // unbuffered: backpressure
				out := make(chan parResult[R], workers)
				panics := make(chan panicInfo, workers+1) // +1: producer can also panic

				// admit is a token pool bounding how many items the producer
				// may have dispatched but the ordered sink has not yet
				// emitted: outstanding tokens = dispatched - emitted < window,
				// which is exactly the bound Window promises. Without it, a
				// single pathologically slow element would not stop the
				// producer from feeding workers further and further ahead —
				// out is drained continuously by the consumer (see the
				// ordered branch below), so its fixed capacity never
				// backpressures the producer on its own, and the reorder
				// sink's pending map would grow without limit.
				//
				// The producer acquires a token (send, blocking, cancellable
				// via ctx) before dispatching item i into in; the ordered
				// sink releases one (receive, immediately after advancing
				// next past the item it just emitted). The sink's receive can
				// never block: it is releasing the token for an item that was
				// necessarily admitted — and therefore already holds a token
				// — before it could ever have been computed and delivered
				// here, so no ctx case is needed on that side. Gating happens
				// on dispatch, before a worker ever sees the item, which is
				// why the pool loop below needs no changes. admit is never
				// touched when !opts.ordered, so unordered mode is
				// unaffected.
				admit := make(chan struct{}, window)

				var producer sync.WaitGroup
				producer.Add(1)
				go func() {
					defer producer.Done()
					defer close(in)
					// Every caller-supplied stage upstream of AsParallel
					// (sequential Select/Where/SelectErr, FromSeqTry, or a
					// nested parMap's own re-panic) runs on THIS goroutine via
					// planOf(src)(ctx). Without this recover, an upstream
					// panic kills the process instead of reaching the caller.
					// Declared last so it runs first (defers are LIFO),
					// letting it still observe and report the panic before
					// producer.Done() and close(in) unwind normally.
					defer func() {
						if r := recover(); r != nil {
							info := panicInfo{value: r, stack: debug.Stack()}
							// Unwrap a nested AsParallel's PanicValue so
							// nesting does not double-wrap it.
							if pv, ok := r.(PanicValue); ok {
								info = panicInfo{value: pv.Value, stack: pv.Stack}
							}
							select {
							case panics <- info:
							default:
							}
							cancel()
						}
					}()
					i := 0
					for v, err := range planOf(src)(ctx) {
						if opts.ordered {
							select {
							case admit <- struct{}{}:
							case <-ctx.Done():
								return // cancelled while waiting for admission
							}
						}
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

				var pool sync.WaitGroup
				for range workers {
					pool.Add(1)
					go func() {
						defer pool.Done()
						defer func() {
							if r := recover(); r != nil {
								info := panicInfo{value: r, stack: debug.Stack()}
								// Unwrap a nested AsParallel's PanicValue so
								// nesting does not double-wrap it, mirroring
								// the producer's recover above.
								if pv, ok := r.(PanicValue); ok {
									info = panicInfo{value: pv.Value, stack: pv.Stack}
								}
								select {
								case panics <- info:
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

				var closer sync.WaitGroup
				closer.Add(1)
				go func() {
					defer closer.Done()
					pool.Wait()
					close(out) // never blocks: closing a channel cannot block
				}()

				// stop cancels and joins everything, then re-raises a worker
				// or producer panic if there was one. It must run before any
				// return from the consumer's own exit paths (completion,
				// break, error, cancellation). It does NOT run if the
				// consumer's own callback (yield, i.e. ForEach's fn) panics:
				// that unwinds straight through this function. The deferred
				// cancel() above still fires in that case, so the workers and
				// producer are told to stop, but nothing joins them — a
				// measured, not-a-leak gap, since goleak only checks after
				// they have had time to react to cancellation, not that this
				// specific call joined them itself.
				stop := func() {
					cancel()
					producer.Wait()
					pool.Wait()
					closer.Wait()
					select {
					case p := <-panics:
						panic(PanicValue{Value: p.value, Stack: p.stack})
					default:
					}
				}

				if !opts.ordered {
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
				} else {
					// Reorder sink: hold results back until the next expected
					// index arrives. The admission gate above is what actually
					// keeps pending from growing past window-1 entries: the
					// producer never dispatches an item more than window-1
					// ahead of next, so at most window-1 results can ever
					// arrive here before the one pending needs.
					pending := make(map[int]parResult[R], window)
					next := 0
					for res := range out {
						pending[res.idx] = res
						for {
							item, ok := pending[next]
							if !ok {
								break
							}
							delete(pending, next)
							next++
							<-admit // release a token: let the producer dispatch one more
							if item.err != nil {
								var zero R
								yield(zero, item.err)
								stop()
								return
							}
							if !yield(item.val, nil) {
								stop()
								return
							}
						}
						if len(pending) > window {
							// Defensive: unreachable given the admission gate,
							// which never lets more than window-1 results
							// accumulate here. Failing loudly beats growing
							// without limit if that reasoning is ever wrong.
							var zero R
							yield(zero, fmt.Errorf(
								"goq: reorder buffer exceeded window %d; this is a bug",
								window))
							stop()
							return
						}
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
				//
				// This has its own, narrower race, mirroring the bug it fixes:
				// if parent's deadline expires after the drain above has
				// already delivered every element but before this check runs,
				// a fully-delivered pipeline reports (nil, DeadlineExceeded)
				// instead of its true, complete result. This is judged
				// acceptable — a cancelled ctx makes no promises about
				// already-delivered results — but is called out explicitly
				// rather than left for the next reader to rediscover.
				if cerr := parent.Err(); cerr != nil {
					var zero R
					yield(zero, cerr)
				}
			}
		},
	}
}
