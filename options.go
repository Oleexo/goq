package goq

import "runtime"

// parOptions configures the parallel engine.
type parOptions struct {
	workers int
	window  int
	// ordered is set by AsOrdered (Task 18), not by any Option here. parMap
	// does not yet branch on it — Task 18 adds that branch to the consumption
	// loop — so it is unread until then.
	ordered bool //nolint:unused // wired up by AsOrdered, added in Task 18
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
