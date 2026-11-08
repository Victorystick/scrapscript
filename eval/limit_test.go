package eval_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Victorystick/scrapscript/eval"
)

// stops evaluates source in e until ctx is done, and checks that it fails
// with an error that wraps want, soon after.
func stops(t *testing.T, ctx context.Context, e *eval.Environment, source string, want error) {
	t.Helper()
	start := time.Now()
	_, err := e.Eval(ctx, read(t, e, source))
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
		// A loop of a match function, which takes several arguments.
		`f 0 1 ; f = | a -> b -> f b a`,
		// A loop in a function that a builtin calls.
		`list/map (_ -> f 0) [1] ; f = n -> f n`,
	} {
		e := eval.NewEnvironment()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		stops(t, ctx, e, source, context.DeadlineExceeded)
		cancel()
	}
}

func TestDeadlinePassed(t *testing.T) {
	e := eval.NewEnvironment()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	stops(t, ctx, e, `f 0 ; f = n -> f (n + 1)`, context.DeadlineExceeded)

	// A scrap that makes no calls still runs to the end.
	if v, err := e.Eval(ctx, read(t, e, `1 + 2`)); err != nil || e.Print(v) != "3" {
		t.Errorf("expected 3, got %v, %v", v, err)
	}
}

func TestCancel(t *testing.T) {
	e := eval.NewEnvironment()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	stops(t, ctx, e, `f 0 ; f = n -> f (n + 1)`, context.Canceled)
}

// TestDeadlineCall checks that a host's calls into a value are stopped
// too, and that the environment goes on after one is.
func TestDeadlineCall(t *testing.T) {
	e := eval.NewEnvironment()
	bg := context.Background()
	loop, err := e.Eval(bg, read(t, e, `| 0 -> 0 | n -> f n ; f = n -> f n`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(bg, 50*time.Millisecond)
	defer cancel()
	if _, err := eval.Call(ctx, loop, eval.Int(1)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the call to stop, got %v", err)
	}

	if v, err := eval.Call(bg, loop, eval.Int(0)); err != nil || v.Int() != 0 {
		t.Errorf("expected 0, got %v, %v", v, err)
	}
	if v, err := e.Eval(bg, read(t, e, `list/fold 0 (a -> b -> a + b) [1, 2, 3]`)); err != nil || e.Print(v) != "6" {
		t.Errorf("expected 6, got %v, %v", v, err)
	}
}

// tooDeep evaluates source in e and checks that it fails because its calls
// nest too deep.
func tooDeep(t *testing.T, e *eval.Environment, source string) {
	t.Helper()
	_, err := e.Eval(context.Background(), read(t, e, source))
	if err == nil || !strings.Contains(err.Error(), "calls nested deeper than") {
		t.Fatalf("%s: expected calls nested too deep, got %v", source, err)
	}
}

// deep is a recursion 50,000 calls deep, which is allowed natively.
const deep = `f 50000 ; f = | 0 -> 0 | n -> 1 + f (n - 1)`

func TestDepth(t *testing.T) {
	for _, source := range []string{
		`f 0 ; f = n -> 1 + f n`,
		// A function that takes several arguments.
		`f 0 1 ; f = a -> b -> 1 + f b a`,
		// A recursion through a function that a builtin calls.
		`f 0 ; f = n -> (| [x] -> x | _ -> 0) (list/map f [n])`,
	} {
		e := eval.NewEnvironment()
		tooDeep(t, e, source)

		// The calls that the error unwound don't count any longer.
		if v, err := e.Eval(context.Background(), read(t, e, deep)); err != nil || e.Print(v) != "50000" {
			t.Errorf("after %s: expected 50000, got %v, %v", source, v, err)
		}
	}
}

// TestDepthCall checks that a host's calls into a value are limited too,
// and that an error in one doesn't count against the next.
func TestDepthCall(t *testing.T) {
	e := eval.NewEnvironment()
	f, err := e.Eval(context.Background(), read(t, e, `| 0 -> 0 | n -> 1 + f n ; f = n -> 1 + f n`))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := eval.Call(context.Background(), f, eval.Int(1)); err == nil || !strings.Contains(err.Error(), "calls nested deeper than") {
			t.Fatalf("expected calls nested too deep, got %v", err)
		}
	}
	if v, err := e.Eval(context.Background(), read(t, e, deep)); err != nil || e.Print(v) != "50000" {
		t.Errorf("expected 50000, got %v, %v", v, err)
	}
}

// TestConcurrentDeadlines checks that each call has its own deadline: one
// that runs past its deadline stops, while calls without one, at the same
// time, run to the end. Run it with -race.
func TestConcurrentDeadlines(t *testing.T) {
	e := eval.NewEnvironment()
	bg := context.Background()
	f, err := e.Eval(bg, read(t, e, `| 0 -> 0 | n -> f n ; f = n -> f n`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(bg, 50*time.Millisecond)
	defer cancel()
	stopped := make(chan error)
	go func() {
		_, err := eval.Call(ctx, f, eval.Int(1))
		stopped <- err
	}()
	for range 100 {
		if v, err := eval.Call(bg, f, eval.Int(0)); err != nil || v.Int() != 0 {
			t.Fatalf("expected 0, got %v, %v", v, err)
		}
	}
	if err := <-stopped; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the call to stop, got %v", err)
	}
}

// TestConcurrentCalls calls functions of one environment from many
// goroutines at once, each compiled on its first call, which may be on any
// of them. Each call nests its own calls deep, which wouldn't fit under
// maxDepth together. Run it with -race.
func TestConcurrentCalls(t *testing.T) {
	e := eval.NewEnvironment()
	v, err := e.Eval(context.Background(), read(t, e, `{ deep, sum, sorted, pick }
; deep = | 0 -> 0 | n -> 1 + deep (n - 1)
; sum = list/fold 0 (a -> b -> a + b) >> (n -> { n, twice = n * 2 })
; sorted = list/sort int/cmp >> list/map (n -> n + 1)
; pick = | { ..rest, a = x } -> x + rest.b`))
	if err != nil {
		t.Fatal(err)
	}
	r := e.Record(v)
	get := func(name string) eval.Value { return r.Get(e.Label(name)) }
	deep, sum, sorted, pick := get("deep"), get("sum"), get("sorted"), get("pick")
	abc := e.Shape(e.Label("a"), e.Label("b"), e.Label("c"))
	twice := e.Label("twice")

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			check := func(what string, want int64, read func(eval.Value) int64, v eval.Value, err error) {
				if err != nil {
					t.Errorf("%s: %v", what, err)
				} else if got := read(v); got != want {
					t.Errorf("%s: expected %d, got %d", what, want, got)
				}
			}
			n := eval.Int(int64(i))
			v, err := eval.Call(context.Background(), deep, eval.Int(50_000))
			check("deep", 50_000, eval.Value.Int, v, err)
			v, err = eval.Call(context.Background(), sum, eval.NewList(eval.Int(1), eval.Int(2), n))
			check("sum", int64(2*(3+i)), func(v eval.Value) int64 { return e.Record(v).Get(twice).Int() }, v, err)
			v, err = eval.Call(context.Background(), sorted, eval.NewList(eval.Int(3), n, eval.Int(1)))
			check("sorted", int64(min(i, 1)+1), func(v eval.Value) int64 { return v.List().Elements()[0].Int() }, v, err)
			v, err = eval.Call(context.Background(), pick, abc.New(n, eval.Int(10), eval.Hole()))
			check("pick", int64(i+10), eval.Value.Int, v, err)
		}()
	}
	wg.Wait()
}
