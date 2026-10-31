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

// A limit stops the evaluations of an environment's programs once their
// context is done. Its programs share it, so that a call from one into a
// function of another counts the same.
//
// The context is polled rather than waited on: a WebAssembly host has one
// thread, so a timer can't fire while a scrap runs, and the deadline is
// compared with the clock instead.
type limit struct {
	ctx      context.Context // nil if evaluations are unlimited
	done     <-chan struct{}
	deadline time.Time
	timed    bool
	left     int // calls until the next check
	depth    int // non-tail calls under way
	maxDepth int // at most, beyond which they fail
}

// UseContext stops evaluations in e once ctx is done, by its deadline or
// by being cancelled from another goroutine, checking it every so many
// calls. That covers calls of values that e evaluated too, as by eval.Call
// and the bindings that scrap-gen-go generates. An evaluation that it stops
// fails with an error that wraps ctx.Err(), or context.DeadlineExceeded
// once the deadline has passed, even if ctx hasn't noticed yet. A nil ctx
// lifts the limit.
//
// In WebAssembly, give it a context that only reports its deadline, as
// cmd/scrap-wasm does: one from context.WithDeadline starts a timer, which
// the Go runtime waits on through WASI's poll_oneoff, which a host may not
// implement.
func (e *Environment) UseContext(ctx context.Context) {
	l := e.limit
	*l = limit{ctx: ctx, depth: l.depth, maxDepth: l.maxDepth}
	if ctx != nil {
		l.done = ctx.Done()
		l.deadline, l.timed = ctx.Deadline()
	}
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

// maxDepth returns how deep non-tail calls may nest, so that a recursion
// fails before it overflows the stack. Natively, a call takes up to about
// 1 KB of Go's stack, which can grow to 1 GB. In WebAssembly the engine's
// stack overflows first: a browser's Web Worker has room for about 2,400
// calls, and failing takes about as much stack again as the calls it
// unwinds.
func maxDepth() int {
	if runtime.GOARCH == "wasm" {
		return 1_000
	}
	return 100_000
}

type tooDeepError struct{ max int }

func (e tooDeepError) Error() string {
	return fmt.Sprintf("evaluation stopped: calls nested deeper than %d", e.max)
}
