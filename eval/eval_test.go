package eval

import (
	"fmt"
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
	{`(x -> x) (y -> y)`, `y -> y`},
	{`#just 2 |> | #just 2 -> "two" | #just _ -> "other" | #no -> "x"`, `"two"`},
	// {`| "hey" -> ""
	// 	| "hello " ++ name -> name
	// 	| _ -> "<empty>" <| "hello Oseg"`, Text("Oseg")},
	{`#empty`, `#empty`},
	{`#a |> | #a () -> 1`, `1`},
	{`#fun (x -> x * 2)`, `#fun x -> x * 2`},

	// Destructuring.
	{`{ a = 1, b = 2 } |> | { a = c, b = d } -> c + d`, `3`},
	{`{ a = 1 } |> | { a = 2 } -> 0 | { a = c } -> c`, `1`},
	{`3 |> a -> b -> a`, `b -> a`},

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

	{`| n >+ ns -> ns <| [1, 2, 3]`, `[ 2, 3 ]`},
	{`| ns +< n -> ns <| [1, 2, 3]`, `[ 1, 2 ]`},
	{`| ns ++ [2, 3] -> ns <| [1, 2, 3]`, `[ 1 ]`},
	{`| [1, 2] ++ ns -> ns <| [1, 2, 3]`, `[ 3 ]`},
	{`| ns ++ [2, last] -> ns +< last <| [1, 2, 3]`, `[ 1, 3 ]`},
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
			rep := env.Scrap(val)
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
	{`[ 1, ] |> | [] -> "four"`, `[] -> "four" had no alternative for [ 1 ]`},
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
    | { ..x, a = 1, b = b,       } -> b
    | { ..x,               c = c } -> c + x.a`, `6`},
	{`~ff`, "~FF"},
	{`~~abcd`, "~~abcd"},
	{`f 1 <| 2 ; f = a -> b -> a + b`, "3"},
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
	{`list/fold 0 (a -> b -> a + b)`, `list/fold 0 a -> b -> a + b`},
	{`list/fold 0 (a -> b -> a + b) [1, 2]`, `3`},
	{`list/fold 0 (a -> b -> a + text/length b) ["hey", "beautiful"]`, `12`},

	{`[ 4 + 2, 5 - 1, ]`, "[ 6, 4 ]"},
	{`[ 1, 4 ] |> | [1,3] -> "three" |[_,4] -> "four"`, `"four"`},

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

func TestFailures(t *testing.T) {
	for _, ex := range failures {
		evalFailure(t, ex.source, ex.error)
	}
}

func eval(e *Environment, source string) (Value, error) {
	scrap, err := e.Read([]byte(source))
	if err != nil {
		return nil, err
	}
	return e.Eval(scrap)
}

// Evaluates an expression and compares the string representation of the
// result with a target string; optionally with some additional variables
// in scope.
func evalString(t *testing.T, source, expected string) {
	val, err := eval(NewEnvironment(), source)

	if err != nil {
		t.Error(err)
	} else {
		if val.String() != expected {
			t.Errorf("Expected: %#v, got: %#v", expected, val.String())
		}
	}
}

// Evaluates to a comparable value
func evalFailure(t *testing.T, source string, expected string) {
	val, err := eval(NewEnvironment(), source)

	if err == nil {
		t.Errorf("%s - should fail but got %s", source, val)
	} else {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("Expected '%s' in error:\n%s", expected, err.Error())
		}
	}
}

func TestEvalImport(t *testing.T) {
	env := NewEnvironment()
	env.UseFetcher(MapFetcher{
		"a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447": `3 + $sha256~~a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445`,
		"a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445": `2`,
	})

	val, err := eval(env, `$sha256~~a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447 - 1`)
	if err != nil {
		t.Error(err)
	} else {
		if val.String() != "4" {
			t.Errorf("Expected: %#v, got: %#v", "4", val.String())
		}
	}
}

func TestImportType(t *testing.T) {
	env := NewEnvironment()
	env.UseFetcher(MapFetcher{
		"a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445": `t ; t : type = [#a int, #b]`,
	})

	scrap, err := env.Read([]byte(`f (#a 1) ; f : $sha256~~a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445 -> int = | #a n -> n | #b -> 0`))
	if err != nil {
		t.Fatal(err)
	}
	val, err := env.Eval(scrap)
	if err != nil {
		t.Fatal(err)
	}
	if val.String() != "1" {
		t.Errorf("Expected 1, got %s", val)
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

type MapFetcher map[string]string

func (mf MapFetcher) FetchSha256(key string) ([]byte, error) {
	source, ok := mf[key]
	if !ok {
		return nil, fmt.Errorf("can't import '%s'", key)
	}
	return []byte(source), nil
}
