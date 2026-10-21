package eval_test

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Victorystick/scrapscript/eval"
)

var terminal = fstest.MapFS{
	"program.scrap": {Data: []byte(`program
; program     : type -> type = s => { init : turn s, step : event -> s -> turn s }
; turn        : type -> type = s => { state : s, out : text, next : next-action }
; event       : type = [#line text, #eof]
; next-action : type = [#read-line, #exit int]`)},
}

func read(t *testing.T, e *eval.Environment, source string) *eval.Scrap {
	t.Helper()
	s, err := e.Read([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func environment() *eval.Environment {
	e := eval.NewEnvironment()
	e.UseDrafts(terminal)
	return e
}

// A program that counts the lines it echoes, and exits with the count.
const counter = `{ init = { state = 0, out = "", next = #read-line }
, step = | #line text -> n -> { state = n + 1, out = text ++ "\n", next = #read-line }
         | #eof       -> n -> { state = n,     out = "",          next = #exit n }
}`

func TestCheck(t *testing.T) {
	e := environment()
	iface := read(t, e, `$"./program.scrap"`)
	if err := e.Check(read(t, e, counter), iface); err != nil {
		t.Errorf("counter: %v", err)
	}

	failures := []struct{ source, error string }{
		{`{ init = 1, step = 2 }`, `does not implement the interface`},
		{strings.Replace(counter, `#exit n`, `#exit "n"`, 1), `does not implement the interface`},
		{`t ; t : type = int`, `cannot check a type`},
	}
	for _, f := range failures {
		err := e.Check(read(t, e, f.source), iface)
		if err == nil || !strings.Contains(err.Error(), f.error) {
			t.Errorf("%s: expected an error containing %q, got %v", f.source, f.error, err)
		}
	}
	if err := e.Check(read(t, e, counter), read(t, e, `1`)); err == nil {
		t.Errorf("expected checking against a value to fail")
	}
}

// TestRunProgram runs the counter as a terminal host would.
func TestRunProgram(t *testing.T) {
	e := environment()
	app := read(t, e, counter)
	if err := e.Check(app, read(t, e, `$"./program.scrap"`)); err != nil {
		t.Fatal(err)
	}
	v, err := e.Eval(app)
	if err != nil {
		t.Fatal(err)
	}
	prog := v.Record()
	turn := prog.Get(e.Label("init")).Record()
	var out strings.Builder
	input := []string{"a", "b"}
	for {
		out.WriteString(turn.Get(e.Label("out")).Text())
		next := turn.Get(e.Label("next")).Variant()
		if next.Tag() == e.Label("exit") {
			if status := next.Payload().Int(); status != 2 {
				t.Errorf("expected status 2, got %d", status)
			}
			break
		}
		event := eval.NewVariant(e.Label("eof"), eval.Value{})
		if len(input) > 0 {
			event, input = eval.NewVariant(e.Label("line"), eval.Text(input[0])), input[1:]
		}
		res, err := eval.Call(prog.Get(e.Label("step")), event, turn.Get(e.Label("state")))
		if err != nil {
			t.Fatal(err)
		}
		turn = res.Record()
	}
	if out.String() != "a\nb\n" {
		t.Errorf("expected %q, got %q", "a\nb\n", out.String())
	}
}

func TestHostFunc(t *testing.T) {
	e := environment()
	add := eval.NewFunc("add", 2, func(args []eval.Value) (eval.Value, error) {
		return eval.Int(args[0].Int() + args[1].Int()), nil
	})
	// Partial applications of one function keep their own arguments.
	fv, err := e.Eval(read(t, e, `f -> ([f 1 2, g 3, g 4] ; g = f 10)`))
	if err != nil {
		t.Fatal(err)
	}
	v, err := eval.Call(fv, add)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Print(v); got != `[ 3, 13, 14 ]` {
		t.Errorf("expected [ 3, 13, 14 ], got %s", got)
	}
	if got := e.Print(eval.NewVariant(e.Label("f"), must(eval.Call(add, eval.Int(1))))); got != `#f (add 1)` {
		t.Errorf("expected a partial host function to print as #f (add 1), got %s", got)
	}

	fail := eval.NewFunc("fail", 1, func([]eval.Value) (eval.Value, error) {
		return eval.Value{}, errors.New("no")
	})
	if _, err := eval.Call(fv, fail); err == nil || err.Error() != "fail: no" {
		t.Errorf("expected the host function's error, got %v", err)
	}
}

func TestShape(t *testing.T) {
	e := environment()
	r := e.Shape(e.Label("b"), e.Label("a")).New(eval.Int(1), eval.Text("x"))
	if got := e.Print(r); got != `{ a = "x", b = 1 }` {
		t.Errorf("expected { a = \"x\", b = 1 }, got %s", got)
	}
	if r.Record().Get(e.Label("b")).Int() != 1 || r.Record().Get(e.Label("c")).IsValid() {
		t.Errorf("expected b = 1 and no c")
	}
	// A scrap reads the record's fields as those of its own records, which
	// have the same shape, whatever its fields' order.
	fn, err := e.Eval(read(t, e, `| { b = 1, a = a } -> a ++ "y" | _ -> "no"`))
	if err != nil {
		t.Fatal(err)
	}
	v, err := eval.Call(fn, r)
	if err != nil || v.Text() != "xy" {
		t.Errorf("expected \"xy\", got %s, %v", e.Print(v), err)
	}
	// Labels given in the record's order need no reordering.
	if got := e.Print(e.Shape(e.Label("a"), e.Label("b")).New(eval.Text("x"), eval.Int(1))); got != `{ a = "x", b = 1 }` {
		t.Errorf("expected { a = \"x\", b = 1 }, got %s", got)
	}
	if got := e.Print(e.Shape().New()); got != `{}` {
		t.Errorf("expected {}, got %s", got)
	}
	defer func() {
		if recover() == nil {
			t.Errorf("expected a label given twice to panic")
		}
	}()
	e.Shape(e.Label("a"), e.Label("a"))
}

func TestCallFailure(t *testing.T) {
	e := environment()
	fn, err := e.Eval(read(t, e, `| #a -> 2`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eval.Call(fn, eval.NewVariant(e.Label("b"), eval.Value{})); err == nil || !strings.Contains(err.Error(), "no arm matches #b") {
		t.Errorf("expected no arm to match, got %v", err)
	}
	fn, err = e.Eval(read(t, e, `| #a -> #c -> 2`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eval.Call(fn, eval.NewVariant(e.Label("a"), eval.Value{}), eval.NewVariant(e.Label("d"), eval.Value{})); err == nil || !strings.Contains(err.Error(), "no arm matches #a, #d") {
		t.Errorf("expected no arm to match, got %v", err)
	}
}

func must(v eval.Value, err error) eval.Value {
	if err != nil {
		panic(err)
	}
	return v
}

func TestReadAsOtherKind(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || r != "eval: reading text as int" {
			t.Errorf("expected reading text as int to panic, got %v", r)
		}
	}()
	eval.Text("x").Int()
}
