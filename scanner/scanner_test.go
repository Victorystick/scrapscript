package scanner

import (
	"bytes"
	"slices"
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
	{token.IMPORT, "$", operator},    // Import
	{token.IDENT, "sha256", literal}, // Hash algo
	{token.IDENT, "bytes/to-utf8-text", literal},
	{token.INT, "13", literal},
	{token.INT, "-13", literal},
	{token.FLOAT, "3.7", literal},
	{token.FLOAT, "-1.6", literal},
	{token.TEXT, `"world"`, literal},
	{token.BYTE, "~ca", literal},
	{token.BYTES, "~~aGVsbG8gd29ybGQ=", literal},
	{token.BYTES, "~~", literal},

	// Operators and delimiters
	{token.ASSIGN, "=", operator},
	{token.WHERE, ";", operator},

	{token.DEFINE, ":", operator},
	{token.OPTION, "#", operator},
	{token.ACCESS, ".", operator},
	{token.SPREAD, "..", operator},

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

// scan scans src and fails the test on any scanning error.
func scan(t *testing.T, src []byte) Tokens {
	t.Helper()
	toks, errs := Scan(src)
	for _, e := range errs {
		t.Errorf("%s at %d", e.Code, e.Start)
	}
	return toks
}

// Verify that Scan() provides the correct results.
func TestScan(t *testing.T) {
	toks := scan(t, source)
	if len(toks.Kinds) != len(elements) {
		t.Fatalf("got %d tokens, expected %d", len(toks.Kinds), len(elements))
	}

	for i, e := range elements {
		tok, span := toks.Kinds[i], toks.Span(source, i)

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

	toks := scan(t, source)
	if len(toks.Kinds) != 18 {
		t.Fatalf("got %d tokens, expected 18", len(toks.Kinds))
	}

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
		tok, span := toks.Kinds[i], toks.Span(source, i)

		if tok != e.tok {
			t.Errorf("i: %d - bad token for %q: got %s, expected %s", i, e.lit, tok, e.tok)
		}

		lit := span.Get(source)
		if lit != e.lit {
			t.Errorf("i: %d - bad literal for %q: got %q, expected %q", i, e.lit, lit, e.lit)
		}
	}
}

func TestScanErrors(t *testing.T) {
	examples := []struct {
		src   string
		toks  []token.Token // excluding EOF
		codes []ErrCode
	}{
		{`é`, []token.Token{token.BAD}, []ErrCode{ErrNonASCII}},
		{"\xff", []token.Token{token.BAD}, []ErrCode{ErrInvalidUTF8}},
		{`@`, []token.Token{token.BAD}, []ErrCode{ErrIllegalChar}},
		{`a @ b`, []token.Token{token.IDENT, token.BAD, token.IDENT}, []ErrCode{ErrIllegalChar}},
		{`"é"`, []token.Token{token.TEXT}, nil},
		{"\"a\x00b\"", []token.Token{token.TEXT}, []ErrCode{ErrIllegalChar}},
		{"\"a\xffb\"", []token.Token{token.TEXT}, []ErrCode{ErrInvalidUTF8}},
		{"\"abc\nd", []token.Token{token.TEXT, token.IDENT}, []ErrCode{ErrUnterminatedText}},
		{`~zz`, []token.Token{token.BAD, token.IDENT}, []ErrCode{ErrExpectedHex}},
		{`~~abc`, []token.Token{token.BAD}, []ErrCode{ErrBase64Padding}},
	}

	for _, ex := range examples {
		src := []byte(ex.src)
		toks, errs := Scan(src)
		want := append(ex.toks, token.EOF)
		if !slices.Equal(toks.Kinds, want) {
			t.Errorf("%q: got tokens %v, expected %v", ex.src, toks.Kinds, want)
		}
		var codes []ErrCode
		for _, e := range errs {
			codes = append(codes, e.Code)
		}
		if !slices.Equal(codes, ex.codes) {
			t.Errorf("%q: got errors %v, expected %v", ex.src, codes, ex.codes)
		}
		checkTokens(t, src, toks)
	}
}

// checkTokens verifies that toks covers src: tokens are in order, do not
// overlap, are separated only by whitespace and end with EOF at the end of src.
func checkTokens(t *testing.T, src []byte, toks Tokens) {
	t.Helper()
	n := len(toks.Kinds)
	if n == 0 || toks.Kinds[n-1] != token.EOF || int(toks.Starts[n-1]) != len(src) {
		t.Fatalf("%q: token list does not end with EOF at %d", src, len(src))
	}
	prev := 0
	for i := range n {
		start, end := int(toks.Starts[i]), toks.End(src, i)
		if start < prev {
			t.Fatalf("%q: token %d starts at %d, before the previous token ends at %d", src, i, start, prev)
		}
		if gap := src[prev:start]; len(bytes.Trim(gap, " \t\n\r")) > 0 {
			t.Fatalf("%q: non-whitespace %q between tokens", src, gap)
		}
		if end <= start && i < n-1 {
			t.Fatalf("%q: empty %s token at %d", src, toks.Kinds[i], start)
		}
		prev = end
	}
}

func FuzzScan(f *testing.F) {
	f.Add(source)
	f.Add([]byte(benchSource))
	f.Add([]byte("a @ é \"x\n~z ~~ab"))
	f.Fuzz(func(t *testing.T, src []byte) {
		toks, _ := Scan(src)
		checkTokens(t, src, toks)
	})
}

const benchSource = `f "b" ; f = | "a" -> 1 | "b" -> 2 | _ -> 0
; rec = { a = 1, b = -2.5, c = "hello world", d = ~~aGVsbG8gd29ybGQ= }
; g = x -> x + 1 |> h >> k
; xs = [1, 2, 3] ++ [4] +< 5
; opt : #some int #none = #some 3
; r = { ..rec, a = rec.a * 2 }
; imp = $sha256~~aGVsbG8gd29ybGQ=
`

func BenchmarkScan(b *testing.B) {
	src := bytes.Repeat([]byte(benchSource), 1<<14) // ~4 MB
	b.SetBytes(int64(len(src)))
	var n int
	for b.Loop() {
		toks, _ := Scan(src)
		n = len(toks.Kinds)
	}
	b.ReportMetric(float64(len(src))/float64(n), "bytes/token")
}
