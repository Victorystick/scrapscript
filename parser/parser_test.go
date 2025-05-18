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
		`{ a = 1, b = "x"}`,
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
	}

	for _, src := range valid {
		_, err := ParseExpr(src)
		if err != nil {
			writeParseError(t, src, err)
		}
	}
}

func writeParseError(t *testing.T, src string, err error) {
	if e, ok := err.(scanner.Errors); ok {
		lines := strings.Split(src, "\n")
		first := e[0]
		line := first.Pos.Line
		t.Errorf("ParseExpr: %s", first.Error())
		t.Errorf("%4d: %s", line, lines[line-1])
		t.Error("   ", strings.Repeat(" ", first.Pos.Column), "^")
	} else {
		t.Errorf("ParseExpr(%q): %v", src, err)
	}
}

func writeExpr(t *testing.T, src string, expr ast.Expr) {
	var buf bytes.Buffer
	printer.Fprint(&buf, []byte(src), expr)
	t.Error(buf.String())
}
