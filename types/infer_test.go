package types

import (
	"fmt"
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
		{`1 + 2`, `int`},
		// Lists
		{`[]`, `list $0`}, // empty list has an unbound type for its values
		{`[1, 2]`, `list int`},
		// Records
		{`{ a = 1 }`, `{ a : int }`},
		{`{ ..base, a = ~01 } ; base = { a = ~00 }`, `{ a : byte }`},
		// // Enums
		{`bool ; bool : #true #false`, `#false #true`},
		{`e ; e : #l int #r`, `#l int #r`},
		{`e::r ; e : #l int #r`, `#l int #r`},
		{`e::l 4 ; e : #l int #r`, `#l int #r`},
		// Functions
		{`a -> a`, `$0 -> $0`},
		{`_ -> "hi"`, `$0 -> text`},
		{`_ -> _ -> "hi"`, `$0 -> $1 -> text`},
		{`(_ -> "hi") ()`, `text`},

		// Prepend and append
		{`a -> a >+ []`, `$1 -> list $1`},
		{`a -> a +< int`, `list int -> list int`},
		{`a -> a >+ ~~1111`, `byte -> bytes`},
		{`a -> a +< ~ff`, `bytes -> bytes`},

		// Concat
		{`"hi " ++ "you!"`, `text`},
		{`[] ++ [1]`, `list int`},
		{`~~1111 ++ ~~`, `bytes`},
		{`a -> b -> a ++ b`, `list $2 -> list $2 -> list $2`},

		// Math
		{`a -> 1.0 + a`, `float -> float`},
		{`4 - 3`, `int`},
		{`a -> b -> a * b`, `int -> int -> int`}, // Default to int.

		{`a -> b -> { a = a, b = b }`, `$0 -> $1 -> { a : $0, b : $1 }`},
		{`(a -> b -> { a = a, b = b }) 1`, `$2 -> { a : int, b : $2 }`},
		{`(a -> b -> { a = a, b = b }) 1 "yo" `, `{ a : int, b : text }`},
		{`a ; a : int = 1`, `int`},
		{`a -> a + 1`, `int -> int`},
		{`b -> (a ; a : int = b)`, `int -> int`},

		{`f -> f (f 1)`, `(int -> int) -> int`},
		{`a -> f -> f (f a)`, `$2 -> ($2 -> $2) -> $2`},
		{`f -> a -> f (f a)`, `($2 -> $2) -> $2 -> $2`},

		{`f -> a -> [ a ]`, `$0 -> $1 -> list $1`},
		{`(f -> a -> [ a ]) "a"`, `$2 -> list $2`},
		{`(f -> a -> [ a ]) "a" 3`, `list int`},

		{`f -> a -> ([ b, b ] ; b = (f a))`, `($1 -> $2) -> $1 -> list $2`},
		// If used the same, arguments must be the same.
		{`a -> b -> [ a, b ]`, `$1 -> $1 -> list $1`},
		{`(a -> b -> [ a, b ]) 1`, `int -> list int`},

		{`typ::fun (x -> x * 2) ; typ : #fun (int -> int)`, `#fun (int -> int)`},
	}

	for _, ex := range examples {
		se := must(parser.ParseExpr(ex.source))
		var reg Registry
		typ, err := Infer(&reg, DefaultScope(&reg), se, nil)
		if err != nil {
			t.Error(err)
		} else {
			typStr := reg.String(typ)
			if typStr != ex.typ {
				t.Errorf("Expected %s, got %s", ex.typ, typStr)
			}
		}
	}
}

func TestInferFailure(t *testing.T) {
	examples := []struct{ source, message string }{
		// Unbound
		{`b ; a = b -> b`, `unbound variable: b`},
		// Lists
		{`[1, 1.0]`, `cannot unify 'int' with 'float'`},
		{`[4] ++ ["text"]`, `cannot unify 'int' with 'text'`},
		{`4 ++ 6`, `cannot unify 'int' with 'list $0'`},
		// Records
		{`{ ..base, a = 1 } ; base = { a = ~00 }`, `type of a must be byte, not int`},
		{`{ ..1, a = 1 }`, `cannot spread from non-record type int`},
		// Enums
		{`1::a`, `1 isn't an enum`},
		{`a::a ; a : #b`, `#a isn't a valid option for enum #b`},
		{`a::b 1 ; a : #b`, `#b doesn't take any value`},
		{`a::b 1 ; a : #b text`, `cannot unify 'int' with 'text'`},
		{`1 + ~dd`, `cannot unify 'byte' with 'int'`},
		{`a ; a : int = 1.0`, `cannot unify 'float' with 'int'`},
		{`f ; f : int -> text = a -> 1`, `cannot unify 'int' with 'text'`},
		// Math
		{`1 + 1.0`, `cannot unify 'int' with 'float'`},
		// No imports.
		{`$sha256~~`, `<internal error> missing infer import function`},
	}

	for _, ex := range examples {
		var reg Registry
		se := must(parser.ParseExpr(ex.source))
		_, err := Infer(&reg, DefaultScope(&reg), se, nil)
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
	examples := []struct{ source, typ string }{
		{`len`, `list $0 -> int`},
		{`len []`, `int`},
		{`(f -> a -> ([ b, b ] ; b = (f a))) len`, `list $4 -> list int`},
		{`(f -> a -> ([ b, b ] ; b = (f a))) len []`, `list int`},

		{`{ a = id 1, b = id "" }`, `{ a : int, b : text }`},
		{`{ a = id2 1, b = id2 "" } ; id2 = a -> a`, `{ a : int, b : text }`},
	}

	for _, ex := range examples {
		se := must(parser.ParseExpr(ex.source))

		// New registry every test.
		reg := Registry{}
		var scope *Scope[TypeRef]

		scope = scope.Bind("len", reg.Func(reg.List(reg.Unbound()), IntRef))

		a := reg.Unbound()
		scope = scope.Bind("id", reg.Func(a, a))

		ref, err := Infer(&reg, scope, se, nil)
		if err != nil {
			t.Error(err)
		} else {
			typ := reg.String(ref)
			if typ != ex.typ {
				t.Errorf("Invalid type for '%s'\n  expected: %s\n       got: %s", ex.source, ex.typ, typ)
			}
		}
	}
}

type MapFetcher map[string]string

func (mf MapFetcher) FetchSha256(key string) ([]byte, error) {
	source, ok := mf[key]
	if !ok {
		return nil, fmt.Errorf("can't import '%s'", key)
	}
	return []byte(source), nil
}

func TestInferImport(t *testing.T) {
	var reg Registry

	a := reg.Var()

	examples := []struct {
		in     string  // The input.
		imp    TypeRef // The imported type.
		result string  // The stringified result type, or
		err    string  // an error.
	}{
		// Since the `InferImport` function below doesn't check the hash length
		// '$sha256~~' is sufficient to encode an import.
		{in: `$sha256~~`, imp: IntRef, result: `int`},
		{in: `$sha256~~`, imp: FloatRef, result: `float`},
		{in: `1 + $sha256~~`, imp: FloatRef, err: `cannot unify 'int' with 'float'`},
		{in: `$sha256~~`, imp: a, result: `$0`},
		{in: `a ; a = $sha256~~`, imp: a, result: `$0`},
		{in: `$sha256~~ [ 1, 2 ]`, imp: reg.Func(a, a), result: `list int`},
		// TODO: Aliasing allocates new type vars, just importing does not. :/
		{in: `a ; a = $sha256~~`, imp: reg.Func(a, a), result: `$2 -> $2`},
		{in: `a ; a = $sha256~~`, imp: reg.Func(a, a), result: `$3 -> $3`},
	}

	for _, ex := range examples {
		se := must(parser.ParseExpr(ex.in))
		typ, err := Infer(&reg, DefaultScope(&reg), se, func(algo string, hash []byte) (TypeRef, error) {
			return ex.imp, nil
		})
		if err != nil {
			if ex.err != "" {
				str := err.Error()
				if !strings.Contains(str, ex.err) {
					t.Errorf("Expected '%s' to be in error:\n%s", ex.err, str)
				}
			} else {
				t.Error(err)
			}
		} else {
			typStr := reg.String(typ)
			if typStr != ex.result {
				t.Errorf("Expected %s, got %s", ex.result, typStr)
			}
		}
	}
}
