package types

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/ast"
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

		{`#r`, `[ #r, ..a ]`},
		{`#l 4`, `[ #l int, ..a ]`},
		{`f (#horse "Lucy") ; f : [#horse text, #zebra int] -> int = _ -> 1`, `int`},
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

		{`"to" |> (a -> b -> a) <| 2`, `text`},
		{`4 |> a -> b -> a`, `a -> int`},

		{`#fun (x -> x * 2)`, `[ #fun (int -> int), ..a ]`},

		{`| "/" -> (status 200 <| page "you're on the index")
		| "/about" -> (status 200 <| page "you're on the about page")
		| _ -> notfound

		; notfound = (status 404 <| page "not found")
		; status = code -> body -> { code = code, body = body }
		; page = body -> "<!doctype html><html><body>" ++ body ++ "</body></html>"`, `text -> { body : text, code : int }`},
		{`f
; f : t -> int = _ -> 1
; t : type =
  [ #a a
  , #b int
  , #c byte
  ]
; a : type = [#x, #y, #z]`, "t -> int\n; t : type = [ #a a, #b int, #c byte ]\n; a : type = [ #x, #y, #z ]"},
		{`get-a ; get-a : { a : int, b : [#x] } -> int = r -> r.a`, `{ a : int, b : [ #x ] } -> int`},
		{`ignore ; ignore : () -> {} -> [] -> int = _ -> _ -> _ -> 1`, `() -> {} -> [] -> int`},
		// Type constructors.
		{`or-zero ; or-zero : option int -> int = | #some n -> n | #none -> 0 ; option = a => [#some a, #none]`,
			"option int -> int\n; option = a => [ #none, #some a ]"},
		{`ignore ; ignore : either int (option text) -> int = _ -> 1
; option : type -> type = a => [#some a, #none]
; either : type -> type -> type = a => b => [#left a, #right b]`,
			"either int (option text) -> int\n; either = a => b => [ #left a, #right b ]\n; option = a => [ #none, #some a ]"},
		{`start ; start : turn int = { state = 1, out = "", next = #read-line }
; turn = s => { state : s, out : text, next : next-action }
; next-action : type = [#read-line, #exit int]`,
			"turn int\n; turn = a => { next : next-action, out : text, state : a }\n; next-action : type = [ #exit int, #read-line ]"},
		// A type constructor's parameters may be rows.
		{`1 ; t : type -> row -> type = a => r => { a : a, ..r } ; u = r => [#u (t int r), ..r]`, `int`},
		// Annotated bindings are checked against, and used with, their schemes.
		{`id ; id : a => a -> a = x -> x`, `a -> a`},
		{`{ a = id 1, b = id "" } ; id : a => a -> a = x -> x`, `{ a : int, b : text }`},
		{`swap ; swap : a => b => { l : a, r : b } -> { l : b, r : a } = p -> { l = p.r, r = p.l }`,
			`{ l : a, r : b } -> { l : b, r : a }`},
		{`get-a ; get-a : t => r => { a : t, ..r } -> t = x -> x.a`, `{ a : a, ..b } -> a`},
		{`get-a { a = 1, b = "" } ; get-a : t => r => { a : t, ..r } -> t = x -> x.a`, `int`},
		{`get-a ; get-a : r => has-a int r -> int = x -> x.a ; has-a = t => r => { a : t, ..r }`, "has-a int a -> int\n; has-a = a => b => { a : a, ..b }"},
		{`to-list ; to-list : a => [#some a, #none] -> list a = | #some x -> [x] | #none -> []`, `[ #none, #some a ] -> list a`},
		// An annotation informs the inference of its value's body.
		{`join ; join : text -> text -> text = a -> b -> a ++ b`, `text -> text -> text`},
		// A variable that the annotation doesn't bind keeps its own name.
		{`singleton ; singleton : b => b -> list b = x -> [x]`, `a -> list a`},
		// Annotated bindings may recurse at other types: polymorphic recursion.
		{`nest ; nest : a => int -> a -> int = | 0 -> _ -> 0 | n -> x -> nest (n - 1) [x]`, `int -> a -> int`},
		// The group's other bindings see an annotated binding's scheme.
		{`both ; both = x -> { a = id x, b = id 1 } ; id : a => a -> a = x -> x`, `a -> { a : a, b : int }`},
		{`id ; id : a => a -> a = x -> id2 x ; id2 = y -> y`, `a -> a`},
		// Type variables and aliases shadow the builtins.
		{`ignore ; ignore : t text -> int = _ -> 1 ; t = int => [#a int]`, "t text -> int\n; t = a => [ #a a ]"},
		{`empty ; empty : int -> int = _ -> "" ; int : type = text`, `text -> text`},

		{`| n >+ ns -> ns | [] -> []`, `list a -> list a`},
		{`| ns +< n -> ns | [] -> []`, `list a -> list a`},
		{`| ns ++ [a, b] -> ns | _ -> []`, `list a -> list a`},

		{`| n >+ ns -> ns | [] -> [] <| [1, 2, 3]`, `list int`},
		// Multi-pattern arms: one pattern per argument.
		{`| 1 -> b -> 1 | a -> b -> a + b`, `int -> int -> int`},
		{`| _ -> [] -> 0 | _ -> x >+ _ -> x`, `a -> list int -> int`},
		{`| #a -> s -> s | #b -> s -> 0`, `[ #a, #b ] -> int -> int`},
		{`| 0 -> (x -> x) | _ -> (x -> 0)`, `int -> int -> int`},
		{`| ns +< n -> ns | [] -> [] <| [1, 2, 3]`, `list int`},
		{`| ns ++ [2, 3] -> ns | _ -> [] <| [1, 2, 3]`, `list int`},
		{`| [1, 2] ++ ns -> ns | _ -> [] <| [1, 2, 3]`, `list int`},
		{`| ns ++ [2, last] -> ns +< last | _ -> [] <| [1, 2, 3]`, `list int`},
		// Is empty.
		{`| [] -> #true | _ -> #false`, `list a -> [ #false, #true, ..b ]`},
		{`| a >+ _ -> #first a | _ -> #empty`, `list a -> [ #empty, #first a, ..b ]`},
		// Unifies nested types.
		{`| [] -> { empty = #true } | _ -> { empty = #false }`, `list a -> { empty : [ #false, #true, ..b ] }`},
		{`| 1 -> { list = [] } | _ -> { list = [ 1 ] }`, `int -> { list : list int }`},
		{`| #true -> [1] | #false -> []`, `[ #false, #true ] -> list int`},

		// A tag without a payload has the payload ().
		{`[#a, #a ()]`, `list [ #a, ..a ]`},
		// Tags are open; a payload tag is nullary.
		{`#true #false`, `[ #true [ #false, ..a ], ..b ]`},
		// Without a catch-all, a match accepts exactly the tags it lists.
		{`| #a #x -> 1 | #b -> 2`, `[ #a [ #x ], #b ] -> int`},
		{`| #a -> 1 | _ -> 2`, `[ #a, ..a ] -> int`},
		{`| #a #x -> 1 | #a y -> 2 | #b -> 3`, `[ #a [ #x, ..a ], #b ] -> int`},

		// Records
		{`r -> r.a`, `{ a : a, ..b } -> a`},
		{`{ a = 1, b = "" }.b`, `text`},
		{`| { a = 1 } -> 1 | { a = n } -> n`, `{ a : int } -> int`},
		{`| { ..r, a = n } -> r`, `{ a : a, ..b } -> { ..b }`},
		{`(rest { a = 1, b = 2 }).b ; rest = | { ..r, a = y } -> r`, `int`},
		{`f ; f : r => { a : int, ..r } -> { ..r } = | { ..rest, a = y } -> rest`, `{ a : int, ..a } -> { ..a }`},

		// Composition
		{`f >> g ; f = x -> x + 1 ; g = x -> [x]`, `int -> list int`},
		{`g << f ; f = x -> x + 1 ; g = x -> [x]`, `int -> list int`},

		// Bindings are generalized.
		{`{ a = 1 >+ e, b = "" >+ e } ; e = []`, `{ a : list int, b : list text }`},
		// Where-groups are recursive.
		{`x ; x = y + 1 ; y = 2`, `int`},
		// A scrap may denote a type.
		{`t ; t : type = int`, `type = int`},
		{`t ; t : type = [#a a, #b] ; a : type = [#x, #y]`, "type = [ #a a, #b ]\n; a : type = [ #x, #y ]"},
		{`t ; t : type = int -> list text`, `type = int -> list text`},
		{`option ; option = a => [#some a, #none]`, `type -> type = a => [ #none, #some a ]`},
		{`has-a ; has-a : type -> row -> type = t => r => { a : t, ..r }`, `type -> row -> type = a => b => { a : a, ..b }`},
		{`f ; f = | 0 -> 0 | n -> f (n - 1)`, `int -> int`},
		{`even
; even = | 0 -> #true | n -> odd (n - 1)
; odd = | 0 -> #false | n -> even (n - 1)`, `int -> [ #false, #true, ..a ]`},

		// Recursive types are cycles, which print by name where they recur.
		{`t ; t : type = [#a t]`, "type = t\n; t : type = [ #a t ]"},
		{`tree ; tree = a => [#leaf, #node { value : a, left : tree a, right : tree a }]`,
			"type -> type = a => tree a\n; tree = a => [ #leaf, #node { left : tree a, right : tree a, value : a } ]"},
		{`size ; size : tree -> int = | #leaf -> 0 | #node n -> 1 + size n.left + size n.right
; tree : type = [#leaf, #node { left : tree, right : tree }]`,
			"tree -> int\n; tree : type = [ #leaf, #node { left : tree, right : tree } ]"},
		{`size (#node { left = #leaf, right = #node { left = #leaf, right = #leaf } })
; size : tree -> int = | #leaf -> 0 | #node n -> 1 + size n.left + size n.right
; tree : type = [#leaf, #node { left : tree, right : tree }]`, `int`},
		{`sum ; sum : a => (a -> int) -> tree a -> int = f -> | #leaf -> 0 | #node n -> f n.value + sum f n.left + sum f n.right
; tree = a => [#leaf, #node { value : a, left : tree a, right : tree a }]`,
			"(a -> int) -> tree a -> int\n; tree = a => [ #leaf, #node { left : tree a, right : tree a, value : a } ]"},
		{`xs ; xs : ints = #cons { head = 1, tail = #cons { head = 2, tail = #nil } }
; ints : type = [#nil, #cons { head : int, tail : ints }]`, "ints\n; ints : type = [ #cons { head : int, tail : ints }, #nil ]"},
		// A recursive function may be under a where-clause.
		{`f ; f = (n -> f (n - k) ; k = 1)`, `int -> a`},
		// Aliases of primitives print as the primitive.
		{`x ; x : name = "a" ; name : type = text`, `text`},
		{`f ; f : handler = n -> n ; handler : type = int -> int`, "handler\n; handler : type = int -> int"},
		// Where an alias and an unnamed type unify, the alias is kept.
		{`[x, { a = 1 }] ; x : p = { a = 2 } ; p : type = { a : int }`, "list p\n; p : type = { a : int }"},
		{`[{ a = 1 }, x] ; x : p = { a = 2 } ; p : type = { a : int }`, "list p\n; p : type = { a : int }"},
		// Mutually recursive types each get a binding.
		{`count ; count : tree -> int = _ -> 0
; tree : type = [#node { label : text, children : forest }]
; forest : type = [#nil, #cons { head : tree, tail : forest }]`, `tree -> int
; tree : type = [ #node { children : forest, label : text } ]
; forest : type = [ #cons { head : tree, tail : forest }, #nil ]`},
		// Different recursive types with one name get primes.
		{`{ a = x, b = (y ; y : t = #end ; t : type = [#b t, #end]) } ; x : t = #end ; t : type = [#a t, #end]`,
			"{ a : t, b : t' }\n; t : type = [ #a t, #end ]\n; t' : type = [ #b t', #end ]"},
		// Mutually recursive types.
		{`a ; a : type = [#a b, #end] ; b : type = [#b a]`, "type = a\n; a : type = [ #a b, #end ]\n; b : type = [ #b a ]"},
		// Different unrollings of one type are the same type, and unify.
		{`same ; same : t -> u = x -> x ; t : type = [#a t, #z] ; u : type = [#a [#a u, #z], #z]`,
			"t -> t\n; t : type = [ #a t, #z ]"},
	}

	for _, ex := range examples {
		se := must(parser.ParseExpr(ex.source))
		var reg Registry
		res, err := Infer(&reg, nil, se, nil)
		if err != nil {
			t.Error(err)
		} else {
			typStr := reg.Describe(res)
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
		{`{ ..1, a = 1 }`, `cannot unify 'int' with '{ a : int, ..a }'`},
		// Enums

		{`1 + ~dd`, `cannot unify 'byte' with 'int'`},
		{`a ; a : int = 1.0`, `cannot unify 'float' with 'int'`},
		{`f ; f : int -> text = a -> 1`, `cannot unify 'int' with 'text'`},
		// Math
		{`1 + 1.0`, `cannot unify 'int' with 'float'`},
		// No imports.
		{`$source:0`, `<internal error> $source:0 was not loaded`},
		// Different return types.
		{`| [] -> #box 1 | _ -> #box "a"`, `cannot unify 'int' with 'text'`},
		// Different input types.
		{`| #box n -> [ n + 1 ] | #box "o" -> []`, `cannot unify 'int' with 'text'`},
		// Different fields.
		{`| [] -> { a = 1 } | _ -> { b = 1 }`, `cannot unify '{ a : int }' with '{ b : int }'`},
		{`| { a = 1 } -> 1 | { a = 1, b = 2 } -> 2`, `cannot unify '{ a : int }' with '{ a : a, b : b }'`},
		{`{ a = 1 }.b`, `cannot unify '{ a : int }' with '{ b : a, ..b }'`},
		{`(| #a -> 1 | #b -> 2) #c`, `cannot unify '[ #a, #b ]' with '[ #c, ..a ]'`},
		// A row has each label at most once; the rest of a record lacks the
		// fields taken from it, also when generalized.
		{`x -> ((| { ..rest, a = y } -> rest) x).a`, `cannot unify '{ ..a }' with '{ a : b, ..c }', which would have a twice`},
		{`r -> (rest r).a ; rest = | { ..r, a = y } -> r`, `which would have a twice`},
		// Function arguments aren't generalized.
		{`f -> ({ a = g 1, b = g "" } ; g = x -> f x)`, `cannot unify 'int' with 'text'`},
		{`x -> z -> { a = [z, [x]], b = [x, [z]] }`, `cannot construct infinite type`},
		{`x ; x = 1 ; x = 2`, `cannot bind x twice`},
		{`| a -> a -> 1`, `cannot bind a twice`},
		{`| 1 -> "a" -> 1 | a -> 1 -> a`, `cannot unify 'text' with 'int'`},
		{`ignore ; ignore : c -> int = _ -> 1 ; c : type = [#a, #a]`, `cannot define tag #a more than once`},
		{`ignore ; ignore : { a : int, a : text } -> int = _ -> 1`, `cannot define field a more than once`},
		// A recursive type recurs within a tag's payload, with the same arguments.
		{`1 ; t : type = t`, `recursive type t must recur within a tag's payload`},
		{`1 ; t : type = list t`, `recursive type t must recur within a tag's payload`},
		{`1 ; t : type = { a : t }`, `recursive type t must recur within a tag's payload`},
		{`1 ; t : type = u ; u : type = list t`, `recursive type t must recur within a tag's payload`},
		{`1 ; t = a => [#a (t (list a))]`, `recursive type t must recur with the same type arguments`},
		{`f #b ; f : t -> int = _ -> 1 ; t : type = [#a t]`, "cannot unify 't' with '[ #b, ..a ]'\n; t : type = [ #a t ]"},
		// Only functions may be recursive.
		{`x ; x : t = #next x ; t : type = [#next t]`, `x is recursive, so it must be a function`},
		{`x ; x = y ; y = f 1 ; f = _ -> x`, `x is recursive, so it must be a function`},
		{`s ; s : t = #more (_ -> s) ; t : type = [#more (() -> t)]`, `s is recursive, so it must be a function`},
		{`r ; r = { f = x -> r }`, `r is recursive, so it must be a function`},
		{`t + 1 ; t : type = int`, `cannot use type t as a value`},
		{`ignore ; ignore : [#a, ..r] -> int = _ -> 1`, `unknown row variable r`},
		// Annotations with type variables are checked with them rigid.
		{`inc ; inc : a => a -> a = x -> x + 1`, `cannot unify 'a' with 'int'`},
		{`coerce ; coerce : a => b => a -> b = x -> x`, `cannot unify 'a' with 'b'`},
		{`to-int ; to-int : a => a -> int = x -> x`, `cannot unify 'a' with 'int'`},
		{`drop-rest ; drop-rest : r => { a : int, ..r } -> { a : int } = x -> x`, `cannot unify '{ a : int, ..r }' with '{ a : int }'`},
		{`get-a ; get-a : r => { ..r } -> int = x -> x.a`, `cannot unify '{ ..r }' with '{ a : a, ..b }'`},
		{`ignore ; ignore : a => [#x, ..a] -> a = _ -> 1`, `a is a row, not a type`},
		{`id ; id : int -> (a => a) = x -> x`, `type variables must be bound at the start of an annotation`},
		// A rigid variable can't escape into a type from outside its annotation.
		{`y -> (always-y ; always-y : a => a -> a = x -> y)`, `type variable 'a' would escape its annotation through 'b'`},
		{`one ; one : a => a => a = 1`, `cannot bind type variable a twice`},
		// Kinds.
		{`1 ; t : type = a => [#a a]`, `t has kind type -> type, not type`},
		{`1 ; t : type -> type = int`, `t has kind type, not type -> type`},
		{`1 ; t : row -> type = a => [#a a]`, `t has kind type -> type, not row -> type`},
		{`1 ; t : (type -> type) -> type = f => f int`, `higher kinds are not supported yet`},
		{`1 ; t : int -> type = a => a`, `int is not a kind`},
		{`1 ; t : type -> int = a => a`, `int is not a kind of types`},
		{`1 ; t = a => a => [#a a]`, `cannot bind type variable a twice`},
		// Type constructors.
		{`ignore ; ignore : option -> int = _ -> 1 ; option = a => [#some a, #none]`, `option takes 1 type argument, not 0`},
		{`ignore ; ignore : option int text -> int = _ -> 1 ; option = a => [#some a, #none]`, `option takes 1 type argument, not 2`},
		{`ignore ; ignore : either int -> int = _ -> 1 ; either = a => b => [#left a, #right b]`, `either takes 2 type arguments, not 1`},
		{`ignore ; ignore : int text -> int = _ -> 1`, `int is not a type constructor`},
		{`ignore ; ignore : t int -> int = _ -> 1 ; t : type = int`, `t is not a type constructor`},
		{`ignore ; ignore : list -> int = _ -> 1`, `list takes 1 type argument, not 0`},
		{`ignore ; ignore : list int int -> int = _ -> 1`, `list takes 1 type argument, not 2`},
		{`1 ; t = f => f int`, `f is not a type constructor`},
		{`1 ; t = a => [#a b]`, `unknown type b`},
		{`1 ; t = a => { a : a, ..a }`, `a is a row, not a type`},
		{`ignore ; ignore : t int -> int = _ -> 1 ; t = r => { ..r }`, `type argument 1 of t must be a row variable, not int`},
		// Sort bindings are checked even when unused.
		{`1 ; t : type = [#a, #a]`, `cannot define tag #a more than once`},
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

		res, err := Infer(&reg, scope, se, nil)
		if err != nil {
			t.Error(err)
		} else {
			typ := reg.Describe(res)
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

	// option = a => [#some a, #none]
	optionVar := reg.Gen()
	option := reg.Enum(map[string]TypeRef{"some": optionVar, "none": HoleRef})
	optionParams := []Param{{Var: optionVar}}

	// has-a = t => r => { a : t, ..r }
	hasT, hasR := reg.Gen(), reg.Gen()
	hasA := reg.add(node{kind: recordKind, a: reg.row(map[string]TypeRef{"a": hasT}, hasR)})
	hasAParams := []Param{{Var: hasT}, {Var: hasR, Row: true}}

	examples := []struct {
		in     string  // The input.
		imp    TypeRef // The imported type.
		sort   bool    // Whether the import denotes imp,
		params []Param // as a type constructor with these parameters.
		result string  // The stringified result type, or
		err    string  // an error.
	}{
		// Imports aren't loaded here, so '$source:0' is sufficient to encode one.
		{in: `$source:0`, imp: IntRef, result: `int`},
		{in: `$source:0`, imp: FloatRef, result: `float`},
		{in: `1 + $source:0`, imp: FloatRef, err: `cannot unify 'int' with 'float'`},
		{in: `$source:0`, imp: a, result: `a`},
		{in: `a ; a = $source:0`, imp: a, result: `a`},
		{in: `$source:0 [ 1, 2 ]`, imp: reg.Func(a, a), result: `list int`},
		{in: `a ; a = $source:0`, imp: reg.Func(a, a), result: `a -> a`},
		{in: `{ x = a 1, y = a "" } ; a = $source:0`, imp: reg.Func(a, a), result: `{ x : int, y : text }`},
		// A scrap that denotes a type can only be used in type position.
		{in: `$source:0`, imp: IntRef, sort: true, result: `type = int`},
		{in: `ignore ; ignore : $source:0 -> int = _ -> 1`, imp: TextRef, sort: true, result: `text -> int`},
		{in: `1 + $source:0`, imp: IntRef, sort: true, err: `cannot use type $source:0 as a value`},
		{in: `ignore ; ignore : $"./t.scrap" -> int = _ -> 1`, imp: TextRef, sort: true, result: `text -> int`},
		// An imported type constructor is applied by substitution.
		{in: `$source:0`, imp: option, sort: true, params: optionParams, result: `type -> type = a => [ #none, #some a ]`},
		{in: `or-zero ; or-zero : $source:0 int -> int = | #some n -> n | #none -> 0`,
			imp: option, sort: true, params: optionParams, result: `$source:0 int -> int`},
		{in: `wrap ; wrap : a => a -> $source:0 a = x -> #some x`,
			imp: option, sort: true, params: optionParams, result: `a -> $source:0 a`},
		{in: `get-a ; get-a : r => $source:0 int r -> int = x -> x.a`,
			imp: hasA, sort: true, params: hasAParams, result: `$source:0 int a -> int`},
		{in: `ignore ; ignore : $source:0 -> int = _ -> 1`, imp: option, sort: true, params: optionParams,
			err: `$source:0 takes 1 type argument, not 0`},
		{in: `ignore ; ignore : $source:0 int text -> int = _ -> 1`, imp: option, sort: true, params: optionParams,
			err: `$source:0 takes 1 type argument, not 2`},
		{in: `ignore ; ignore : $source:0 int -> int = _ -> 1`, imp: IntRef, sort: true,
			err: `$source:0 is not a type constructor`},
		{in: `ignore ; ignore : $source:0 int int -> int = _ -> 1`, imp: hasA, sort: true, params: hasAParams,
			err: `type argument 2 of $source:0 must be a row variable, not int`},
		{in: `one ; one : $source:0 = 1`, imp: IntRef, err: `$source:0 is a value, not a type`},
	}

	for _, ex := range examples {
		se := must(parser.ParseExpr(ex.in))
		imports := make(Imports)
		for _, n := range se.Imports() {
			imports[n] = Result{Type: ex.imp, Sort: ex.sort, Params: ex.params}
		}
		res, err := Infer(&reg, nil, se, imports)
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
			typStr := reg.Describe(res)
			if typStr != ex.result {
				t.Errorf("Expected %s, got %s", ex.result, typStr)
			}
		}
	}
}

func TestInferNodes(t *testing.T) {
	examples := []struct {
		in    string
		types map[string]string // A node's text, and its type.
	}{
		{`f 1 ; f = a -> a`, map[string]string{
			`f 1`:    `int`,
			`f`:      `int -> int`, // The use is instantiated.
			`1`:      `int`,
			`a -> a`: `a -> a`,
			`a`:      `a`,
		}},
		{`g (#x 2) ; g : [#x int, #y] -> int = | #x n -> n + 1 | #y -> 0`, map[string]string{
			`#x 2`:                `[ #x int, #y ]`,
			`#x n`:                `[ #x int, #y ]`,
			`n`:                   `int`,
			`n + 1`:               `int`,
			`[#x int, #y] -> int`: ``, // A type expression has no type.
		}},
		{`{ a = x } ; x = [1]`, map[string]string{
			`{ a = x }`: `{ a : list int }`,
			`x`:         `list int`,
			`[1]`:       `list int`,
		}},
	}

	for _, ex := range examples {
		var reg Registry
		se := must(parser.ParseExpr(ex.in))
		res := must(Infer(&reg, nil, se, nil))
		found := make(map[string]string)
		for n := ast.None + 1; int(n) < len(se.Kinds); n++ {
			text := se.NodeText(n)
			if _, ok := ex.types[text]; !ok {
				continue
			}
			typ := ""
			if ref := res.Nodes[n]; ref != NoType {
				strs, _ := reg.Strings(ref)
				typ = strs[0]
			}
			if prev, ok := found[text]; ok && prev != typ {
				t.Errorf("%s: nodes %q have types %q and %q", ex.in, text, prev, typ)
			}
			found[text] = typ
		}
		for text, want := range ex.types {
			if got, ok := found[text]; !ok {
				t.Errorf("%s: no node %q", ex.in, text)
			} else if got != want {
				t.Errorf("%s: node %q has type %q, want %q", ex.in, text, got, want)
			}
		}
	}
}
