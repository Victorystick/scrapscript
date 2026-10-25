package token

import "testing"

func TestRender(t *testing.T) {
	src := NewSource([]byte("x\n1 + \"a\"\n; x = 1"))
	err := src.Error(Span{Start: 6, End: 9}, "cannot unify 'text' with 'int'")

	plain := "error: cannot unify 'text' with 'int'\n\n    2: 1 + \"a\"\n           ~~~"
	if got := err.Error(); got != plain {
		t.Errorf("Error() = %q, want %q", got, plain)
	}
	// The highlighter is given the whole source and the line's bounds.
	hl := func(src []byte, start, end int) string { return "<" + string(src[start:end]) + ">" }
	colored := "\033[31merror\033[m: cannot unify 'text' with 'int'\n\n\033[33m    2\033[m: <1 + \"a\">\n           \033[31m~~~\033[m"
	if got := Render(err, hl); got != colored {
		t.Errorf("Render(err, hl) = %q, want %q", got, colored)
	}
}
