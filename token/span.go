package token

import (
	"bytes"
	"sort"
)

type Source struct {
	bytes []byte
	lines []int // offsets at which lines start; built on first use
}

func NewSource(bytes []byte) Source {
	return Source{bytes: bytes}
}

func (s *Source) Error(span Span, msg string) Error {
	pos := s.GetPosition(span.Start)
	return Error{
		Pos:   pos,
		Range: span,
		Msg:   msg,
		Line:  s.GetLine(pos.Line),
		Src:   s.bytes,
	}
}

func (s *Source) Bytes() []byte {
	return s.bytes
}

// lineStarts returns the offset at which each line starts. Only diagnostics
// need it, so it is computed on demand instead of while scanning.
func (s *Source) lineStarts() []int {
	if s.lines == nil {
		s.lines = []int{0}
		for i := 0; ; {
			j := bytes.IndexByte(s.bytes[i:], '\n')
			if j < 0 {
				break
			}
			i += j + 1
			s.lines = append(s.lines, i)
		}
	}
	return s.lines
}

type Position struct {
	Line, Column int
}

func (s *Source) GetPosition(offset int) (p Position) {
	lines := s.lineStarts()
	i := sort.Search(len(lines), func(i int) bool { return lines[i] > offset }) - 1
	if i >= 0 {
		p.Line, p.Column = i+1, offset-lines[i]+1
	}
	return
}

// GetLine returns the string contents of a 1-indexed line, without its newline.
func (s *Source) GetLine(i int) string {
	lines := s.lineStarts()
	if i <= 0 || i > len(lines) {
		return ""
	}
	span := Span{Start: lines[i-1], End: len(s.bytes)}
	if i < len(lines) {
		span.End = lines[i] - 1
	}
	return s.GetString(span)
}

func (s *Source) GetString(span Span) string {
	return span.Get(s.bytes)
}

type Span struct {
	Start int // First index into the parsed source.
	End   int // End index.
}

func (span Span) Len() int {
	return span.End - span.Start
}

// Get returns the string sliced from src.
func (span Span) Get(src []byte) string {
	return string(src[span.Start:span.End])
}

// TrimBoth removes a character from either side of the Span,
// for example the quotes of a string literal.
func (span Span) TrimBoth() Span {
	span.Start += 1
	span.End -= 1
	return span
}

// TrimStart removes `n` characters from the start of the Span,
// for example the tildes from a byte or bytes literal.
func (span Span) TrimStart(n int) Span {
	span.Start += n
	return span
}
