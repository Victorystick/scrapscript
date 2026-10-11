package eval

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/types"
)

const fibSource = `list/map fib (seq 15)
; seq = | 0 -> [0] | n -> seq (n - 1) +< n
; fib =
    | 0 -> 0
    | 1 -> 1
    | n -> fib (n - 1) + fib (n - 2)`

// inferSource is a where-chain of 1000 bindings that type-check, drawn from
// the types tests.
var inferSource = func() string {
	bindings := []string{
		`(f -> a -> [ a ]) "a" 3`,
		`| n >+ ns -> ns <| [1, 2, 3]`,
		`| ns ++ [2, last] -> ns +< last <| [1, 2, 3]`,
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
	scrap, err := e.Read([]byte(source))
	if err != nil {
		b.Fatal(err)
	}
	return e, scrap
}

// BenchmarkEval measures walking the tree while evaluating.
func BenchmarkEval(b *testing.B) {
	e, scrap := readBench(b, fibSource)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Eval(scrap.expr, e.vars, nil); err != nil {
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
