package eval

import (
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/parser"
	"github.com/Victorystick/scrapscript/token"
)

var expressions = []struct {
	source string
	result Value
}{
	{`1`, Int(1)},
	{`1.0 + to-float 1`, Float(2)},
	{`"hello" ++ " " ++ "world"`, Text("hello world")},
	// {`f 1 2 ; f = a -> b -> a + b`, Int(3)},
	{`f "b" ; f = | "a" -> 1 | "b" -> 2 | "c" -> 3 | x -> 0`, Int(2)},
	{`(f >> (x -> x) >> g) 7
	  ; f =
		  | 7 -> "cat"
	    | 4 -> "dog"
	    | _ -> "shark"
	  ; g =
		  | "cat" -> "kitten"
	    | "dog" -> "puppy"
	    |   a   -> "baby " ++ a`, Text("kitten")},
	{`(x -> x) (y -> y)`, ScriptFunc{source: "y -> y"}},
	{`m::just 2 |> | #just 2 -> "two" | #just _ -> "other" | #no -> "x" ; m : #just int #no`, Text("two")},
	// {`| "hey" -> ""
	// 	| "hello " ++ name -> name
	// 	| _ -> "<empty>" <| "hello Oseg"`, Text("Oseg")},
	{`a::x 1 ; a : #x f ; f = x -> 2`, Variant{"x", Int(2)}},

	// Destructuring.
	{`{ a = 1, b = 2 } |> | { a = c, b = d } -> c + d`, Int(3)},
	{`{ a = 1 } |> | { a = 2 } -> c | { a = c } -> c`, Int(1)},
	{`3 |> a -> b -> a`, ScriptFunc{source: "b -> a"}},
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
}

func TestLiterals(t *testing.T) {
	eval(t, `1`, Int(1))
	eval(t, `1.0`, Float(1))
	eval(t, `"hello"`, Text("hello"))
	eval(t, `~41`, Byte(65))
	eval(t, `~~aGVsbG8gd29ybGQ=`, Bytes("hello world"))
}

func TestOperators(t *testing.T) {
	eval(t, `1 + 2`, Int(3))
	eval(t, `1 + 3 * 3`, Int(10))
	eval(t, `1.0 + 2.0`, Float(3.0))
	eval(t, `3 - 2`, Int(1))
	eval(t, `3.0 - 2.0`, Float(1.0))
}

func TestBuiltins(t *testing.T) {
	eval(t, `to-float 1 + 0.5`, Float(1.5))

	// bytes/to-utf8-text
	eval(t, `bytes-to-utf8-text ~~aGVsbG8gd29ybGQ=`, Text("hello world"))
	eval(t, `bytes-to-utf8-text <| ~~aGVsbG8gd29ybGQ= +< ~21`, Text("hello world!"))
	eval(t, `~~aGVsbG8gd29ybGQ= +< ~21 |> bytes-to-utf8-text`, Text("hello world!"))
}

func TestWhere(t *testing.T) {
	eval(t, `200 + (x ; x = 150)`, Int(350))
	eval(t, `a + b + c ; a = 1 ; b = 2 ; c = 3`, Int(6))
	// eval(t, `(f >> (x -> x) >> g) 7`, 7)
}

func TestFunc(t *testing.T) {
	eval(t, `2 |> | _ -> 3`, Int(3))
	// eval(t, `f #true ; f = | #true -> 1 | #false -> 2`, 1)
	// eval(t, `bool::true |> | #true -> 1 | #false -> 2 ; bool : #true #false`, 1)
	eval(t, `f 2 ; f = | a -> a + a`, Int(4))
	eval(t, `2 |> | a -> a + a`, Int(4))
	eval(t, `hand::l 5 |> | #l n -> n * 2 | #r n -> n * 3 ; hand : #l int #r int`, Int(10))

	eval(t, `f "b"
; f =
  | "a" -> 1
  | "b" -> 2
  | "c" -> 3
  |  x  -> 0`, Int(2))
}

func TestEval(t *testing.T) {
	for _, ex := range expressions {
		eval(t, ex.source, ex.result)
	}
}

var exp2str = []struct{ source, result string }{
	{`()`, `()`},
	// Should be bytes/to-utf8-text.
	{`bytes-to-utf8-text <| ~~aGVsbG8gd29ybGQ= +< ~21`, `"hello world!"`},
	{`1 >+ [2, 3] +< 4`, `[ 1, 2, 3, 4 ]`},
	{`["prefix"] ++ ["in" ++ "fix"] +< "postfix"`, `[ "prefix", "infix", "postfix" ]`},
	// Records
	{`rec.a ; rec = { a = 1, b = "x" }`, `1`},
	{`{ a = 2, c = ~FF, ..g }
	; g = { a = 1, b = "x", c = ~00 }`, `{ a = 2, b = "x", c = ~FF }`},
	{`{ a = 2, b = 3, c = 4 } |>
    | { a = 1, b = 2, c = 3, ..x } -> ()
    | { a = 1, b = b,        ..x } -> ()
    | { a = 1, b = 2,            } -> ()
    | {               c = c, ..x } -> { c = c, x = x }`, `{ c = 4, x = { a = 2, b = 3 } }`},
	{`a ; a : #x int #y float #z`, "#x int #y float #z"},
	{`~ff`, "~FF"},
	{`~~abcd`, "~~abcd"},
	{`f 1 <| 2 ; f = a -> b -> a + b`, "3"},
	{`f 1 2 ; f = a -> b -> a + b`, "3"},
	{`1 + 2 * floor 3.4`, "7"},
	{`2 * ceil 2.2 + 1`, "7"},
	{`-3 - 5`, `-8`},
	{`[ 4 + 2, 5 - 1, ]`, "[ 6, 4 ]"},
	{`[ 1, 4 ] |> | [1,3] -> "three" |[_,4] -> "four"`, `"four"`},
}

func TestEvalString(t *testing.T) {
	for _, ex := range exp2str {
		evalString(t, ex.source, ex.result)
	}

	evalString(t, `external-value`, `"Injected"`, Binding{"external-value", Text("Injected")})
}

func TestFailures(t *testing.T) {
	for _, ex := range failures {
		evalFailure(t, ex.source, ex.error)
	}
}

// Evaluates to a comparable value
func eval(t *testing.T, source string, expected Value) {
	src := token.NewSource([]byte(source))

	se, err := parser.Parse(&src)
	if err != nil {
		t.Error(err)
	} else {
		val, err := Eval(se)
		if err != nil {
			t.Error(err)
		} else {
			if !val.eq(expected) {
				t.Errorf("Expected: %#v, got: %#v", expected, val)
			}
		}
	}
}

// Evaluates an expression and compares the string representation of the
// result with a target string; optionally with some additional variables
// in scope.
func evalString(t *testing.T, source, expected string, vars ...Vars) {
	src := token.NewSource([]byte(source))

	se, err := parser.Parse(&src)
	if err != nil {
		t.Error(err)
	} else {
		val, err := Eval(se, vars...)
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
		val, err := Eval(se)
		if err == nil {
			t.Errorf("%s - should fail but got %s", source, val)
		} else {
			if !strings.Contains(err.Error(), expected) {
				t.Errorf("Expected '%s' in error:\n%s", expected, err.Error())
			}
		}
	}
}
