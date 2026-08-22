package goq

import "errors"

// Sentinel errors returned by goq. Compare them with errors.Is.
//
// These describe the shape of the data, not a failure of the pipeline: a query
// that returns ErrMultiple ran correctly and found more than one element.
// Errors produced by caller-supplied callbacks are returned verbatim and are
// never wrapped in these.
var (
	// ErrEmpty is returned by Single when the source yields no elements.
	ErrEmpty = errors.New("goq: sequence is empty")

	// ErrMultiple is returned by Single when the source yields more than one
	// element.
	ErrMultiple = errors.New("goq: sequence contains more than one element")

	// ErrConsumed is returned by a terminal operator on a single-shot source
	// that has already been enumerated. See FromChan and Memoize.
	ErrConsumed = errors.New("goq: source has already been consumed")

	// ErrDuplicateKey is returned by ToMap when two elements produce the same
	// key. Use ToMapLast to overwrite instead.
	ErrDuplicateKey = errors.New("goq: duplicate key")
)
