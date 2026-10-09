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
		{`[]`, `list a`}, // empty list has an unbound type for its values
		{`[1, 2]`, `list int`},
		// Records
		{`{ a = 1 }`, `{ a : int }`},
		{`{ ..base, a = ~01 } ; base = { a = ~00 }`, `{ a : byte }`},
		// // Enums

		{`#r`, `[ #r, ...a ]`},
		{`#l 4`, `[ #l int, ...a ]`},
		{`f (#horse "Lucy") ; f : (#horse text #zebra int) -> int = _ -> 1`, `int`},
		// Functions
		{`a -> a`, `a -> a`},
		{`_ -> "hi"`, `a -> text`},
		{`_ -> _ -> "hi"`, `a -> b -> text`},
		{`(_ -> "hi") ()`, `text`},

		// Prepend and append
		{`a -> a >+ []`, `a -> list a`},
		{`a -> a +< 1`, `list int -> list int`},
		{`a -> a >+ ~~1111`, `byte -> bytes`},
		{`a -> a +< ~ff`, `bytes -> bytes`},

		// Concat
		{`"hi " ++ "you!"`, `text`},
		{`[] ++ [1]`, `list int`},
		{`~~1111 ++ ~~`, `bytes`},
		{`a -> b -> a ++ b`, `list a -> list a -> list a`},

		// Math
		{`a -> 1.0 + a`, `float -> float`},
		{`4 - 3`, `int`},
		{`a -> b -> a * b`, `int -> int -> int`}, // Default to int.

		{`a -> b -> { a = a, b = b }`, `a -> b -> { a : a, b : b }`},
		{`(a -> b -> { a = a, b = b }) 1`, `a -> { a : int, b : a }`},
		{`(a -> b -> { a = a, b = b }) 1 "yo" `, `{ a : int, b : text }`},
		{`a ; a : int = 1`, `int`},
		{`a -> a + 1`, `int -> int`},
		{`b -> (a ; a : int = b)`, `int -> int`},

		{`f -> f (f 1)`, `(int -> int) -> int`},
		{`a -> f -> f (f a)`, `a -> (a -> a) -> a`},
		{`f -> a -> f (f a)`, `(a -> a) -> a -> a`},

		{`f -> a -> [ a ]`, `a -> b -> list b`},
		{`(f -> a -> [ a ]) "a"`, `a -> list a`},
		{`(f -> a -> [ a ]) "a" 3`, `list int`},

		{`a -> (b ; b = a)`, `a -> a`},
		{`f -> a -> ([ b, b ] ; b = (f a))`, `(a -> b) -> a -> list b`},
		// If used the same, arguments must be the same.
		{`a -> b -> [ a, b ]`, `a -> a -> list a`},
		{`(a -> b -> [ a, b ]) 1`, `int -> list int`},

		{`"to" |> a -> b -> a <| 2`, `text`},
		{`4 |> a -> b -> a`, `a -> int`},

		{`#fun (x -> x * 2)`, `[ #fun (int -> int), ...a ]`},

		{`| "/" -> (status 200 <| page "you're on the index")
		| "/about" -> (status 200 <| page "you're on the about page")
		| _ -> notfound

		; notfound = (status 404 <| page "not found")
		; status = code -> body -> { code = code, body = body }
		; page = body -> "<!doctype html><html><body>" ++ body ++ "</body></html>"`, `text -> { body : text, code : int }`},
		{`f
; f : t -> int = _ -> 1
; t :
  #a a
  #b int
  #c byte
; a :
  #x
  #y
  #z`, `[ #a [ #x, #y, #z ], #b int, #c byte ] -> int`},

		{`| n >+ ns -> ns`, `list a -> list a`},
		{`| ns +< n -> ns`, `list a -> list a`},
		{`| ns ++ [a, b] -> ns`, `list a -> list a`},

		{`| n >+ ns -> ns <| [1, 2, 3]`, `list int`},
		{`| ns +< n -> ns <| [1, 2, 3]`, `list int`},
		{`| ns ++ [2, 3] -> ns <| [1, 2, 3]`, `list int`},
		{`| [1, 2] ++ ns -> ns <| [1, 2, 3]`, `list int`},
		{`| ns ++ [2, last] -> ns +< last <| [1, 2, 3]`, `list int`},
		// Is empty.
		{`| [] -> #true | _ -> #false`, `list a -> [ #false, #true, ...b ]`},
		{`| a >+ _ -> #first a | _ -> #empty`, `list a -> [ #empty, #first a, ...b ]`},
		// Unifies nested types.
		{`| [] -> { empty = #true } | _ -> { empty = #false }`, `list a -> { empty : [ #false, #true, ...b ] }`},
		{`| 1 -> { list = [] } | _ -> { list = [ 1 ] }`, `int -> { list : list int }`},
		{`| #true -> [1] | #false -> []`, `[ #false, #true ] -> list int`},

		// Tags are open; a payload tag is nullary.
		{`#true #false`, `[ #true [ #false, ...a ], ...b ]`},
		// Without a catch-all, a match accepts exactly the tags it lists.
		{`| #a #x -> 1 | #b -> 2`, `[ #a [ #x ], #b ] -> int`},
		{`| #a -> 1 | _ -> 2`, `[ #a, ...a ] -> int`},
		{`| #a #x -> 1 | #a y -> 2 | #b -> 3`, `[ #a [ #x, ...a ], #b ] -> int`},

		// Records
		{`r -> r.a`, `{ a : a, ...b } -> a`},
		{`{ a = 1, b = "" }.b`, `text`},
		{`| { a = 1 } -> 1 | { a = n } -> n`, `{ a : int } -> int`},
		{`| { ..r, a = n } -> r`, `{ a : a, ...b } -> { ...b }`},

		// Composition
		{`f >> g ; f = x -> x + 1 ; g = x -> [x]`, `int -> list int`},

		// Bindings are generalized.
		{`{ a = 1 >+ e, b = "" >+ e } ; e = []`, `{ a : list int, b : list text }`},
		// Where-groups are recursive.
		{`x ; x = y + 1 ; y = 2`, `int`},
		{`f ; f = | 0 -> 0 | n -> f (n - 1)`, `int -> int`},
		{`even
; even = | 0 -> #true | n -> odd (n - 1)
; odd = | 0 -> #false | n -> even (n - 1)`, `int -> [ #false, #true, ...a ]`},
	}

	for _, ex := range examples {
		se := must(parser.ParseExpr(ex.source))
		var reg Registry
		typ, err := Infer(&reg, nil, se, nil)
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
		{`4 ++ 6`, `cannot unify 'int' with 'list a'`},
		// Records
		{`{ ..base, a = 1 } ; base = { a = ~00 }`, `cannot unify 'byte' with 'int'`},
		{`{ ..1, a = 1 }`, `cannot unify 'int' with '{ a : int, ...a }'`},
		// Enums

		{`1 + ~dd`, `cannot unify 'byte' with 'int'`},
		{`a ; a : int = 1.0`, `cannot unify 'float' with 'int'`},
		{`f ; f : int -> text = a -> 1`, `cannot unify 'int' with 'text'`},
		// Math
		{`1 + 1.0`, `cannot unify 'int' with 'float'`},
		// No imports.
		{`$sha256~~`, `<internal error> missing infer import function`},
		// Different return types.
		{`| [] -> #box 1 | _ -> #box "a"`, `cannot unify 'int' with 'text'`},
		// Different input types.
		{`| #box n -> [ n + 1 ] | #box "o" -> []`, `cannot unify 'int' with 'text'`},
		// Different fields.
		{`| [] -> { a = 1 } | _ -> { b = 1 }`, `cannot unify '{ a : int }' with '{ b : int }'`},
		{`| { a = 1 } -> 1 | { a = 1, b = 2 } -> 2`, `cannot unify '{ a : int }' with '{ a : a, b : b }'`},
		{`{ a = 1 }.b`, `cannot unify '{ a : int }' with '{ b : a, ...b }'`},
		{`(| #a -> 1 | #b -> 2) #c`, `cannot unify '[ #a, #b ]' with '[ #c, ...a ]'`},
		// Function arguments aren't generalized.
		{`f -> ({ a = g 1, b = g "" } ; g = x -> f x)`, `cannot unify 'int' with 'text'`},
		{`x -> z -> { a = [z, [x]], b = [x, [z]] }`, `cannot construct infinite type`},
		{`x ; x = 1 ; x = 2`, `cannot bind x twice`},
		{`f ; f : c -> int = _ -> 1 ; c : #a #a`, `cannot define tag #a more than once`},
		{`f ; f : t -> int = _ -> 1 ; t : #a t`, `recursive type t is not supported yet`},
	}

	for _, ex := range examples {
		var reg Registry
		se := must(parser.ParseExpr(ex.source))
		_, err := Infer(&reg, nil, se, nil)
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
		{`len`, `list a -> int`},
		{`len []`, `int`},
		{`(f -> a -> ([ b, b ] ; b = (f a))) len`, `list a -> list int`},
		{`(f -> a -> ([ b, b ] ; b = (f a))) len []`, `list int`},

		{`{ a = id 1, b = id "" }`, `{ a : int, b : text }`},
		{`{ a = id2 1, b = id2 "" } ; id2 = a -> a`, `{ a : int, b : text }`},
	}

	for _, ex := range examples {
		se := must(parser.ParseExpr(ex.source))

		// New registry every test.
		reg := Registry{}
		var scope *Scope[TypeRef]

		scope = scope.Bind("len", reg.Func(reg.List(reg.Gen()), IntRef))

		a := reg.Gen()
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

	a := reg.Gen()

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
		{in: `$sha256~~`, imp: a, result: `a`},
		{in: `a ; a = $sha256~~`, imp: a, result: `a`},
		{in: `$sha256~~ [ 1, 2 ]`, imp: reg.Func(a, a), result: `list int`},
		{in: `a ; a = $sha256~~`, imp: reg.Func(a, a), result: `a -> a`},
		{in: `{ x = a 1, y = a "" } ; a = $sha256~~`, imp: reg.Func(a, a), result: `{ x : int, y : text }`},
	}

	for _, ex := range examples {
		se := must(parser.ParseExpr(ex.in))
		typ, err := Infer(&reg, nil, se, func(algo string, hash []byte) (TypeRef, error) {
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
