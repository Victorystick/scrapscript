// Package highlight classifies a scrap's source for syntax highlighting.
// It works from the scanner's tokens alone, so it knows tags, literals,
// references, comments and operators, but not what a name is bound to.
package highlight

import (
	"bytes"
	"fmt"
	"html"
	"strings"

	"github.com/Victorystick/scrapscript/scanner"
	"github.com/Victorystick/scrapscript/token"
)

// Class is the kind of a highlighted span. Its name is the tree-sitter
// capture that the editor grammar uses for the same syntax, so that themes
// carry over.
type Class uint8

const (
	Comment Class = iota + 1
	DocComment
	Number
	String
	StringSpecial // bytes and a hash's digest
	Keyword       // the $ or $scheme: of a reference
	Constructor   // a tag
	Constant      // the hole, ()
	Operator
	Separator // the | of a match arm
	Delimiter // : ; , . ..
	Bracket
)

var names = [...]string{
	Comment:       "comment",
	DocComment:    "comment.doc",
	Number:        "number",
	String:        "string",
	StringSpecial: "string.special",
	Keyword:       "keyword",
	Constructor:   "constructor",
	Constant:      "constant.builtin",
	Operator:      "operator",
	Separator:     "punctuation.special",
	Delimiter:     "punctuation.delimiter",
	Bracket:       "punctuation.bracket",
}

func (c Class) String() string {
	return names[c]
}

// Spans are the highlighted parts of a source as parallel arrays, in order
// and not overlapping: span i has class Classes[i] and covers the bytes from
// Starts[i] to Ends[i]. Identifiers, whitespace and malformed tokens have no
// span.
type Spans struct {
	Classes []Class
	Starts  []uint32
	Ends    []uint32
}

func (s *Spans) add(c Class, start, end int) {
	s.Classes = append(s.Classes, c)
	s.Starts = append(s.Starts, uint32(start))
	s.Ends = append(s.Ends, uint32(end))
}

// Highlight returns the spans of src. Like the scanner, it doesn't stop at
// errors.
func Highlight(src []byte) (s Spans) {
	toks, _ := scanner.Scan(src)
	end := 0
	for i, kind := range toks.Kinds {
		start := int(toks.Starts[i])
		// Only whitespace and comments lie between tokens.
		s.comments(src, end, start)
		if kind == token.EOF {
			break
		}
		end = toks.End(src, i)

		switch kind {
		case token.INT, token.FLOAT:
			s.add(Number, start, end)
		case token.TEXT:
			s.add(String, start, end)
		case token.BYTE, token.BYTES:
			s.add(StringSpecial, start, end)
		case token.PATH:
			s.add(Keyword, start, start+1)
			s.add(String, start+1, end)
		case token.HASH:
			colon := start + bytes.IndexByte(src[start:end], ':') + 1
			s.add(Keyword, start, colon)
			s.add(StringSpecial, colon, end)
		case token.HOLE:
			s.add(Constant, start, end)
		case token.OPTION:
			s.add(Constructor, start, end)
		case token.IDENT:
			if i > 0 && toks.Kinds[i-1] == token.OPTION {
				if n := len(s.Ends) - 1; s.Ends[n] == uint32(start) {
					s.Ends[n] = uint32(end) // #tag is one span
				} else {
					s.add(Constructor, start, end)
				}
			}
		case token.PIPE:
			s.add(Separator, start, end)
		case token.DEFINE, token.WHERE, token.COMMA, token.ACCESS, token.SPREAD:
			s.add(Delimiter, start, end)
		case token.LPAREN, token.RPAREN, token.LBRACK, token.RBRACK, token.LBRACE, token.RBRACE:
			s.add(Bracket, start, end)
		case token.BAD:
		default:
			s.add(Operator, start, end)
		}
	}
	return s
}

// comments adds the comments between start and end, which hold only
// whitespace and comments.
func (s *Spans) comments(src []byte, start, end int) {
	for {
		i := bytes.Index(src[start:end], []byte("--"))
		if i < 0 {
			return
		}
		start += i
		stop := bytes.IndexByte(src[start:end], '\n')
		if stop < 0 {
			stop = end
		} else {
			stop += start
		}
		s.add(commentClass(src[start:stop]), start, stop)
		start = stop
	}
}

// commentClass returns DocComment for a comment that starts with `---`
// followed by a space or the end of the line; `----` is a plain comment.
func commentClass(c []byte) Class {
	if c = bytes.TrimRight(c, "\r"); bytes.HasPrefix(c, []byte("---")) && (len(c) == 3 || c[3] == ' ') {
		return DocComment
	}
	return Comment
}

// ansi are the terminal colors of each class; 0 leaves it uncolored.
var ansi = [...]int{
	Comment:       90, // gray
	DocComment:    90,
	Number:        35, // magenta
	String:        32, // green
	StringSpecial: 36, // cyan
	Keyword:       35,
	Constructor:   33, // yellow
	Constant:      35,
	Operator:      34, // blue
}

// ANSI returns src from start to end in terminal colors. It highlights all
// of src, so that a part that starts inside a multi-line text or comment
// is colored as such. It is a token.Highlighter.
func ANSI(src []byte, start, end int) string {
	s := Highlight(src)
	var b strings.Builder
	at := start
	for i, c := range s.Classes {
		from, to := max(int(s.Starts[i]), start), min(int(s.Ends[i]), end)
		if from >= to || ansi[c] == 0 {
			continue
		}
		b.Write(src[at:from])
		fmt.Fprintf(&b, "\033[%dm%s\033[m", ansi[c], src[from:to])
		at = to
	}
	b.Write(src[at:end])
	return b.String()
}

// HTML returns src as HTML, with each span in a <span> whose class is
// hl- and the class's name, dots as dashes: hl-string-special.
func HTML(src []byte) string {
	s := Highlight(src)
	var b strings.Builder
	at := 0
	for i, c := range s.Classes {
		start, end := int(s.Starts[i]), int(s.Ends[i])
		b.WriteString(html.EscapeString(string(src[at:start])))
		fmt.Fprintf(&b, `<span class="hl-%s">%s</span>`,
			strings.ReplaceAll(c.String(), ".", "-"), html.EscapeString(string(src[start:end])))
		at = end
	}
	b.WriteString(html.EscapeString(string(src[at:])))
	return b.String()
}
