package types

import (
	"testing"

	"github.com/Victorystick/scrapscript/parser"
)

func must[T any](val T, err error) T {
	if err != nil {
		panic(err)
	}
	return val
}

func TestInfer(t *testing.T) {
	examples := []struct{ source, typ string }{
		// Primitives
		{`5`, `int`},
		{`a ; a = 5`, `int`},
		// Lists
		{`[]`, `list never`}, // empty list has no values
		{`[1, 2]`, `list int`},
		{`[1, 1.0]`, `never`}, // bad list
		// Records
		{`{ a = 1 }`, `{ a : int }`},
		{`{ ..base, a = ~01 } ; base = { a = ~00 }`, `{ a : byte }`},
		{`{ ..base, a = 1 } ; base = { a = ~00 }`, `never`}, // change type
		{`{ ..1, a = 1 }`, `never`},                         // bad spread
		// Enums
		{`bool ; bool : #true #false`, `#false #true`},
		{`e ; e : #l int #r`, `#l int #r`},
		{`e::r ; e : #l int #r`, `#l int #r`},
		{`e::l 4 ; e : #l int #r`, `#l int #r`},
		// Functions
		{`_ -> "hi"`, `a -> text`},
		{`_ -> _ -> "hi"`, `a -> b -> text`},
		{`(_ -> "hi") ()`, `text`},
		{`a -> b -> { a = a, b = b }`, `a -> b -> { a : a, b : b }`},
		{`(a -> b -> { a = a, b = b }) 1`, `b -> { a : int, b : b }`},
		{`(a -> b -> { a = a, b = b }) 1 "yo" `, `{ a : int, b : text }`},
	}

	for _, ex := range examples {
		se := must(parser.ParseExpr(ex.source))
		reg, types := Infer(se)
		typ := reg.String(types[se.Expr])
		if typ != ex.typ {
			t.Errorf("Expected %s, got %s", ex.typ, typ)
		}
	}
}
