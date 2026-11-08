package eval

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"
)

// checkEvery is how many calls of script functions pass between checks of
// an evaluation's context. Every loop of a scrap is a call, so checking at
// calls stops any of them.
const checkEvery = 1024

// A limit stops one evaluation once its context is done, or once its
// non-tail calls nest too deep. Each evaluation has its own, which the
// frames of its calls carry, so evaluations on different goroutines share
// nothing that changes: a call from one program into a function of another
// counts against the evaluation it is part of.
//
// The context is polled rather than waited on: a WebAssembly host has one
// thread, so a timer can't fire while a scrap runs, and the deadline is
// compared with the clock instead.
type limit struct {
	ctx      context.Context // nil if it can't be done
	done     <-chan struct{}
	deadline time.Time
	timed    bool
	left     int // calls until the next check
	depth    int // non-tail calls under way
}

// newLimit returns the limit of an evaluation that ctx stops, as Call
// describes, checking it every checkEvery calls, starting with the first.
// A ctx that can't be done, such as context.Background(), isn't checked.
func newLimit(ctx context.Context) *limit {
	l := &limit{}
	if ctx != nil {
		l.done = ctx.Done()
		l.deadline, l.timed = ctx.Deadline()
	}
	if l.done != nil || l.timed {
		l.ctx = ctx
	}
	return l
}

// tick counts a call, checking the context every checkEvery calls.
func (l *limit) tick() {
	if l.ctx == nil {
		return
	}
	l.left--
	if l.left <= 0 {
		l.check()
	}
}

// check fails the evaluation if the context is done.
func (l *limit) check() {
	l.left = checkEvery
	select {
	case <-l.done:
		panic(runtimeError{stopped(l.ctx.Err())})
	default:
	}
	if l.timed && !time.Now().Before(l.deadline) {
		panic(runtimeError{stopped(context.DeadlineExceeded)})
	}
}

// stopped returns the error of an evaluation stopped because of err, a
// context's error, which it wraps.
func stopped(err error) error {
	return stoppedError{err}
}

type stoppedError struct{ err error }

func (e stoppedError) Error() string {
	if errors.Is(e.err, context.DeadlineExceeded) {
		return "evaluation stopped: it ran past its deadline"
	}
	return "evaluation stopped: " + e.err.Error()
}

func (e stoppedError) Unwrap() error { return e.err }

// maxDepth is how deep the non-tail calls of an evaluation may nest, so
// that a recursion fails before it overflows the stack, which no deadline
// prevents and which Go can't recover from. Natively, a call takes up to
// about 1 KB of Go's stack, which can grow to 1 GB. In WebAssembly the
// engine's stack overflows first: a browser's Web Worker has room for about
// 2,400 calls, and failing takes about as much stack again as the calls it
// unwinds.
var maxDepth = 100_000

func init() {
	if runtime.GOARCH == "wasm" {
		maxDepth = 1_000
	}
}

type tooDeepError struct{ max int }

func (e tooDeepError) Error() string {
	return fmt.Sprintf("evaluation stopped: calls nested deeper than %d", e.max)
}
