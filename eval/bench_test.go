package eval

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/types"
)

// evalSources are the programs that BenchmarkEval runs, each stressing one
// part of the evaluator.
var evalSources = []struct{ name, source string }{
	// Calls and integer arithmetic.
	{"fib", `list/map fib (seq 15)
; seq = | 0 -> [0] | n -> seq (n - 1) +< n
; fib =
    | 0 -> 0
    | 1 -> 1
    | n -> fib (n - 1) + fib (n - 2)`},
	// Tail calls with an accumulator, in a curried function.
	{"loop", `fib 0 1 10000
; fib = a -> b -> | 0 -> a | n -> fib b (a + b) (n - 1)`},
	// List patterns.
	{"sum", `sum (list/repeat 1000 1)
; sum = | [] -> 0 | x >+ xs -> x + sum xs`},
	// A builtin calling back into a closure.
	{"fold", `list/fold 0 (a -> b -> a + b) (list/repeat 10000 1)`},
	// Record spreads and field access.
	{"records", `loop 1000 { a = 0, b = 0 }
; loop = n -> r -> (| 0 -> r | _ -> loop (n - 1) { ..r, a = r.a + 1 }) n`},
	// Wide record literals, destructured by closed patterns.
	{"fields", `loop 1000 0
; loop = | 0 -> acc -> acc | n -> acc -> loop (n - 1) (acc + sum (make n))
; make = n -> { a = n, b = n, c = n, d = n, e = n, f = n, g = n, h = n }
; sum = | { a = a, b = b, c = c, d = d, e = e, f = f, g = g, h = h } -> a + b + c + d + e + f + g + h`},
	// One field access over records of three shapes, and a rest pattern.
	{"rows", `loop 1000 0
; loop = | 0 -> acc -> acc | n -> acc -> loop (n - 1) (acc + get-x { x = n } + get-x { w = 1, x = n } + get-x { x = n, y = 2, z = 3 } + drop { x = n, y = 1, z = 2 })
; get-x = r -> r.x
; drop = | { ..rest, x = x } -> x + rest.y`},
	// A rest that is built but never used; compare destructure-closed.
	{"destructure", `loop 1000 0
; loop = | 0 -> sum -> sum | n -> sum -> loop (n - 1) (sum + take (record n))
; record = i -> { a = i, b = i + 2, c = "ignored" }
; take = | { ..r, a = a, b = b } -> a + b`},
	// The same as destructure, with a closed pattern instead of a rest.
	{"destructure-closed", `loop 1000 0
; loop = | 0 -> sum -> sum | n -> sum -> loop (n - 1) (sum + take (record n))
; record = i -> { a = i, b = i + 2, c = "ignored" }
; take = | { a = a, b = b, c = _ } -> a + b`},
	// Tags of a recursive type, built and matched.
	{"tree", `size (build 10)
; build : int -> tree = | 0 -> #leaf | n -> #node { left = build (n - 1), right = build (n - 1) }
; size : tree -> int = | #leaf -> 0 | #node t -> 1 + size t.left + size t.right
; tree : type = [#leaf, #node { left : tree, right : tree }]`},
	// Text concatenation.
	{"text", `text/length (list/fold "" cat (list/repeat 1000 "ab"))
; cat : text -> text -> text = a -> b -> a ++ b`},
	// A program on the terminal platform, run by its reference semantics.
	{"echo", `$"./simulate.scrap" $"./echo.scrap" (list/repeat 1000 "line")`},
}

// platformFiles are the drafts that the echo benchmark refers to.
var platformFiles = fstest.MapFS{
	"program.scrap": {Data: []byte(`program
; program     : type -> type = s => { init : turn s, step : event -> s -> turn s }
; turn        : type -> type = s => { state : s, out : text, next : next-action }
; event       : type = [#line text, #eof]
; next-action : type = [#read-line, #exit int]`)},
	"simulate.scrap": {Data: []byte(`simulate
; simulate : s => $"./program.scrap" s -> list text -> result
  = prog -> input -> run prog.step input "" prog.init
; result : type = { out : text, status : int }
; run = step -> input -> acc -> t -> dispatch step input (text/join "" [acc, t.out]) t.state t.next
; dispatch = step -> input -> acc -> state ->
  | #exit status -> { out = acc, status = status }
  | #read-line ->
      (| []           -> run step [] acc (step #eof state)
       | line >+ rest -> run step rest acc (step (#line line) state)) input`)},
	"echo.scrap": {Data: []byte(`echo
; echo : $"./program.scrap" () =
  { init = { state = (), out = "", next = #read-line }
  , step = | #line text -> state -> { state = state, out = text ++ "\n", next = #read-line }
           | #eof       -> state -> { state = state, out = "",           next = #exit 0 }
  }`)},
}

// inferSource is a where-chain of 1000 bindings that type-check, drawn from
// the types tests.
var inferSource = func() string {
	bindings := []string{
		`(f -> a -> [ a ]) "a" 3`,
		`| n >+ ns -> ns | [] -> [] <| [1, 2, 3]`,
		`| ns ++ [2, last] -> ns +< last | _ -> [] <| [1, 2, 3]`,
		`(a -> b -> { a = a, b = b }) 1 "yo"`,
		`| [] -> { empty = #true } | _ -> { empty = #false }`,
	}
	var sb strings.Builder
	sb.WriteString("x0_0\n")
	for i := range 200 {
		for j, b := range bindings {
			fmt.Fprintf(&sb, "; x%d_%d = %s\n", i, j, b)
		}
	}
	return sb.String()
}()

func readBench(b *testing.B, source string) (*Environment, *Scrap) {
	b.Helper()
	e := NewEnvironment()
	e.UseDrafts(platformFiles)
	scrap, err := e.Read([]byte(source))
	if err != nil {
		b.Fatal(err)
	}
	return e, scrap
}

// BenchmarkEval measures evaluating a compiled program. Only the root is
// evaluated each time; the values of the drafts it refers to are cached.
func BenchmarkEval(b *testing.B) {
	for _, ex := range evalSources {
		b.Run(ex.name, func(b *testing.B) {
			e, scrap := readBench(b, ex.source)
			if _, err := e.infer(scrap); err != nil {
				b.Fatal(err)
			}
			c, err := compile(&scrap.expr, e.vars, e.shapes)
			if err != nil {
				b.Fatal(err)
			}
			p := &program{core: c, evalImport: func(n ast.Node) (Value, error) {
				dep := scrap.deps[n]
				return e.eval(dep, dep.result)
			}}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := p.eval(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkRead measures the front end on a program and the drafts it
// refers to: scanning, parsing, loading and type checking.
func BenchmarkRead(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		e, scrap := readBench(b, evalSources[len(evalSources)-1].source)
		if _, err := e.infer(scrap); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInfer measures walking the tree while inferring types. Each
// iteration uses a fresh environment, since inference grows the registry.
func BenchmarkInfer(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		b.StopTimer()
		e, scrap := readBench(b, inferSource)
		b.StartTimer()
		if _, err := types.Infer(&e.reg, e.typeScope, scrap.expr, nil); err != nil {
			b.Fatal(err)
		}
	}
}
