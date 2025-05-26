package eval

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/parser"
	"github.com/Victorystick/scrapscript/token"
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
	// eval(t, `f #true ; f = | #true -> 1 | #false -> 2`, 1)
	// eval(t, `bool::true |> | #true -> 1 | #false -> 2 ; bool : #true #false`, 1)
	{`f 2 ; f = | a -> a + a`, `4`},
	{`2 |> | a -> a + a`, `4`},
	{`hand::l 5 |> | #l n -> n * 2 | #r n -> n * 3 ; hand : #l int #r int`, `10`},
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
	{`m::just 2 |> | #just 2 -> "two" | #just _ -> "other" | #no -> "x" ; m : #just int #no`, `"two"`},
	// {`| "hey" -> ""
	// 	| "hello " ++ name -> name
	// 	| _ -> "<empty>" <| "hello Oseg"`, Text("Oseg")},
	{`a::x 1 ; a : #x f ; f = x -> 2`, `#x 2`},

	// Destructuring.
	{`{ a = 1, b = 2 } |> | { a = c, b = d } -> c + d`, `3`},
	{`{ a = 1 } |> | { a = 2 } -> c | { a = c } -> c`, `1`},
	{`3 |> a -> b -> a`, `b -> a`},
}

var failures = []struct {
	source string
	error  string
}{
	{`f 1 ; f = a -> b`, "unknown variable b"},
	{`f 1 ; b = 2 ; f = a -> b`, "unknown variable b"},
	{`{} |> | { b = a } -> a`, "cannot bind to missing key b"},
	{`[ 1, ] |> | [] -> "four"`, `[] -> "four" had no alternative for [ 1 ]`},
	{`[] ++ ""`, `non-list value ""`},
	{`"" ++ []`, `non-text value []`},
	{`1 -> x`, `function parameter must be an identifier`},
	{`hand::left 5 ; hand : #l int #r int`, `left isn't one of the valid tags: l, r`},
	{`{ a = 2 } |> | { a = a, b = a } -> ()`, `cannot bind to missing key b`},
	{`{ a = 2, b = 1 } |> | { a = a, b = a } -> ()`, `cannot bind a twice`},
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
	{`["prefix"] ++ ["in" ++ "fix"] +< "postfix"`, `[ "prefix", "infix", "postfix" ]`},
	// Records
	{`rec.a ; rec = { a = 1, b = "x" }`, `1`},
	{`{ ..g, a = 2, c = ~FF }
	; g = { a = 1, b = "x", c = ~00 }`, `{ a = 2, b = "x", c = ~FF }`},
	{`{ ..{ a = 2, c = 1 }, a = 1, b = "x"}`, `{ a = 1, b = "x", c = 1 }`},
	{`{ a = 2, b = 3, c = 4 } |>
    | { ..x, a = 1, b = 2, c = 3 } -> ()
    | {      a = 1, b = b,       } -> ()
    | {      a = 1, b = 2,       } -> ()
    | { ..x,               c = c } -> { c = c, x = x }`, `{ c = 4, x = { a = 2, b = 3 } }`},
	{`a ; a : #x int #y float #z`, "#x int #y float #z"},
	{`~ff`, "~FF"},
	{`~~abcd`, "~~abcd"},
	{`f 1 <| 2 ; f = a -> b -> a + b`, "3"},
	{`f 1 2 ; f = a -> b -> a + b`, "3"},
	{`1 + 2 * floor 3.4`, "7"},
	{`2 * ceil 2.2 + 1`, "7"},
	{`-3 - 5`, `-8`},

	{`(hand::l 5 |>
			| #l n -> n * 2
			| #r n -> n * 3)
	; hand : #l int #r int`, `10`},

	{`list/map text/length ["hey", "beautiful"]`, `[ 3, 9 ]`},
	{`list/map text/length`, `list/map text/length`},

	{`list/fold 0 (a -> b -> a + b) []`, `0`},
	{`list/fold 0 (a -> b -> a + b)`, `list/fold 0 a -> b -> a + b`},
	{`list/fold 0 (a -> b -> a + b) [1, 2]`, `3`},

	{`[ 4 + 2, 5 - 1, ]`, "[ 6, 4 ]"},
	{`[ 1, 4 ] |> | [1,3] -> "three" |[_,4] -> "four"`, `"four"`},
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

// Evaluates an expression and compares the string representation of the
// result with a target string; optionally with some additional variables
// in scope.
func evalString(t *testing.T, source, expected string) {
	src := token.NewSource([]byte(source))

	se, err := parser.Parse(&src)
	if err != nil {
		t.Error(err)
	} else {
		val, err := Eval(se, builtIns)
		if err != nil {
			t.Error(err)
		} else {
			if val.String() != expected {
				t.Errorf("Expected: %#v, got: %#v", expected, val.String())
			}
		}
	}
}

// Evaluates to a comparable value
func evalFailure(t *testing.T, source string, expected string) {
	src := token.NewSource([]byte(source))

	se, err := parser.Parse(&src)
	if err != nil {
		t.Errorf("%s - %s", source, err)
	} else {
		val, err := Eval(se, builtIns)
		if err == nil {
			t.Errorf("%s - should fail but got %s", source, val)
		} else {
			if !strings.Contains(err.Error(), expected) {
				t.Errorf("Expected '%s' in error:\n%s", expected, err.Error())
			}
		}
	}
}

func TestEvalImport(t *testing.T) {
	env := NewEnvironment(MapFetcher{
		"a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447": `3 + $sha256~~a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445`,
		"a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445": `2`,
	})

	val, err := env.Eval([]byte(`$sha256~~a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447 - 1`))
	if err != nil {
		t.Error(err)
	} else {
		if val.String() != "4" {
			t.Errorf("Expected: %#v, got: %#v", "4", val.String())
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
