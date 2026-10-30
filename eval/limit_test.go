package eval_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Victorystick/scrapscript/eval"
)

// stops evaluates source in e and checks that it fails with an error that
// wraps want, soon after the context's deadline.
func stops(t *testing.T, e *eval.Environment, source string, want error) {
	t.Helper()
	start := time.Now()
	_, err := e.Eval(read(t, e, source))
	if !errors.Is(err, want) {
		t.Fatalf("%s: expected an error wrapping %v, got %v", source, want, err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("%s: took %s to stop", source, took)
	}
}

func TestDeadline(t *testing.T) {
	for _, source := range []string{
		// A loop of tail calls, which never grows the stack.
		`f 0 ; f = n -> f (n + 1)`,
		// A recursion that would overflow the stack in time.
		`f 0 ; f = n -> 1 + f n`,
		// A loop of a match function, which takes several arguments.
		`f 0 1 ; f = | a -> b -> f b a`,
		// A loop in a function that a builtin calls.
		`list/map (_ -> f 0) [1] ; f = n -> f n`,
	} {
		e := eval.NewEnvironment()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		e.UseContext(ctx)
		stops(t, e, source, context.DeadlineExceeded)
		cancel()
	}
}

func TestDeadlinePassed(t *testing.T) {
	e := eval.NewEnvironment()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	e.UseContext(ctx)
	stops(t, e, `f 0 ; f = n -> f (n + 1)`, context.DeadlineExceeded)

	// A scrap that makes no calls still runs to the end.
	if v, err := e.Eval(read(t, e, `1 + 2`)); err != nil || e.Print(v) != "3" {
		t.Errorf("expected 3, got %v, %v", v, err)
	}
}

func TestCancel(t *testing.T) {
	e := eval.NewEnvironment()
	ctx, cancel := context.WithCancel(context.Background())
	e.UseContext(ctx)
	time.AfterFunc(20*time.Millisecond, cancel)
	stops(t, e, `f 0 ; f = n -> f (n + 1)`, context.Canceled)
}

// TestDeadlineCall checks that a host's calls into a value are stopped too,
// as the bindings that scrap-gen-go generates make them, and that the
// environment can go on once the limit is lifted.
func TestDeadlineCall(t *testing.T) {
	e := eval.NewEnvironment()
	loop, err := e.Eval(read(t, e, `| 0 -> 0 | n -> f n ; f = n -> f n`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	e.UseContext(ctx)
	if _, err := eval.Call(loop, eval.Int(1)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the call to stop, got %v", err)
	}

	e.UseContext(nil)
	if v, err := eval.Call(loop, eval.Int(0)); err != nil || v.Int() != 0 {
		t.Errorf("expected 0, got %v, %v", v, err)
	}
	if v, err := e.Eval(read(t, e, `list/fold 0 (a -> b -> a + b) [1, 2, 3]`)); err != nil || e.Print(v) != "6" {
		t.Errorf("expected 6, got %v, %v", v, err)
	}
}
