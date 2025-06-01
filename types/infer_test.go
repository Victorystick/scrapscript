package types

import (
	"strings"
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
		// Records
		{`{ a = 1 }`, `{ a : int }`},
		{`{ ..base, a = ~01 } ; base = { a = ~00 }`, `{ a : byte }`},
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
		{`(a -> b -> { a = a, b = b }) 1`, `a -> { a : int, b : a }`},
		{`(a -> b -> { a = a, b = b }) 1 "yo" `, `{ a : int, b : text }`},
	}

	for _, ex := range examples {
		se := must(parser.ParseExpr(ex.source))
		typ, err := Infer(se)
		if err != nil {
			t.Error(err)
		} else {
			if typ != ex.typ {
				t.Errorf("Expected %s, got %s", ex.typ, typ)
			}
		}
	}
}

func TestInferFailure(t *testing.T) {
	examples := []struct{ source, message string }{
		// Lists
		{`[1, 1.0]`, `list elements must all be of type int`},
		// Records
		{`{ ..base, a = 1 } ; base = { a = ~00 }`, `type of a must be byte, not int`},
		{`{ ..1, a = 1 }`, `cannot spread from non-record type int`},
		// Enums
	}

	for _, ex := range examples {
		se := must(parser.ParseExpr(ex.source))
		_, err := Infer(se)
		if err != nil {
			str := err.Error()
			if !strings.Contains(str, ex.message) {
				t.Errorf("Expected '%s' to be in error:\n%s", ex.message, str)
			}
		} else {
			t.Errorf("Expected '%s' error for script:\n%s", ex.message, ex.source)
		}
	}
}

func TestInferInScope(t *testing.T) {
	reg := Registry{}
	var scope *Scope[TypeRef]

	scope = scope.Bind("len", reg.Func(reg.List(reg.Unbound()), IntRef))

	examples := []struct{ source, typ string }{
		{`len`, `list a -> int`},
		{`len []`, `int`},
		{`f -> a -> [ a ]`, `a -> b -> list b`},
		{`(f -> a -> [ a ]) "a"`, `a -> list a`},
		{`(f -> a -> [ a ]) "a" 3`, `list int`},
		{`f -> a -> ([ b, b ] ; b = (f a))`, `(a -> b) -> a -> list b`},
		// If used the same, arguments must be the same.
		{`a -> b -> [ a, b ]`, `a -> a -> list a`},
		{`(a -> b -> [ a, b ]) 1`, `int -> list int`},

		{`(f -> a -> ([ b, b ] ; b = (f a))) len`, `list a -> list int`},
		{`(f -> a -> ([ b, b ] ; b = (f a))) len []`, `list int`},
		// {`twice ; twice = f -> a -> f (f a)`, `(a -> a) -> a -> a`},
	}

	for _, ex := range examples {
		se := must(parser.ParseExpr(ex.source))
		ref, err := InferInScope(&reg, scope, se)
		if err != nil {
			t.Error(err)
		} else {
			typ := reg.String(ref)
			if typ != ex.typ {
				t.Errorf("Expected %s, got %s", ex.typ, typ)
			}
		}
	}
}
