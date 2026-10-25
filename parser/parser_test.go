package parser

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/printer"
	"github.com/Victorystick/scrapscript/scanner"
	"github.com/Victorystick/scrapscript/token"
)

func TestParseExpr(t *testing.T) {
	binops := []string{
		"1 + 2",
		"f 1 + 2",
		"1 + f 2",
	}
	for _, src := range binops {
		se, err := ParseExpr(src)
		if err != nil {
			writeParseError(t, src, err)
		}
		x := se.Expr
		// sanity check
		if k := se.Kind(x); k != ast.Binary {
			t.Errorf("ParseExpr(%q): got %s, want Binary", src, k)
			var buf bytes.Buffer
			printer.Fprint(&buf, &se, x)
			t.Error(buf.String())
		}
	}

	src := "a"
	_, err := ParseExpr(src)
	if err != nil {
		writeParseError(t, src, err)
	}

	src = "a * b + c * d"
	se, err := ParseExpr(src)
	if err != nil {
		writeParseError(t, src, err)
	}
	x := se.Expr
	// sanity check
	if k := se.Kind(x); k != ast.Binary {
		t.Errorf("ParseExpr(%q): got %s, want Binary", src, k)
	} else if op, _, _ := se.Binary(x); op != token.ADD {
		t.Errorf("ParseExpr(%q): got %s, want ADD", src, op)
		writeExpr(t, &se, x)
	}

	fns := []string{
		"f 1 2",
		"f #a 1",
		"f (1 + 2)",
	}
	for _, src := range fns {
		se, err = ParseExpr(src)
		if err != nil {
			writeParseError(t, src, err)
		}
		x := se.Expr
		// sanity check
		if k := se.Kind(x); k != ast.Call {
			t.Errorf("ParseExpr(%q): got %s, want Call", src, k)
			var buf bytes.Buffer
			printer.Fprint(&buf, &se, x)
			t.Error(buf.String())
		}
	}

	src = `f 1 2 ; f = a -> b -> a + b ; ignored = "hi"`
	se, err = ParseExpr(src)
	if err != nil {
		writeParseError(t, src, err)
	}
	x = se.Expr
	// sanity check
	if k := se.Kind(x); k != ast.Where {
		t.Errorf("ParseExpr(%q): got %s, want Where", src, k)
		writeExpr(t, &se, x)
	}
}

func TestParseRecord(t *testing.T) {
	valid := []string{
		`{}`,
		`{ a = 1, b = "x"}`,
		`{ ..other, a = 1, b = "x"}`,
		`{ ..{ a = 2, c = 1 }, a = 1, b = "x"}`,
	}

	for _, src := range valid {
		_, err := ParseExpr(src)
		if err != nil {
			writeParseError(t, src, err)
		}
	}
}

func TestParseEnum(t *testing.T) {
	valid := []string{
		`#true #false`,
		`[#1, #2, #4, #8, #16, #32]`,
		`#tag (#a #b)`,
		`#left ; my-type : type = [#left, #right]`,
		`#horse "Lucy"
; animal : type =
  [ #horse text
  , #zebra int
  ]
`,
	}

	for _, src := range valid {
		_, err := ParseExpr(src)
		if err != nil {
			writeParseError(t, src, err)
		}
	}
}

func TestParseTypes(t *testing.T) {
	examples := []struct {
		source string
		sort   bool
	}{
		{`x ; x : int = 1`, false},
		{`ignore ; ignore : int -> list int -> () = _ -> _ -> ()`, false},
		{`empty ; empty : [#a, #b int, #c (list int), ..r] -> { a : int, ..r } = _ -> {}`, false},
		{`id ; id : a => a -> a = x -> x`, false},
		{`ignore ; ignore : $source:0 -> int = _ -> 1`, false},
		{`t ; t : type = [#a $source:0, #b $"./b.scrap"]`, true},
		{`t ; t : type = int`, true},
		{`t ; t : type = [#a { b : text }, #c]`, true},
		{`t ; t : type = {}`, true},
		{`t ; t : type = []`, true},
		{`t ; t : type = { ..r }`, true},
		{`t ; t : type -> type = s => { state : s }`, true},
		{`t ; t = a => b => [#left a, #right b]`, true},
		{`ignore ; ignore : avl.tree text int -> int = _ -> 1`, false},
		{`t ; t : type = { paths : avl.tree text (p.parser int), q : lib.sub.t }`, true},
		{`t ; t : type = [#a ($"./avl.scrap".tree int int), #b $source:0.t]`, true},
	}

	for _, ex := range examples {
		se, err := ParseExpr(ex.source)
		if err != nil {
			writeParseError(t, ex.source, err)
			continue
		}
		if sort := se.Sort(se.Expr); sort != ex.sort {
			t.Errorf("%s: expected sort binding to be %v", ex.source, ex.sort)
		}
	}
}

func TestMatchFunc(t *testing.T) {
	valid := []string{
		`default -> | #none -> default | #just a -> a`,
		`| "/" -> "Welcome" | _ -> "Where are you?" <| "/"`,
	}

	for _, src := range valid {
		_, err := ParseExpr(src)
		if err != nil {
			writeParseError(t, src, err)
		}
	}

	// An arm takes one pattern per argument; a lambda in its body needs
	// parentheses.
	arities := []struct {
		src   string
		arity int
	}{
		{`| a -> a`, 1},
		{`| 1 -> b -> 1 | a -> b -> a + b`, 2},
		{`| #a -> _ -> [] ++ c -> c | _ -> _ -> _ -> ""`, 3},
		{`| 0 -> (x -> x)`, 1},
	}
	for _, ex := range arities {
		se, err := ParseExpr(ex.src)
		if err != nil {
			writeParseError(t, ex.src, err)
			continue
		}
		if got := se.Arity(se.Expr); got != ex.arity {
			t.Errorf("%s: expected arity %d, got %d", ex.src, ex.arity, got)
		}
	}
}

func TestParses(t *testing.T) {
	valid := []string{
		`f "b" ; f = | "a" -> 1 | "b" -> 2 | "c" -> 3 | x -> 0`,
		`#true ; bool : type = [#true, #false]`,
		`| "hey" -> "" | "hello " ++ name -> name | _ -> ""`,
		`a |> | a -> a ; f = 1`,
		`#l ; hand : type = [#l int, #r int]`,
		`(#left 5 |>
| #l n -> n * 2
| #r n -> n * 3)
  ; hand : type = [#l int, #r int]`,
		`t ; t : type = [#a a, #b int, #c byte] ; a : type = [#x, #y, #z]`,
		`[]`,
		`[ "yo", 2 ]`,
		`foo.a`,
		`inc ; inc : int -> int = a -> a + 1`,
		`$"./f.scrap" $"./x.scrap" $source:0`,
		`#true #false`,
	}

	for _, src := range valid {
		_, err := ParseExpr(src)
		if err != nil {
			writeParseError(t, src, err)
		}
	}
}

func TestImports(t *testing.T) {
	valid := []string{
		`$source:a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447`,
	}

	for _, src := range valid {
		se, err := ParseExpr(src)
		if err != nil {
			writeParseError(t, src, err)
		}

		if k := se.Kind(se.Expr); k != ast.Import {
			t.Errorf("Expected an Import, got %s", k)
		}
	}
}

func TestParseError(t *testing.T) {
	examples := []struct{ source, message string }{
		{`{ a = b ..c }`, `Expected RBRACE got SPREAD`},
		{`{ a = 1, ..other }`, `A spread must be first in a record.`},
		{`#"a"`, `Expected IDENT got TEXT`},
		{`a::b`, `Expected EOF got DEFINE`},
		{`t ; t : #a #b`, `Unexpected OPTION`},
		{`t ; t : type`, `Expected ASSIGN got EOF`},
		{`t ; t : type = [#a, #b,]`, `A type cannot end in a trailing comma.`},
		{`t ; t : type = [#a, ..r, #b]`, `Expected RBRACK got COMMA`},
		{`t ; t : type = { a = int }`, `Expected DEFINE got ASSIGN`},
		{`[ 1, ]`, `A list cannot end in a trailing comma.`},
		{`{ a = 1, }`, `A record cannot end in a trailing comma.`},
		{`{ ..r }`, `Expected COMMA got RBRACE`},
		{`a < b < c`, `Comparisons cannot be chained.`},
		{`f x -> x`, `function parameter must be an identifier`},
		{`#a 1 2`, `Expected EOF got INT`},
		{`| 1 -> 2 -> 3 | a -> a`, `this arm has 1 pattern, but the first arm has 2`},
		{`| a -> a | 1 -> 2 -> 3`, `this arm has 2 patterns, but the first arm has 1`},
	}

	for _, example := range examples {
		_, err := ParseExpr(example.source)
		if err == nil || !strings.Contains(err.Error(), example.message) {
			t.Errorf("Expected error with '%s', got: %s", example.message, err)
		}
	}
}

func writeParseError(t *testing.T, src string, err error) {
	if e, ok := err.(scanner.Errors); ok {
		for _, err := range e {
			t.Errorf("ParseExpr: %s", err.Error())
		}
	} else {
		t.Errorf("ParseExpr: %s", err)
	}
}

func writeExpr(t *testing.T, se *ast.SourceExpr, expr ast.Node) {
	var buf bytes.Buffer
	printer.Fprint(&buf, se, expr)
	t.Error(buf.String())
}

// FuzzParse checks that the parser never fails other than with a parse
// error: Parse re-panics anything else, such as consuming past EOF.
func FuzzParse(f *testing.F) {
	f.Add(string(benchSource[:2000]))
	for _, b := range benchBindings {
		f.Add("x ; " + b)
	}
	f.Add("{ ..r, a = [1, 2], b = #t (x -> x) } ; t : { a : int, ..r } = ()")
	f.Fuzz(func(t *testing.T, src string) {
		ParseExpr(src)
	})
}
