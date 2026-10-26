package scanner

import (
	"bytes"
	"math"
	"unicode/utf8"

	"github.com/Victorystick/scrapscript/token"
)

// Tokens is a scanned source stored as parallel arrays: token i has kind
// Kinds[i] and starts at byte offset Starts[i]. The last token is always EOF,
// at the end of the source.
//
// Ends are not stored. End re-scans the single token instead, so the scanner
// is the only definition of how far a token reaches.
type Tokens struct {
	Kinds  []token.Token
	Starts []uint32
}

// End returns the offset just past token i.
func (t *Tokens) End(src []byte, i int) int {
	s := scanner{src: src}
	_, end := s.token(int(t.Starts[i]))
	return end
}

// Span returns the span of token i.
func (t *Tokens) Span(src []byte, i int) token.Span {
	return token.Span{Start: int(t.Starts[i]), End: t.End(src, i)}
}

// Source averages a few bytes per token; reserving for that up front avoids
// most regrowth of the token arrays.
const bytesPerToken = 4

// Scan splits src into tokens. It does not stop at errors: a malformed token
// is scanned as BAD or recorded in errs, and scanning resumes after it.
func Scan(src []byte) (toks Tokens, errs []Error) {
	if len(src) > math.MaxUint32 {
		return Tokens{Kinds: []token.Token{token.EOF}, Starts: []uint32{0}}, []Error{{Code: ErrTooLarge}}
	}

	n := len(src)/bytesPerToken + 1
	kinds := make([]token.Token, 0, n)
	starts := make([]uint32, 0, n)

	// The arrays and the position are locals, so that the loop keeps them in
	// registers. Identifiers and one-byte operators, the most common tokens,
	// are scanned inline, as token would scan them.
	s := scanner{src: src}
	pos := 0
	for {
		for pos < len(src) && class[src[pos]]&cSpace != 0 {
			pos++
		}
		if pos == len(src) {
			kinds = append(kinds, token.EOF)
			starts = append(starts, uint32(pos))
			return Tokens{Kinds: kinds, Starts: starts}, s.errs
		}
		start := pos
		c := src[pos]
		tok := lead[c]
		switch tok {
		case token.IDENT:
			pos = identEnd(src, pos+1)
		case token.BAD:
			if c == '-' && at(src, pos+1) == '-' {
				pos = s.skipTrivia(pos)
				continue
			}
			tok, pos = s.token(pos)
		default:
			pos++
		}
		kinds = append(kinds, tok)
		starts = append(starts, uint32(start))
	}
}

type scanner struct {
	src  []byte
	errs []Error
}

func (s *scanner) fail(code ErrCode, start, end int) {
	s.errs = append(s.errs, Error{Code: code, Start: uint32(start), End: uint32(end)})
}

// at returns the byte at i, or 0 past the end of src.
func at(src []byte, i int) byte {
	if i < len(src) {
		return src[i]
	}
	return 0
}

// skipTrivia skips the whitespace and comments from i, and returns the
// offset after them.
func (s *scanner) skipTrivia(i int) int {
	src := s.src
	for i < len(src) {
		c := src[i]
		if class[c]&cSpace != 0 {
			i++
		} else if c == '-' && at(src, i+1) == '-' {
			i = s.comment(i)
		} else {
			break
		}
	}
	return i
}

// comment skips a line comment, up to the end of its line. `--` must be
// followed by a space, another `-` or the end of the line; anything else is
// almost always a typo, so it is an error, though still skipped as a comment.
func (s *scanner) comment(start int) int {
	switch at(s.src, start+2) {
	case ' ', '-', '\n', '\r', 0:
	default:
		s.fail(ErrBadComment, start, start+2)
	}
	end := bytes.IndexByte(s.src[start:], '\n')
	if end == -1 {
		return len(s.src)
	}
	return start + end
}

// token scans the token starting at start, and returns its kind and the
// offset just past it. It depends only on the source and the start offset,
// which is what lets Tokens.End re-scan a single token.
func (s *scanner) token(start int) (token.Token, int) {
	src := s.src
	if start >= len(src) {
		return token.EOF, start
	}

	c := src[start]
	switch tok := lead[c]; tok {
	case token.IDENT:
		return tok, identEnd(src, start+1)
	case token.BAD:
		// Needs a closer look.
	default:
		return tok, start + 1
	}

	next := at(src, start+1)
	switch c {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return number(src, start)
	case '.':
		if class[next]&cDigit != 0 {
			return number(src, start)
		}
		return pair(start, next, token.ACCESS, '.', token.SPREAD)
	case '$':
		switch {
		case class[next]&cLetter != 0:
			return s.hash(start)
		case next == '"':
			return token.PATH, s.text(start + 1)
		}
		s.fail(ErrBadReference, start, start+1)
		return token.BAD, start + 1
	case '(':
		return pair(start, next, token.LPAREN, ')', token.HOLE)
	case '~':
		if next == '~' {
			return s.bytes(start)
		}
		return s.byte(start)
	case '"':
		return token.TEXT, s.text(start)
	case '=':
		return pair(start, next, token.ASSIGN, '>', token.FORALL)
	case '+':
		if next == '<' {
			return token.APPEND, start + 2
		}
		return pair(start, next, token.ADD, '+', token.CONCAT)
	case '-':
		if class[next]&cDigit != 0 || next == '.' && class[at(src, start+2)]&cDigit != 0 {
			return number(src, start)
		}
		return pair(start, next, token.SUB, '>', token.ARROW)
	case '|':
		return pair(start, next, token.PIPE, '>', token.RPIPE)
	case '<':
		if next == '<' {
			return token.LCOMP, start + 2
		}
		return pair(start, next, token.LT, '|', token.LPIPE)
	case '>':
		if next == '>' {
			return token.RCOMP, start + 2
		}
		return pair(start, next, token.GT, '+', token.PREPEND)
	}

	return s.illegal(start)
}

// pair returns double and the offset past two bytes if next is and;
// otherwise it returns single and the offset past one.
func pair(start int, next byte, single token.Token, and byte, double token.Token) (token.Token, int) {
	if next == and {
		return double, start + 2
	}
	return single, start + 1
}

func (s *scanner) illegal(start int) (token.Token, int) {
	if s.src[start] < utf8.RuneSelf {
		s.fail(ErrIllegalChar, start, start+1)
		return token.BAD, start + 1
	}
	r, w := utf8.DecodeRune(s.src[start:])
	if r == utf8.RuneError && w == 1 {
		s.fail(ErrInvalidUTF8, start, start+w)
	} else {
		s.fail(ErrNonASCII, start, start+w)
	}
	return token.BAD, start + w
}

func identEnd(src []byte, i int) int {
	for i < len(src) && class[src[i]]&cIdent != 0 {
		i++
	}
	return i
}

func digits(src []byte, i int) int {
	for i < len(src) && class[src[i]]&cDigit != 0 {
		i++
	}
	return i
}

// number scans an INT or FLOAT, with an optional leading '-'.
func number(src []byte, start int) (token.Token, int) {
	i := start
	if src[i] == '-' {
		i++
	}
	i = digits(src, i)
	if at(src, i) != '.' {
		return token.INT, i
	}
	return token.FLOAT, digits(src, i+1)
}

// text scans the text literal whose opening quote is at start, and returns
// the offset past its closing quote.
func (s *scanner) text(start int) int {
	src := s.src
	i := start + 1
	for i < len(src) {
		c := src[i]
		if class[c]&cTextEnd == 0 {
			i++
			continue
		}
		if c != '\\' {
			break
		}
		n := escapeLen(src[i:])
		if n == 0 {
			n = 1
			if i+1 < len(src) && src[i+1] != '\n' {
				n = 2
			}
			s.fail(ErrBadEscape, i, i+n)
		}
		i += n
	}
	s.checkText(start+1, i)
	if i < len(src) && src[i] == '"' {
		return i + 1
	}
	s.fail(ErrUnterminatedText, start, i)
	return i
}

// checkText reports invalid UTF-8 and NUL bytes in text contents, the only
// place where non-ASCII is allowed.
func (s *scanner) checkText(start, end int) {
	b := s.src[start:end]
	if utf8.Valid(b) && bytes.IndexByte(b, 0) < 0 {
		return
	}
	for i := 0; i < len(b); {
		r, w := utf8.DecodeRune(b[i:])
		switch {
		case r == utf8.RuneError && w == 1:
			s.fail(ErrInvalidUTF8, start+i, start+i+w)
		case r == 0:
			s.fail(ErrIllegalChar, start+i, start+i+w)
		}
		i += w
	}
}

// byte scans a single byte literal: '~' and two hex digits.
func (s *scanner) byte(start int) (token.Token, int) {
	for i := start + 1; i < start+3; i++ {
		if class[at(s.src, i)]&cHex == 0 {
			s.fail(ErrExpectedHex, i, i+1)
			return token.BAD, i
		}
	}
	return token.BYTE, start + 3
}

// hash scans a hash: '$', a scheme name, ':' and lowercase hex digits. The
// scheme decides how many, so the digits run to the end of the word.
func (s *scanner) hash(start int) (token.Token, int) {
	i := identEnd(s.src, start+2)
	if at(s.src, i) != ':' {
		s.fail(ErrBadHash, start, i)
		return token.BAD, i
	}
	b := i + 1
	i = identEnd(s.src, b)
	if i == b {
		s.fail(ErrBadHash, start, i)
		return token.BAD, i
	}
	for j := b; j < i; j++ {
		if c := s.src[j]; class[c]&cHex == 0 || 'A' <= c && c <= 'F' {
			s.fail(ErrBadHash, j, j+1)
			return token.BAD, i
		}
	}
	return token.HASH, i
}

// bytes scans a bytes literal: '~~' and padded base64. A bare '~~' is empty.
func (s *scanner) bytes(start int) (token.Token, int) {
	b := start + 2
	i := b
	for i < len(s.src) && class[s.src[i]]&cBase64 != 0 {
		i++
	}
	for (i-b)%4 != 0 {
		if at(s.src, i) != '=' {
			s.fail(ErrBase64Padding, i, i+1)
			return token.BAD, i
		}
		i++
	}
	return token.BYTES, i
}

const (
	cSpace   uint8 = 1 << iota // ' ' '\t' '\n' '\r'
	cLetter                    // starts an identifier: A-Z a-z _
	cIdent                     // continues an identifier: A-Z a-z 0-9 _ ' - /
	cDigit                     // 0-9
	cHex                       // 0-9 A-F a-f
	cBase64                    // A-Z a-z 0-9 + /
	cTextEnd                   // ends a run of plain text: '"' '\n' '\\'
)

var class = func() (t [256]uint8) {
	for _, c := range []byte(" \t\n\r") {
		t[c] |= cSpace
	}
	for c := 'a'; c <= 'z'; c++ {
		t[c] |= cLetter | cIdent | cBase64
		t[c-'a'+'A'] |= cLetter | cIdent | cBase64
	}
	for c := '0'; c <= '9'; c++ {
		t[c] |= cIdent | cDigit | cHex | cBase64
	}
	for _, c := range []byte("abcdefABCDEF") {
		t[c] |= cHex
	}
	t['_'] |= cLetter | cIdent
	t['\''] |= cIdent
	t['-'] |= cIdent
	t['/'] |= cIdent | cBase64
	t['+'] |= cBase64
	for _, c := range []byte("\"\n\\") {
		t[c] |= cTextEnd
	}
	return
}()

// lead maps the first byte of a token to its kind, when that byte alone
// decides it: IDENT for a letter, which starts an identifier, or a one-byte
// operator. Other bytes map to BAD and need a closer look.
var lead = func() (t [256]token.Token) {
	for c := range t {
		if class[c]&cLetter != 0 {
			t[c] = token.IDENT
		}
	}
	for _, tok := range []token.Token{
		token.RPAREN, token.LBRACE, token.RBRACE, token.LBRACK, token.RBRACK,
		token.WHERE, token.COMMA, token.DEFINE, token.OPTION, token.MUL,
	} {
		t[tok.Op()[0]] = tok
	}
	return
}()
