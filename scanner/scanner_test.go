package scanner

import (
	"testing"

	"github.com/Victorystick/scrapscript/token"
)

const /* class */ (
	special = iota
	literal
	operator
)

func tokenclass(tok token.Token) int {
	switch {
	case tok.IsLiteral():
		return literal
	case tok.IsOperator():
		return operator
	}
	return special
}

type elt struct {
	tok   token.Token
	lit   string
	class int
}

var elements = []elt{
	// Special tokens
	{token.IDENT, "hello", literal},
	{token.IDENT, "f", literal},
	{token.IDENT, "$sha256", literal}, // Import
	// {token.IDENT, "bytes/to-utf8-text", literal},
	{token.INT, "13", literal},
	{token.FLOAT, "3.7", literal},
	{token.TEXT, `"world"`, literal},
	{token.BYTE, "~ca", literal},
	{token.BYTES, "~~aGVsbG8gd29ybGQ=", literal},

	// Operators and delimiters
	{token.ASSIGN, "=", operator},
	{token.WHERE, ";", operator},

	{token.DEFINE, ":", operator},
	{token.PICK, "::", operator},
	{token.OPTION, "#", operator},

	{token.ADD, "+", operator},
	{token.SUB, "-", operator},
	{token.MUL, "*", operator},

	{token.CONCAT, "++", operator},
	{token.APPEND, "+<", operator},
	{token.PREPEND, ">+", operator},

	{token.ARROW, "->", operator},
	{token.PIPE, "|", operator},
	{token.LPIPE, "<|", operator},
	{token.RPIPE, "|>", operator},

	{token.LT, "<", operator},
	{token.GT, ">", operator},

	{token.HOLE, "()", operator},
	{token.LPAREN, "(", operator},
	{token.RPAREN, ")", operator},

	{token.LBRACE, "{", operator},
	{token.RBRACE, "}", operator},

	{token.LBRACK, "[", operator},
	{token.RBRACK, "]", operator},

	{token.EOF, "", special},
}

const whitespace = "  \t  \n\n\n" // to separate tokens

var source = func() []byte {
	var src []byte
	for _, t := range elements {
		src = append(src, t.lit...)
		src = append(src, whitespace...)
	}
	return src
}()

func TestingErrorHandler(t *testing.T) ErrorHandler {
	return func(e Error) {
		t.Error(e.Error())
	}
}

// Verify that calling Scan() provides the correct results.
func TestScan(t *testing.T) {
	var s Scanner
	s.Init(source, TestingErrorHandler(t))

	for _, e := range elements {
		tok, span := s.Scan()

		if tok != e.tok {
			t.Errorf("bad token for %q: got %s, expected %s", e.lit, tok, e.tok)
		}

		// check token class
		if tokenclass(tok) != e.class {
			t.Errorf("bad class for %q: got %d, expected %d", e.lit, tokenclass(tok), e.class)
		}

		lit := span.Get(source)

		if lit != e.lit {
			t.Errorf("bad literal for %q: got %q, expected %q", e.tok, lit, e.lit)
		}
	}
}

func TestScanExample(t *testing.T) {
	source := []byte(`f "b" ; f =
	| "a" -> 1 | "b" -> 2 | _ -> 0`)

	var s Scanner
	s.Init(source, TestingErrorHandler(t))

	var ex = [...]struct {
		tok token.Token
		lit string
	}{
		{tok: token.IDENT, lit: `f`},
		{tok: token.TEXT, lit: `"b"`},
		{tok: token.WHERE, lit: `;`},
		{tok: token.IDENT, lit: `f`},
		{tok: token.ASSIGN, lit: `=`},
		{tok: token.PIPE, lit: `|`},
		{tok: token.TEXT, lit: `"a"`},
		{tok: token.ARROW, lit: `->`},
		{tok: token.INT, lit: `1`},
		{tok: token.PIPE, lit: `|`},
		{tok: token.TEXT, lit: `"b"`},
		{tok: token.ARROW, lit: `->`},
		{tok: token.INT, lit: `2`},
		{tok: token.PIPE, lit: `|`},
		{tok: token.IDENT, lit: `_`},
		{tok: token.ARROW, lit: `->`},
		{tok: token.INT, lit: `0`},
		{tok: token.EOF},
	}

	for i, e := range ex {
		tok, span := s.Scan()

		if tok != e.tok {
			t.Errorf("i: %d - bad token for %q: got %s, expected %s", i, e.lit, tok, e.tok)
		}

		lit := span.Get(source)
		if lit != e.lit {
			t.Errorf("i: %d - bad literal for %q: got %q, expected %q", i, e.lit, lit, e.lit)
		}
	}
}
