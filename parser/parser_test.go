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
		"f::a",
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
		if _, ok := x.(*ast.BinaryExpr); !ok {
			t.Errorf("ParseExpr(%q): got %T, want *ast.BinaryExpr", src, x)
			var buf bytes.Buffer
			printer.Fprint(&buf, []byte(src), x)
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
	if b, ok := x.(*ast.BinaryExpr); !ok || b.Op != token.ADD {
		t.Errorf("ParseExpr(%q): got %T, want *ast.BinaryExpr", src, x)
		writeExpr(t, src, x)
	}

	fns := []string{
		"f 1 2",
		"f::a 1",
		"f (1 + 2)",
	}
	for _, src := range fns {
		se, err = ParseExpr(src)
		if err != nil {
			writeParseError(t, src, err)
		}
		x := se.Expr
		// sanity check
		if _, ok := x.(*ast.CallExpr); !ok {
			t.Errorf("ParseExpr(%q): got %T, want *ast.CallExpr", src, x)
			var buf bytes.Buffer
			printer.Fprint(&buf, []byte(src), x)
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
	if _, ok := x.(*ast.WhereExpr); !ok {
		t.Errorf("ParseExpr(%q): got %T, want *ast.LetExpr", src, x)
		writeExpr(t, src, x)
	}
}

func TestParseRecord(t *testing.T) {
	valid := []string{
		`{}`,
		`{ a = 1, }`,
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
		`#1 #2 #4 #8 #16 #32`,
		`#tag (#a #b)`,
		`my-type::left ; my-type : #left #right`,
		`animal::horse "Lucy"
; animal :
  #horse text
  #zebra int
`,
		// 		`my-org::my-config
		// { name = "my-server-001"
		// , cpus = #4
		// , mem  = #16
		// }
		// ; my-org :
		//     #my-config
		//       { name : text
		//       , cpus : #1 #2 #4 #8
		//       , mem  : #1 #2 #4 #8 #16 #32
		//       }`,
	}

	for _, src := range valid {
		_, err := ParseExpr(src)
		if err != nil {
			writeParseError(t, src, err)
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
}

func TestParses(t *testing.T) {
	valid := []string{
		`f "b" ; f = | "a" -> 1 | "b" -> 2 | "c" -> 3 | x -> 0`,
		`bool::true ; bool : #true #false`,
		`| "hey" -> "" | "hello " ++ name -> name | _ -> ""`,
		`a |> | a -> a ; f = 1`,
		`hand::l ; hand : #l int #r int`,
		`(hand::left 5 |>
| #l n -> n * 2
| #r n -> n * 3)
  ; hand : #l int #r int`,
		`t ; t : #a a #b int #c byte ; a : #x #y #z`,
		`[]`,
		`[ "yo", 2, ]`,
		`[ "yo", 2 ]`,
		`foo.a`,
		`inc ; inc : int -> int = a -> a + 1`,
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
		`$sha256~~a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447`,
	}

	for _, src := range valid {
		se, err := ParseExpr(src)
		if err != nil {
			writeParseError(t, src, err)
		}

		if _, ok := se.Expr.(*ast.ImportExpr); !ok {
			t.Errorf("Expected an ImportExpr, got %T", se.Expr)
		}
	}
}

func TestParseError(t *testing.T) {
	examples := []struct{ source, message string }{
		{`{ a = b ..c }`, `Expected RBRACE got SPREAD`},
		{`{ a = 1, ..other }`, `A spread must be first in a record.`},
		{`a::1 ; a : #a`, `Expected IDENT got INT`},
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

func writeExpr(t *testing.T, src string, expr ast.Expr) {
	var buf bytes.Buffer
	printer.Fprint(&buf, []byte(src), expr)
	t.Error(buf.String())
}
