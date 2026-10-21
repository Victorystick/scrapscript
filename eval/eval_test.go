package eval

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"testing"
)

var expressions = []struct {
	source string
	result string
}{
	// Literals
	{`1`, `1`},
	{`1.0`, `1.0`},
	{`1.0002`, `1.0002`},
	{`"hello"`, `"hello"`},
	{`~41`, `~41`},
	{`~~aGVsbG8gd29ybGQ=`, `~~aGVsbG8gd29ybGQ=`},
	// Where
	{`200 + (x ; x = 150)`, `350`},
	{`a + b + c ; a = 1 ; b = 2 ; c = 3`, `6`},
	{"-- doc\na + b -- sum\n; a = 1\n---- separator\n; b = 2 --\n", `3`},
	// Binary ops
	{`1 + 2`, `3`},
	{`1 + 3 * 3`, `10`},
	{`1.0 + 2.0`, `3.0`},
	{`3 - 2`, `1`},
	{`3.0 - 2.0`, `1.0`},
	{`10 - 3 - 2`, `5`},
	// Recursive types.
	{`size (#node { left = #leaf, right = #node { left = #leaf, right = #leaf } })
; size : tree -> int = | #leaf -> 0 | #node n -> 1 + size n.left + size n.right
; tree : type = [#leaf, #node { left : tree, right : tree }]`, `2`},
	{`take 3 (ones ())
; take : int -> stream -> list int = | 0 -> _ -> [] | n -> #more s -> s.head >+ take (n - 1) (s.tail ())
; ones : () -> stream = _ -> #more { head = 1, tail = ones }
; stream : type = [#more { head : int, tail : () -> stream }]`, `[ 1, 1, 1 ]`},
	{`f 2 * 3 ; f = x -> x + 1`, `9`},
	{`1 >+ 2 >+ [3]`, `[ 1, 2, 3 ]`},
	{`[1] +< 2 +< 3`, `[ 1, 2, 3 ]`},
	{`1.0 + to-float 1`, `2.0`},
	{`"hello" ++ " " ++ "world"`, `"hello world"`},
	// Functions
	{`2 |> | _ -> 3`, `3`},
	{`f #true ; f = | #true -> 1 | #false -> 2`, `1`},
	{`f 2 ; f = | a -> a + a`, `4`},
	{`2 |> | a -> a + a`, `4`},
	{`#l 5 |> | #l n -> n * 2 | #r n -> n * 3`, `10`},
	{`f "b"
; f =
  | "a" -> 1
  | "b" -> 2
  | "c" -> 3
  |  x  -> 0`, `2`},
	{`f 1 2 ; f = a -> b -> a + b`, `3`},
	// Multi-pattern arms match all their arguments; a later pattern falls
	// through to the next arm.
	{`f 5 8 ; f = | 1 -> 1 -> 1 | a -> b -> a + b`, `13`},
	{`f 1 8 ; f = | 1 -> 1 -> 1 | a -> b -> a + b`, `9`},
	{`f 1 1 ; f = | 1 -> 1 -> 1 | a -> b -> a + b`, `1`},
	// A chain of lambdas, and the match function it ends in, take all
	// their arguments at once; later names shadow earlier ones.
	{`f 1 2 ; f = a -> a -> a`, `2`},
	{`[f 1 0, f 1 5] ; f = n -> | 0 -> n | n -> n * 10`, `[ 1, 50 ]`},
	// Fewer arguments than a function takes, and more.
	{`g 2 ; g = f 1 ; f = a -> b -> a - b`, `-1`},
	{`f 1 2 ; f = a -> (g ; g = b -> a + b)`, `3`},
	{`f 1 2 3 ; f = a -> b -> (g ; g = c -> a + b + c)`, `6`},
	{`(f 1 2) 3 ; f = a -> b -> c -> a + b + c`, `6`},
	{`m [1, 2] ; m = list/map (x -> x + 1)`, `[ 2, 3 ]`},
	{`list/map (list/repeat 2) [1, 2]`, `[ [ 1, 1 ], [ 2, 2 ] ]`},
	{`[f #a #c, f #a #d, f #b #c] ; f = | #a -> #c -> 1 | _ -> #d -> 2 | _ -> _ -> 3`, `[ 1, 2, 3 ]`},
	{`list/map (f 1) [1, 2] ; f = | 1 -> 1 -> 0 | a -> b -> a + b`, `[ 0, 3 ]`},
	{`f "b" ; f = | "a" -> 1 | "b" -> 2 | "c" -> 3 | x -> 0`, `2`},
	{`(f >> (x -> x) >> g) 7
	  ; f =
		  | 7 -> "cat"
	    | 4 -> "dog"
	    | _ -> "shark"
	  ; g =
		  | "cat" -> "kitten"
	    | "dog" -> "puppy"
	    |   a   -> "baby " ++ a`, `"kitten"`},
	{`(f << g) 3 ; f = x -> x * 2 ; g = x -> x + 1`, `8`},
	{`(f >> g) 3 ; f = x -> x * 2 ; g = x -> x + 1`, `7`},
	{`3 |> f << g ; f = x -> [x] ; g = x -> x + 1`, `[ 4 ]`},
	// Closures in a recursive function's where capture it before it is done.
	{`f 3 ; f = (| 0 -> 0 | n -> g (n - 1) ; g = m -> f m)`, `0`},
	{`f 3 ; f = (| 0 -> 0 | n -> r.k (n - 1) ; r = { k = m -> f m })`, `0`},
	{`(x -> x) (y -> y)`, `y -> y`},
	{`#just 2 |> | #just 2 -> "two" | #just _ -> "other" | #no -> "x"`, `"two"`},
	// {`| "hey" -> ""
	// 	| "hello " ++ name -> name
	// 	| _ -> "<empty>" <| "hello Oseg"`, textValue("Oseg")},
	{`#empty`, `#empty`},
	{`#a |> | #a () -> 1`, `1`},
	{`#fun (x -> x * 2)`, `#fun (x -> x * 2)`},

	// Destructuring.
	{`{ a = 1, b = 2 } |> | { a = c, b = d } -> c + d`, `3`},
	{`{ a = 1 } |> | { a = 2 } -> 0 | { a = c } -> c`, `1`},
	{`3 |> a -> b -> a`, `(a -> b -> a) 3`},
	// Matches in one frame keep their own slots, which closures read later.
	{`[f 10, h 10] ; f = 1 |> y -> (g ; g = z -> y + z) ; h = 2 |> y -> (g ; g = z -> y * z)`, `[ 11, 20 ]`},
	{`1 |> y -> (g ; g = z -> y + z)`, "g\n; g = z -> y + z\n; y = 1"},

	{`#true #false`, `#true #false`},

	// Where-groups are recursive.
	{`f 1 ; b = 2 ; f = a -> b`, `2`},
	{`count 3
	; count = | 0 -> [] | n -> count (n - 1) +< n`, `[ 1, 2, 3 ]`},

	{`list/repeat 2`, `list/repeat 2`},
	{`text/repeat 3`, `text/repeat 3`},
	{`text/join " "`, `text/join " "`},

	{`"hi " ++ text/repeat 3 "a" ++ "ron"`, `"hi aaaron"`},
	{`"yo" |> list/repeat 2 |> text/join " "`, `"yo yo"`},

	{`| n >+ ns -> ns | [] -> [] <| [1, 2, 3]`, `[ 2, 3 ]`},
	{`| ns +< n -> ns | [] -> [] <| [1, 2, 3]`, `[ 1, 2 ]`},
	{`| ns +< n -> n | [] -> 0 <| [1, 2, 3]`, `3`},
	// Values are immutable: appending to one list twice gives two lists.
	{`{ a = xs +< 1, b = xs +< 2 } ; xs = [0, 0, 0] +< 9`, `{ a = [ 0, 0, 0, 9, 1 ], b = [ 0, 0, 0, 9, 2 ] }`},
	{`{ a = xs ++ [1], b = xs ++ [2] } ; xs = [0, 0, 0] +< 9`, `{ a = [ 0, 0, 0, 9, 1 ], b = [ 0, 0, 0, 9, 2 ] }`},
	{`{ a = bs +< ~01, b = bs +< ~02 } ; bs = ~~AAAA +< ~09`, `{ a = ~~AAAACQE=, b = ~~AAAACQI= }`},
	{`| ns ++ [2, 3] -> ns | _ -> [] <| [1, 2, 3]`, `[ 1 ]`},
	{`| [1, 2] ++ ns -> ns | _ -> [] <| [1, 2, 3]`, `[ 3 ]`},
	{`| ns ++ [2, last] -> ns +< last | _ -> [] <| [1, 2, 3]`, `[ 1, 3 ]`},
	// A catch-all binder excludes the tags that earlier arms cover.
	{`eval (#add { l = #lit 1, r = #lit 2 })
; eval : expr -> int = x -> eval-add eval eval-lit x
; expr : type = [#lit int, #add { l : expr, r : expr }]
; eval-lit = | #lit n -> n
; eval-add = self -> ext ->
    | #add { l = l, r = r } -> self l + self r
    | other -> ext other`, `3`},
	{`eval (#add { l = #lit 1, r = #mul { l = #lit 2, r = #neg (#lit 3) } })
; eval : expr -> int = x -> all eval eval-lit x
; expr : type = [#lit int, #add { l : expr, r : expr },
                 #mul { l : expr, r : expr }, #neg expr]
; all = chain eval-add (chain eval-mul eval-neg)
; chain = f -> g -> self -> ext -> f self (g self ext)
; eval-lit = | #lit n -> n
; eval-add = self -> ext ->
    | #add { l = l, r = r } -> self l + self r
    | other -> ext other
; eval-mul = self -> ext ->
    | #mul { l = l, r = r } -> self l * self r
    | other -> ext other
; eval-neg = self -> ext ->
    | #neg e -> 0 - self e
    | other -> ext other`, `-5`},
	// Each use of the binder may or may not have the excluded tags.
	{`{ a = f (#some 1), b = f #none }
; f = | #none -> #some 0 | o -> map inc o
; map = g -> | #some x -> #some (g x) | #none -> #none
; inc = n -> n + 1`, `{ a = #some 2, b = #some 0 }`},
	{`f (#some 1)
; f = | #none -> { a = #none, b = 0 } | o -> { a = map inc o, b = ext o }
; map = g -> | #some x -> #some (g x) | #none -> #none
; inc = n -> n + 1
; ext = | #some n -> n`, `{ a = #some 2, b = 1 }`},
	{`f #b
; f = | #a -> 0 | r -> with-a r + without-a r
; with-a : [#a, #b] -> int = | #a -> 1 | #b -> 2
; without-a : [#b] -> int = | #b -> 3`, `5`},
	// An arm covers a tag when it tests nothing but the tag, on the path to
	// the binder.
	{`{ a = f (#a 1), b = f #b } ; f = | #a x -> x | r -> ext r ; ext = | #b -> 2`, `{ a = 1, b = 2 }`},
	{`{ a = f #a, b = f (#b 2), c = f #c } ; f = | #a -> 1 | #b n -> n | r -> ext r ; ext = | #c -> 3`, `{ a = 1, b = 2, c = 3 }`},
	{`f (#a { x = 1 }) ; f = | #a { x = x } -> x | r -> ext r ; ext = | #b -> 2`, `1`},
	{`{ a = f (#a 1) 5, b = f #b 0 } ; f = | #a x -> _ -> x | r -> 0 -> ext r | _ -> _ -> 2 ; ext = | #b -> 3`, `{ a = 1, b = 3 }`},
	{`{ a = f (#wrap #a), b = f (#wrap #b) } ; f = | #wrap #a -> 1 | #wrap r -> ext r ; ext = | #b -> 2`, `{ a = 1, b = 2 }`},
}

func TestScrapItentity(t *testing.T) {
	var scraps = []string{
		`#horse "Lucy"`,
	}

	for _, scrap := range scraps {
		env := NewEnvironment()
		val, err := eval(env, scrap)

		if err != nil {
			t.Error(err)
		} else {
			rep := env.Print(val)
			if scrap != rep {
				t.Errorf("Expected: %#v, got: %#v", scrap, rep)
			}
		}
	}
}

var failures = []struct {
	source string
	error  string
}{
	{`f 1 ; f = a -> b`, "unbound variable: b"},
	{`{} |> | { b = a } -> a`, "cannot unify '{ b : a }' with '{}'"},
	{`[ 1 ] |> | [] -> "four"`, `match is not exhaustive: no arm matches [ _ ] ++ _`},
	{`[] ++ ""`, `cannot unify 'list a' with 'text'`},
	{`"" ++ []`, `cannot unify 'text' with 'list a'`},
	{`1 -> x`, `function parameter must be an identifier`},
	{`#left 5 |> | #l n -> n | #r n -> n`, `cannot unify '[ #l a, #r a ]' with '[ #left int, ..b ]'`},
	{`{ a = 2 } |> | { a = a, b = b } -> ()`, `cannot unify '{ a : a, b : b }' with '{ a : int }'`},
	{`{ a = 2, b = 1 } |> | { a = a, b = a } -> ()`, `cannot bind a twice`},
	{`f ; f : c -> int = _ -> 1 ; c : type = [#a, #a]`, `cannot define tag #a more than once`},
	{`g ; g : [#x f] -> int = _ -> 1 ; f = x -> 2`, `unknown type f`},
	{`a::x`, `Expected EOF got DEFINE`},
	{`["a"] +< ~be`, `cannot unify 'list text' with 'bytes'`},
	{`1 >+ [~~abcd]`, `cannot unify 'bytes' with 'int'`},
	{`[1, 1.2]`, `cannot unify 'int' with 'float'`},
	{`{ b = 1 }.a`, `cannot unify '{ b : int }' with '{ a : a, ..b }'`},
	{`{ ..{ a = 2, c = 1 }, a = 1, b = "x"}`, `cannot unify '{ a : int, c : int }' with '{ a : int, b : text, ..a }'`},
	{`{ ..{ a = 2 }, a = "x"}`, `cannot unify 'int' with 'text'`},
	// Builtins fail on their values.
	{`list/repeat -1 0`, `list/repeat: negative count -1`},
	{`text/repeat -1 "a"`, `text/repeat: negative count -1`},
	{`bytes/to-utf8-text ~~/w==`, `is not valid UTF-8`},
}

func TestEval(t *testing.T) {
	for _, ex := range expressions {
		evalString(t, ex.source, ex.result)
	}
}

var exp2str = []struct{ source, result string }{
	{`()`, `()`},

	{`bytes/to-utf8-text <| ~~aGVsbG8gd29ybGQ= +< ~21`, `"hello world!"`},
	{`~~aGVsbG8gd29ybGQ= +< ~21 |> bytes/to-utf8-text`, `"hello world!"`},
	{`bytes/to-utf8-text ~~aGVsbG8gd29ybGQ=`, `"hello world"`},
	{`bytes/from-utf8-text "hello world"`, `~~aGVsbG8gd29ybGQ=`},

	{`1 >+ [2, 3] +< 4`, `[ 1, 2, 3, 4 ]`},
	{`1 >+ []`, `[ 1 ]`},
	{`[] +< 2`, `[ 2 ]`},
	{`[ 3 ] ++ []`, `[ 3 ]`},
	{`[] ++ [ 4 ]`, `[ 4 ]`},
	{`["prefix"] ++ ["in" ++ "fix"] +< "postfix"`, `[ "prefix", "infix", "postfix" ]`},
	// Records
	{`rec.a ; rec = { a = 1, b = "x" }`, `1`},
	{`{ ..g, a = 2, c = ~FF }
	; g = { a = 1, b = "x", c = ~00 }`, `{ a = 2, b = "x", c = ~FF }`},
	{`{ ..{ a = 2, c = 1 }, a = 1 }`, `{ a = 1, c = 1 }`},
	{`{ a = 2, b = 3, c = 4 } |>
    | { ..x, a = 1, b = 2, c = 3 } -> 0
    | { ..x, a = 1, b = b        } -> b
    | { ..x,               c = c } -> c + x.a`, `6`},
	{`{ c = 1, a = 2, b = 3 }`, `{ a = 2, b = 3, c = 1 }`},
	{`f { b = 1, a = 2 } + f { a = 3, z = 4 } ; f = r -> r.a`, `5`},
	{`{ b = 1, a = 2 } |> | { a = a, b = b } -> a - b`, `1`},
	{`{ a = 1, b = 2, c = 3 } |> | { ..x, b = _ } -> { ..x, c = x.a }`, `{ a = 1, c = 1 }`},
	{`~ff`, "~FF"},
	{`~~abcd`, "~~abcd"},
	{`f 1 <| 2 ; f = a -> b -> a + b`, "3"},
	{`2 |> f <| 3 ; f = a -> b -> a - b`, "-1"},
	{`f <| 3 |> g ; f = a -> [a] ; g = a -> a + 1`, "[ 4 ]"},
	{`f 1 2 ; f = a -> b -> a + b`, "3"},
	{`1 + 2 * floor 3.4`, "7"},
	{`2 * ceil 2.2 + 1`, "7"},
	{`-3 - 5`, `-8`},

	{`(#l 5 |>
			| #l n -> n * 2
			| #r n -> n * 3)
	; double : int -> int = n -> n * 2`, `10`},

	{`list/map text/length ["hey", "beautiful"]`, `[ 3, 9 ]`},
	{`list/map text/length`, `list/map text/length`},

	{`list/fold 0 (a -> b -> a + b) []`, `0`},
	{`list/fold 0 (a -> b -> a + b)`, `list/fold 0 (a -> b -> a + b)`},
	{`list/fold 0 (a -> b -> a + b) [1, 2]`, `3`},
	{`list/fold 0 (a -> b -> a + text/length b) ["hey", "beautiful"]`, `12`},
	// A partially applied fold starts from its initial value every time.
	{`[g [1, 2], g [1, 2]] ; g = list/fold 0 (a -> b -> a + b)`, `[ 3, 3 ]`},

	{`[ 4 + 2, 5 - 1 ]`, "[ 6, 4 ]"},
	{`[ 1, 4 ] |> | [1,3] -> "three" |[_,4] -> "four" | _ -> "other"`, `"four"`},

	{`[int/cmp 1 2, int/cmp 2 2, int/cmp 3 2]`, `[ #lt, #eq, #gt ]`},
	{`[int/div 7 2, int/div -7 2, int/div 1 0]`, `[ #some 3, #some -3, #none ]`},
	{`int/to-text -42 ++ "!"`, `"-42!"`},
	{`[text/cmp "a" "b", text/cmp "b" "b", text/cmp "b" "a"]`, `[ #lt, #eq, #gt ]`},
	{`text/split " " "GET /a 200"`, `[ "GET", "/a", "200" ]`},
	{`text/split "," ""`, `[ "" ]`},
	{`list/map text/to-int ["12", "-3", "x", ""]`, `[ #some 12, #some -3, #none, #none ]`},
	{`list/sort int/cmp [3, 1, 2]`, `[ 1, 2, 3 ]`},
	// Stable: equal elements keep their order.
	{`list/sort (a -> b -> int/cmp a.k b.k) [{ k = 2, v = 1 }, { k = 1, v = 2 }, { k = 2, v = 3 }]`,
		`[ { k = 1, v = 2 }, { k = 2, v = 1 }, { k = 2, v = 3 } ]`},
	{`[max [], max [3, -1, 7, 2]]
	; max : list int -> [#some int, #none] =
	  | []      -> #none
	  | x >+ xs -> #some (list/fold x bigger xs)
	; bigger = a -> b -> int/cmp a b |> | #lt -> b | _ -> a`, `[ #none, #some 7 ]`},

	// Generates a list of numbers from 0 to n inclusive.
	{`seq 4 ; seq = | 0 -> [0] | n -> seq (n - 1) +< n`, `[ 0, 1, 2, 3, 4 ]`},

	{`list/map fib (seq 10)
	; seq = | 0 -> [0] | n -> seq (n - 1) +< n
    ; fib =
        | 0 -> 0
        | 1 -> 1
        | n -> fib (n - 1) + fib (n - 2)`, `[ 0, 1, 1, 2, 3, 5, 8, 13, 21, 34, 55 ]`},
}

func TestEvalString(t *testing.T) {
	for _, ex := range exp2str {
		evalString(t, ex.source, ex.result)
	}
}

// Calls in tail position run in constant stack, however deep they recurse.
var tailCalls = []struct{ source, result string }{
	{`count 1000000 ; count = | 0 -> "done" | n -> count (n - 1)`, `"done"`},
	{`sum 0 1000000 ; sum = acc -> | 0 -> acc | n -> sum (acc + n) (n - 1)`, `500000500000`},
	{`even 1000001
; even = | 0 -> #true | n -> odd (n - 1)
; odd = | 0 -> #false | n -> even (n - 1)`, `#false`},
	{`count 1000000 ; count = | 0 -> 0 | n -> (count m ; m = n - 1)`, `0`},
	{`count 1000000 ; count = | 0 -> 0 | n -> (n - 1 |> count)`, `0`},
	{`count 1000000 ; count = | 0 -> 0 | n -> (dec >> count) n ; dec = n -> n - 1`, `0`},
	{`count 1000000 ; count = | 0 -> 0 | n -> (| m -> count m) (n - 1)`, `0`},
	// Calls with several arguments, all at once or more than it takes.
	{`loop 1000000 0 ; loop = | 0 -> acc -> acc | n -> acc -> loop (n - 1) (acc + 1)`, `1000000`},
	{`loop 0 1000000 ; loop = acc -> | 0 -> acc | n -> loop (acc + 1) (n - 1)`, `1000000`},
	{`loop 1000000 0 ; loop = n -> (step ; step = | acc -> (| 0 -> acc | _ -> loop (n - 1) (acc + 1)) n)`, `1000000`},
}

func TestTailCalls(t *testing.T) {
	// Without tail calls, these overflow the stack and crash.
	defer debug.SetMaxStack(debug.SetMaxStack(16 << 20))
	for _, ex := range tailCalls {
		evalString(t, ex.source, ex.result)
	}
}

// An expression language built from fragments that each handle some tags and
// pass the rest on, which presence types.
func TestExpressionFragments(t *testing.T) {
	source, err := os.ReadFile("testdata/expr.scrap")
	if err != nil {
		t.Fatal(err)
	}
	evalString(t, string(source), `{ small = #ok -1, small-text = "(1 + -2)", `+
		`text = "let x = (2 + 3) in if (x < 10) then (x * x) else -x", unbound = #err "unbound y", value = #ok 25 }`)
}

func TestFailures(t *testing.T) {
	for _, ex := range failures {
		evalFailure(t, ex.source, ex.error)
	}
}

func eval(e *Environment, source string) (Value, error) {
	scrap, err := e.Read([]byte(source))
	if err != nil {
		return Value{}, err
	}
	return e.Eval(scrap)
}

// Evaluates an expression and compares the string representation of the
// result with a target string; optionally with some additional variables
// in scope.
func evalString(t *testing.T, source, expected string) {
	env := NewEnvironment()
	val, err := eval(env, source)

	if err != nil {
		t.Error(err)
	} else {
		if got := env.Print(val); got != expected {
			t.Errorf("Expected: %#v, got: %#v", expected, got)
		}
	}
}

// Evaluates to a comparable value
func evalFailure(t *testing.T, source string, expected string) {
	env := NewEnvironment()
	val, err := eval(env, source)

	if err == nil {
		t.Errorf("%s - should fail but got %s", source, env.Print(val))
	} else {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("Expected '%s' in error:\n%s", expected, err.Error())
		}
	}
}

func TestEvalImport(t *testing.T) {
	env := NewEnvironment()
	env.UseFetcher(MapFetcher{
		"a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447": `3 + $source:a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445`,
		"a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445": `2`,
	})

	val, err := eval(env, `$source:a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447 - 1`)
	if err != nil {
		t.Error(err)
	} else {
		if got := env.Print(val); got != "4" {
			t.Errorf("Expected: %#v, got: %#v", "4", got)
		}
	}
}

func TestImportType(t *testing.T) {
	env := NewEnvironment()
	env.UseFetcher(MapFetcher{
		"a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445": `t ; t : type = [#a int, #b]`,
	})

	scrap, err := env.Read([]byte(`or-zero (#a 1) ; or-zero : $source:a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445 -> int = | #a n -> n | #b -> 0`))
	if err != nil {
		t.Fatal(err)
	}
	val, err := env.Eval(scrap)
	if err != nil {
		t.Fatal(err)
	}
	if got := env.Print(val); got != "1" {
		t.Errorf("Expected 1, got %s", got)
	}

	// The type scrap itself has no value.
	scrap, err = env.Read([]byte(`t ; t : type = [#a int, #b]`))
	if err != nil {
		t.Fatal(err)
	}
	typ, err := env.Infer(scrap)
	if err != nil || typ != "type = [ #a int, #b ]" {
		t.Errorf("Expected type = [ #a int, #b ], got %s, %v", typ, err)
	}
	if _, err = env.Eval(scrap); err == nil || err.Error() != "cannot evaluate a type" {
		t.Errorf("Expected 'cannot evaluate a type', got %v", err)
	}
}

func TestImportTypeMember(t *testing.T) {
	const lib = "a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445"
	env := NewEnvironment()
	env.UseFetcher(MapFetcher{
		lib: `{ option = option, none = #none } ; option = a => [#some a, #none]`,
	})

	// A path reaches the member through a name bound to the import, or the
	// import itself, the same as it would with the library inlined.
	for _, src := range []string{
		`f ; f : l.option int -> int = | #some n -> n | #none -> 0 ; l = $source:` + lib,
		`f ; f : $source:` + lib + `.option int -> int = | #some n -> n | #none -> 0`,
		`f ; f : l.option int -> int = | #some n -> n | #none -> 0 ; l = ({ option = option, none = #none } ; option = a => [#some a, #none])`,
	} {
		scrap, err := env.Read([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if typ, err := env.Infer(scrap); err != nil || !strings.HasSuffix(strings.SplitN(typ, "\n", 2)[0], " int -> int") {
			t.Errorf("%s: expected ... int -> int, got %s, %v", src, typ, err)
		}
	}

	// The library's value leaves its members out, also when evaluated.
	scrap, err := env.Read([]byte(`$source:` + lib))
	if err != nil {
		t.Fatal(err)
	}
	if typ, err := env.Infer(scrap); err != nil || typ != "{ none : [ #none, ..a ] }" {
		t.Errorf("Expected { none : [ #none, ..a ] }, got %s, %v", typ, err)
	}
	if val, err := env.Eval(scrap); err != nil || env.Print(val) != "{ none = #none }" {
		t.Errorf("Expected { none = #none }, got %v, %v", val, err)
	}
	scrap, err = env.Read([]byte(`f l.none ; f : l.option int -> int = | #some n -> n | #none -> 0 ; l = $source:` + lib))
	if err != nil {
		t.Fatal(err)
	}
	if val, err := env.Eval(scrap); err != nil || env.Print(val) != "0" {
		t.Errorf("Expected 0, got %v, %v", val, err)
	}

	// Printed as a scrap, the library keeps its members, through an import
	// too. A binding that only types refer to prints as the import.
	for _, ex := range []struct{ source, result string }{
		{`$source:` + lib, "{ none = #none, option = option }\n; option = a => [ #some a, #none ]"},
		{`f ; f : l.option int -> int = | #some n -> n | #none -> 0 ; l = $source:` + lib,
			"f\n; f : l.option int -> int =\n  | #some n -> n\n  | #none -> 0\n; l = $source:" + lib},
		{`f ; f = x -> l.none ; l = $source:` + lib, "f\n; f = x -> l.none\n; l = $source:" + lib},
	} {
		scrap, err := env.Read([]byte(ex.source))
		if err != nil {
			t.Fatal(err)
		}
		if got, err := env.PrintScrap(scrap); err != nil || got != ex.result {
			t.Errorf("%s: expected %q, got %q, %v", ex.source, ex.result, got, err)
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
