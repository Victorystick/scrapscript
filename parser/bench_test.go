package parser

import (
	"bytes"
	"testing"

	"github.com/Victorystick/scrapscript/token"
)

// benchBindings are bindings that the parser accepts, drawn from the tests.
var benchBindings = []string{
	`f = | "a" -> 1 | "b" -> 2 | "c" -> 3 | x -> 0`,
	`hand : type = [#l int, #r int]`,
	`h = (#l 5 |> | #l n -> n * 2 | #r n -> n * 3)`,
	`r = { ..{ a = 2, c = 1 }, a = 1, b = "x" }`,
	`xs = ["prefix"] ++ ["in" ++ "fix"] +< "postfix"`,
	`g = | ns ++ [2, last] -> ns +< last`,
	`inc : int -> int = a -> a + 1`,
	`fib = | 0 -> 0 | 1 -> 1 | n -> fib (n - 1) + fib (n - 2)`,
	`s = bytes/to-utf8-text <| ~~aGVsbG8gd29ybGQ= +< ~21`,
	`v = list/fold 0 (a -> b -> a + text/length b) ["hey", "beautiful"]`,
	`m = #just 2 |> | #just 2 -> "two" | #just _ -> "other" | #no -> "x"`,
	`p = foo.a.b`,
}

// benchSource is a single where-chain of about 1 MB.
var benchSource = func() []byte {
	var buf bytes.Buffer
	buf.WriteString("x\n")
	for buf.Len() < 1<<20 {
		for _, b := range benchBindings {
			buf.WriteString("; ")
			buf.WriteString(b)
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes()
}()

func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(benchSource)))
	b.ReportAllocs()
	for b.Loop() {
		src := token.NewSource(benchSource)
		if _, err := Parse(&src); err != nil {
			b.Fatal(err)
		}
	}
}
