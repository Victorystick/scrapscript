package token

import "testing"

func TestRender(t *testing.T) {
	src := NewSource([]byte("1 + \"a\""))
	err := src.Error(Span{Start: 4, End: 7}, "cannot unify 'text' with 'int'")

	plain := "error: cannot unify 'text' with 'int'\n\n    1: 1 + \"a\"\n           ~~~"
	if got := err.Error(); got != plain {
		t.Errorf("Error() = %q, want %q", got, plain)
	}
	colored := "\033[31merror\033[m: cannot unify 'text' with 'int'\n\n\033[33m    1\033[m: 1 + \"a\"\n           \033[31m~~~\033[m"
	if got := Render(err, true); got != colored {
		t.Errorf("Render(err, true) = %q, want %q", got, colored)
	}
}
