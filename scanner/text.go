package scanner

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// escapeLen returns the length of the escape sequence that b starts with, or
// 0 if it isn't a valid one. b starts with a backslash.
func escapeLen(b []byte) int {
	if len(b) < 2 {
		return 0
	}
	switch b[1] {
	case '"', '\\', 'n', 't':
		return 2
	case 'u':
		_, n := unicodeEscape(b)
		return n
	}
	return 0
}

// unicodeEscape decodes the \u{HEX} escape that b starts with, returning the
// rune and the escape's length, or a length of 0 if it isn't valid: one to
// six hex digits naming a Unicode scalar value.
func unicodeEscape(b []byte) (rune, int) {
	if len(b) < 4 || b[2] != '{' {
		return 0, 0
	}
	var r rune
	for i := 3; i < len(b) && i <= 9; i++ {
		if b[i] == '}' {
			if i == 3 || !utf8.ValidRune(r) {
				return 0, 0
			}
			return r, i + 1
		}
		if class[b[i]]&cHex == 0 {
			return 0, 0
		}
		d, _ := strconv.ParseUint(string(b[i]), 16, 8)
		r = r<<4 | rune(d)
	}
	return 0, 0
}

// Unquote returns the text that a TEXT token's source denotes, decoding its
// escapes. The token must have scanned without errors.
func Unquote(lit string) string {
	body := lit[1 : len(lit)-1]
	if strings.IndexByte(body, '\\') < 0 {
		return body
	}
	var b strings.Builder
	for i := 0; i < len(body); {
		c := body[i]
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}
		switch body[i+1] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'u':
			r, n := unicodeEscape([]byte(body[i:]))
			b.WriteRune(r)
			i += n
			continue
		default: // '"' or '\\'
			b.WriteByte(body[i+1])
		}
		i += 2
	}
	return b.String()
}

// Quote returns a TEXT literal that Unquote reads back as s, which must be
// valid UTF-8. Printable characters other than '"' and '\' are kept as they
// are; the rest are escaped.
func Quote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.IsPrint(r):
			b.WriteRune(r)
		default:
			b.WriteString(`\u{`)
			b.WriteString(strings.ToUpper(strconv.FormatInt(int64(r), 16)))
			b.WriteByte('}')
		}
	}
	b.WriteByte('"')
	return b.String()
}
