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
	s := scanner{src: src, pos: int(t.Starts[i])}
	s.token()
	return s.pos
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
	toks.Kinds = make([]token.Token, 0, n)
	toks.Starts = make([]uint32, 0, n)

	s := scanner{src: src}
	for {
		s.skipWhitespace()
		start := s.pos
		tok := s.token()
		toks.Kinds = append(toks.Kinds, tok)
		toks.Starts = append(toks.Starts, uint32(start))
		if tok == token.EOF {
			return toks, s.errs
		}
	}
}

type scanner struct {
	src  []byte
	pos  int
	errs []Error
}

func (s *scanner) fail(code ErrCode, start, end int) {
	s.errs = append(s.errs, Error{Code: code, Start: uint32(start), End: uint32(end)})
}

// at returns the byte at i, or 0 past the end of the source.
func (s *scanner) at(i int) byte {
	if i < len(s.src) {
		return s.src[i]
	}
	return 0
}

// skipWhitespace skips whitespace and comments, which are trivia.
func (s *scanner) skipWhitespace() {
	for s.pos < len(s.src) {
		switch {
		case class[s.src[s.pos]]&cSpace != 0:
			s.pos++
		case s.src[s.pos] == '-' && s.at(s.pos+1) == '-':
			s.comment()
		default:
			return
		}
	}
}

// comment skips a line comment, up to the end of its line. `--` must be
// followed by a space, another `-` or the end of the line; anything else is
// almost always a typo, so it is an error, though still skipped as a comment.
func (s *scanner) comment() {
	start := s.pos
	switch s.at(start + 2) {
	case ' ', '-', '\n', '\r', 0:
	default:
		s.fail(ErrBadComment, start, start+2)
	}
	end := bytes.IndexByte(s.src[start:], '\n')
	if end == -1 {
		s.pos = len(s.src)
	} else {
		s.pos = start + end
	}
}

// token scans the token starting at s.pos and advances past it. It depends
// only on the source and the start offset, which is what lets Tokens.End
// re-scan a single token.
func (s *scanner) token() token.Token {
	start := s.pos
	if start >= len(s.src) {
		return token.EOF
	}

	c := s.src[start]
	switch {
	case class[c]&cLetter != 0:
		s.pos = s.identEnd(start + 1)
		return token.IDENT
	case s.isStartOfNumber(start):
		return s.number(start)
	}

	s.pos = start + 1
	switch c {
	case '$':
		return token.IMPORT
	case '(':
		return s.switch2(token.LPAREN, ')', token.HOLE)
	case ')':
		return token.RPAREN
	case '{':
		return token.LBRACE
	case '}':
		return token.RBRACE
	case '[':
		return token.LBRACK
	case ']':
		return token.RBRACK
	case '~':
		if s.at(start+1) == '~' {
			return s.bytes(start)
		}
		return s.byte(start)
	case ';':
		return token.WHERE
	case ',':
		return token.COMMA
	case '"':
		return s.text(start)
	case '=':
		return s.switch2(token.ASSIGN, '>', token.FORALL)
	case '+':
		if s.at(start+1) == '<' {
			s.pos++
			return token.APPEND
		}
		return s.switch2(token.ADD, '+', token.CONCAT)
	case '-':
		if s.isStartOfNumber(start + 1) {
			return s.number(start)
		}
		return s.switch2(token.SUB, '>', token.ARROW)
	case '|':
		return s.switch2(token.PIPE, '>', token.RPIPE)
	case '<':
		return s.switch2(token.LT, '|', token.LPIPE)
	case '>':
		if s.at(start+1) == '>' {
			s.pos++
			return token.RCOMP
		}
		return s.switch2(token.GT, '+', token.PREPEND)
	case ':':
		return token.DEFINE
	case '#':
		return token.OPTION
	case '.':
		return s.switch2(token.ACCESS, '.', token.SPREAD)
	case '*':
		return token.MUL
	}

	return s.illegal(start)
}

// switch2 consumes the next byte and returns double if that byte is and;
// otherwise it returns single.
func (s *scanner) switch2(single token.Token, and byte, double token.Token) token.Token {
	if s.at(s.pos) == and {
		s.pos++
		return double
	}
	return single
}

func (s *scanner) illegal(start int) token.Token {
	if s.src[start] < utf8.RuneSelf {
		s.fail(ErrIllegalChar, start, start+1)
		return token.BAD
	}
	r, w := utf8.DecodeRune(s.src[start:])
	if r == utf8.RuneError && w == 1 {
		s.fail(ErrInvalidUTF8, start, start+w)
	} else {
		s.fail(ErrNonASCII, start, start+w)
	}
	s.pos = start + w
	return token.BAD
}

func (s *scanner) identEnd(i int) int {
	for i < len(s.src) && class[s.src[i]]&cIdent != 0 {
		i++
	}
	return i
}

func (s *scanner) isStartOfNumber(i int) bool {
	c := s.at(i)
	return class[c]&cDigit != 0 || c == '.' && class[s.at(i+1)]&cDigit != 0
}

func (s *scanner) digits(i int) int {
	for i < len(s.src) && class[s.src[i]]&cDigit != 0 {
		i++
	}
	return i
}

// number scans an INT or FLOAT, with an optional leading '-'.
func (s *scanner) number(start int) token.Token {
	i := start
	if s.src[i] == '-' {
		i++
	}
	tok := token.INT
	i = s.digits(i)
	if s.at(i) == '.' {
		tok = token.FLOAT
		i = s.digits(i + 1)
	}
	s.pos = i
	return tok
}

func (s *scanner) text(start int) token.Token {
	i := start + 1
	end := len(s.src)
	if j := bytes.IndexAny(s.src[i:], "\"\n"); j >= 0 {
		end = i + j
	}
	s.checkText(i, end)
	if end < len(s.src) && s.src[end] == '"' {
		s.pos = end + 1
	} else {
		s.fail(ErrUnterminatedText, start, end)
		s.pos = end
	}
	return token.TEXT
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
func (s *scanner) byte(start int) token.Token {
	for i := start + 1; i < start+3; i++ {
		if class[s.at(i)]&cHex == 0 {
			s.fail(ErrExpectedHex, i, i+1)
			s.pos = i
			return token.BAD
		}
	}
	s.pos = start + 3
	return token.BYTE
}

// bytes scans a bytes literal: '~~' and padded base64. A bare '~~' is empty.
func (s *scanner) bytes(start int) token.Token {
	b := start + 2
	i := b
	for i < len(s.src) && class[s.src[i]]&cBase64 != 0 {
		i++
	}
	for (i-b)%4 != 0 {
		if s.at(i) != '=' {
			s.fail(ErrBase64Padding, i, i+1)
			s.pos = i
			return token.BAD
		}
		i++
	}
	s.pos = i
	return token.BYTES
}

const (
	cSpace  uint8 = 1 << iota // ' ' '\t' '\n' '\r'
	cLetter                   // starts an identifier: A-Z a-z _
	cIdent                    // continues an identifier: A-Z a-z 0-9 _ - /
	cDigit                    // 0-9
	cHex                      // 0-9 A-F a-f
	cBase64                   // A-Z a-z 0-9 + /
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
	t['-'] |= cIdent
	t['/'] |= cIdent | cBase64
	t['+'] |= cBase64
	return
}()
