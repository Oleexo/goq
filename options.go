package goq

import "runtime"

// parOptions configures the parallel engine.
type parOptions struct {
	workers int
	window  int
	// ordered is set by AsOrdered, not by any Option here. parMap branches on
	// it in its consumption loop to decide whether to reassemble source order
	// via the reorder sink.
	ordered bool
}

// Option configures a parallel pipeline. Pass options to AsParallel.
type Option func(*parOptions)

// Workers sets the number of concurrent workers. Values below one are ignored.
// The default is runtime.GOMAXPROCS(0).
func Workers(n int) Option {
	return func(o *parOptions) {
		if n >= 1 {
			o.workers = n
		}
	}
}

// Window sets how many out-of-order results the ordered sink may buffer before
// it stops accepting more. It bounds memory when one element is far slower than
// its neighbours. Values below one are ignored. The default is four times the
// worker count. It has no effect unless AsOrdered is used.
//
// In ordered mode, Window also caps how many items may be in flight at once —
// the producer will not dispatch an item more than Window-1 positions ahead of
// the next one the sink is waiting to emit — so effective parallelism is at
// most min(Workers, Window). The default (four times Workers) exceeds Workers,
// so it never throttles; only an explicitly small Window (e.g. Window(1))
// serialises the pipeline.
func Window(n int) Option {
	return func(o *parOptions) {
		if n >= 1 {
			o.window = n
		}
	}
}

func newParOptions(opts []Option) parOptions {
	o := parOptions{workers: runtime.GOMAXPROCS(0)}
	for _, fn := range opts {
		fn(&o)
	}
	if o.workers < 1 {
		o.workers = 1
	}
	if o.window < 1 {
		o.window = 4 * o.workers
	}
	return o
}
