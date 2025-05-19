package scanner

import (
	"unicode"
	"unicode/utf8"

	"github.com/Victorystick/scrapscript/token"
)

type Scanner struct {
	source *token.Source

	src []byte // source
	err ErrorHandler

	// mutable
	ch         rune // current character
	offset     int  // character offset
	rdOffset   int  // reading offset (position after current character)
	lineOffset int  // current line offset
}

const (
	bom = 0xFEFF // byte order mark, only permitted as very first character
	eof = -1     // end of file
)

func (s *Scanner) Init(source *token.Source, err ErrorHandler) {
	s.source = source
	s.src = source.Bytes()
	s.err = err

	s.ch = ' '
	s.offset = 0
	s.rdOffset = 0
}

func (s *Scanner) span(start int) token.Span {
	return token.Span{Start: start, End: s.offset}
}

// Only valid for the current token while scanning.
func (s *Scanner) error(offs int, msg string) {
	if s.err != nil {
		span := token.Span{Start: offs, End: offs + 1}
		s.err(s.source.Error(span, msg))
	}
}

func (s *Scanner) next() {
	if s.rdOffset < len(s.src) {
		s.offset = s.rdOffset
		if s.ch == '\n' {
			s.lineOffset = s.offset
			s.source.AddLineBreak(s.offset)
		}
		r, w := rune(s.src[s.rdOffset]), 1
		switch {
		case r == 0:
			s.error(s.offset, "illegal character NUL")
		case r >= utf8.RuneSelf:
			// not ASCII
			r, w = utf8.DecodeRune(s.src[s.rdOffset:])
			if r == utf8.RuneError && w == 1 {
				s.error(s.offset, "illegal UTF-8 encoding")
			} else if r == bom && s.offset > 0 {
				s.error(s.offset, "illegal byte order mark")
			}
		}
		s.rdOffset += w
		s.ch = r
	} else {
		s.offset = len(s.src)
		if s.ch == '\n' {
			s.lineOffset = s.offset
			s.source.AddLineBreak(s.offset)
		}
		s.ch = eof
	}
}

func (s *Scanner) peek() byte {
	if s.rdOffset < len(s.src) {
		return s.src[s.rdOffset]
	}
	return 0
}

func (s *Scanner) skipWhitespace() {
	for s.ch == ' ' || s.ch == '\t' || s.ch == '\n' || s.ch == '\r' {
		s.next()
	}
}

func (s *Scanner) scanIdentifier() token.Span {
	offs := s.offset

	// Optimize for the common case of an ASCII identifier.
	//
	// Ranging over s.src[s.rdOffset:] lets us avoid some bounds checks, and
	// avoids conversions to runes.
	//
	// In case we encounter a non-ASCII character, fall back on the slower path
	// of calling into s.next().
	for rdOffset, b := range s.src[s.rdOffset:] {
		if 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || b == '_' || b == '-' || '0' <= b && b <= '9' {
			// Avoid assigning a rune for the common case of an ascii character.
			continue
		}
		s.rdOffset += rdOffset
		if 0 < b && b < utf8.RuneSelf {
			// Optimization: we've encountered an ASCII character that's not a letter
			// or number. Avoid the call into s.next() and corresponding set up.
			//
			// Note that s.next() does some line accounting if s.ch is '\n', so this
			// shortcut is only possible because we know that the preceding character
			// is not '\n'.
			s.ch = rune(b)
			s.offset = s.rdOffset
			s.rdOffset++
			goto exit
		}
		// We know that the preceding character is valid for an identifier because
		// scanIdentifier is only called when s.ch is a letter, so calling s.next()
		// at s.rdOffset resets the scanner state.
		s.next()
		for isLetter(s.ch) || isDigit(s.ch) {
			s.next()
		}
		goto exit
	}
	s.offset = len(s.src)
	s.rdOffset = len(s.src)
	s.ch = eof

exit:
	return s.span(offs)
}

func (s *Scanner) byte() (tok token.Token, span token.Span) {
	offs := s.offset

	for s.offset-offs < 2 {
		if !isHex(s.ch) {
			s.error(s.offset, "expected hex")
			return
		}
		s.next()
	}

	return token.BYTE, s.span(offs - 1)
}

func (s *Scanner) bytes() (tok token.Token, span token.Span) {
	offs := s.offset

	for isBase64(s.ch) {
		s.next()
	}

	if s.offset-offs < 2 {
		s.error(s.offset, "too short base64 string")
		tok = token.BAD
		return
	}

	for (s.offset-offs)%4 > 0 {
		if s.ch != '=' {
			s.error(s.offset, "missing base64 padding")
			tok = token.BAD
			return
		}
		s.next()
	}

	return token.BYTES, s.span(offs - 2)
}

func (s *Scanner) scanNumber() (tok token.Token, span token.Span) {
	offs := s.offset
	// invalid := -1 // index of invalid digit in literal, or < 0

	// integer part
	if s.ch != '.' {
		tok = token.INT
		for isDecimal(s.ch) {
			s.next()
		}
	}

	// fractional part
	if s.ch == '.' {
		tok = token.FLOAT
		s.next()
		for isDecimal(s.ch) {
			s.next()
		}
	}

	span = s.span(offs)
	return
}

func (s *Scanner) scanText() token.Span {
	// '"' opening already consumed
	start := s.offset - 1

	for {
		ch := s.ch
		if ch == '\n' || ch < 0 {
			s.error(start, "string literal not terminated")
			break
		}
		s.next()
		if ch == '"' {
			break
		}
		// if ch == '\\' {
		// 	s.scanEscape('"')
		// }
	}

	return s.span(start)
}

func (s *Scanner) switch2(single token.Token, and rune, double token.Token) (token.Token, token.Span) {
	start := s.offset - 1
	if s.ch == and {
		s.next()
		return double, s.span(start)
	}
	return single, s.span(start)
}

func (s *Scanner) char(tok token.Token) (token.Token, token.Span) {
	return tok, s.span(s.offset - 1)
}

func (s *Scanner) Scan() (token.Token, token.Span) {
	s.skipWhitespace()
	start := s.offset

	switch ch := s.ch; {
	case isLetter(ch):
		return token.IDENT, s.scanIdentifier()
	case isDecimal(ch) || ch == '.' && isDecimal(rune(s.peek())):
		return s.scanNumber()
	default:
		s.next() // always make progress
		switch ch {
		case eof:
			return token.EOF, token.Span{Start: start, End: start}
		case '(':
			return s.switch2(token.LPAREN, ')', token.HOLE)
		case ')':
			return s.char(token.RPAREN)
		case '{':
			return s.char(token.LBRACE)
		case '}':
			return s.char(token.RBRACE)
		case '[':
			return s.char(token.LBRACK)
		case ']':
			return s.char(token.RBRACK)
		case '~':
			if s.ch == '~' {
				s.next()
				return s.bytes()
			}
			return s.byte()
		case ';':
			return s.char(token.WHERE)
		case ',':
			return s.char(token.COMMA)
		case '"':
			return token.TEXT, s.scanText()
		case '=':
			return s.char(token.ASSIGN)
		case '+':
			if s.ch == '<' {
				s.next()
				return token.APPEND, s.span(start)
			}
			return s.switch2(token.ADD, '+', token.CONCAT)
		case '-':
			return s.switch2(token.SUB, '>', token.ARROW)
		case '|':
			return s.switch2(token.PIPE, '>', token.RPIPE)
		case '<':
			if s.ch == '|' {
				s.next()
				return token.LPIPE, s.span(start)
			}
			return token.LT, s.span(start)
		case '>':
			if s.ch == '>' {
				s.next()
				return token.RCOMP, s.span(start)
			}
			return s.switch2(token.GT, '+', token.PREPEND)
		case ':':
			return s.switch2(token.DEFINE, ':', token.PICK)
		case '#':
			return token.OPTION, s.span(start)
		case '*':
			return token.MUL, s.span(start)
		}
	}

	return token.BAD, s.span(start)
}

func isBase64(ch rune) bool {
	return isAlpha(ch) || isDecimal(ch) || ch == '+' || ch == '/'
}

func isAlpha(ch rune) bool {
	return 'a' <= lower(ch) && lower(ch) <= 'z'
}

func isLetter(ch rune) bool {
	return 'a' <= lower(ch) && lower(ch) <= 'z' || ch == '$' || ch == '_' || ch >= utf8.RuneSelf && unicode.IsLetter(ch)
}
func isDigit(ch rune) bool {
	return isDecimal(ch) || ch >= utf8.RuneSelf && unicode.IsDigit(ch)
}

func lower(ch rune) rune     { return ('a' - 'A') | ch } // returns lower-case ch iff ch is ASCII letter
func isDecimal(ch rune) bool { return '0' <= ch && ch <= '9' }
func isHex(ch rune) bool     { return '0' <= ch && ch <= '9' || 'a' <= lower(ch) && lower(ch) <= 'f' }
