package token

import "bytes"

type Source struct {
	bytes []byte
	lines []int // indices of new lines
}

func NewSource(bytes []byte) Source {
	return Source{bytes, []int{0}}
}

func (s *Source) Error(span Span, msg string) Error {
	pos := s.GetPosition(span.Start)
	return Error{
		Pos:   pos,
		Range: span,
		Msg:   msg,
		Line:  s.GetLine(pos.Line),
	}
}

func (s *Source) Bytes() []byte {
	return s.bytes
}

func (s *Source) AddLineBreak(offset int) {
	s.lines = append(s.lines, offset)
}

func (s *Source) LineCount() int {
	return len(s.lines)
}

type Position struct {
	Line, Column int
}

func (s *Source) GetPosition(offset int) (p Position) {
	if i := searchInts(s.lines, offset); i >= 0 {
		p.Line, p.Column = i+1, offset-s.lines[i]+1
	}
	return
}

// GetLine returns the string contents of a 1-indexed line.
func (s *Source) GetLine(i int) string {
	if i <= 0 {
		return ""
	}

	span := Span{Start: s.lines[i-1]}

	// If asking for the currently tokenized line, find the next newline.
	if i == len(s.lines) {
		offset := bytes.IndexByte(s.bytes[span.Start:], '\n')
		if offset < 0 {
			span.End = len(s.bytes)
		} else {
			span.End = span.Start + offset
		}
	} else if i < len(s.lines) {
		// Skip newline.
		span.End = s.lines[i] - 1
	}

	return s.GetString(span)
}

func searchInts(a []int, x int) int {
	// This function body is a manually inlined version of:
	//
	//   return sort.Search(len(a), func(i int) bool { return a[i] > x }) - 1
	i, j := 0, len(a)
	for i < j {
		h := int(uint(i+j) >> 1) // avoid overflow when computing h
		// i ≤ h < j
		if a[h] <= x {
			i = h + 1
		} else {
			j = h
		}
	}
	return i - 1
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
