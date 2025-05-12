package token

type Source struct {
	bytes []byte
}

func NewSource(bytes []byte) Source {
	return Source{bytes}
}

func (s *Source) Bytes() []byte {
	return s.bytes
}

func (s *Source) GetString(span Span) string {
	return span.Get(s.bytes)
}

type Span struct {
	Start int // First index into the parsed source.
	End   int // End index.
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
// for example the semicolons from a byte or bytes literal.
func (span Span) TrimStart(n int) Span {
	span.Start += n
	return span
}
