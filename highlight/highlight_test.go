package highlight

import (
	"strings"
	"testing"
)

// mark returns src with each span wrapped as {class text}.
func mark(src string) string {
	s := Highlight([]byte(src))
	var b strings.Builder
	at := 0
	for i, c := range s.Classes {
		b.WriteString(src[at:s.Starts[i]])
		b.WriteString("{" + c.String() + " " + src[s.Starts[i]:s.Ends[i]] + "}")
		at = int(s.Ends[i])
	}
	b.WriteString(src[at:])
	return b.String()
}

func TestHighlight(t *testing.T) {
	tests := []struct{ src, want string }{
		{"f 1 -2.5", "f {number 1} {number -2.5}"},
		{`"a\n" ++ ~ff ++ ~~aGk=`, `{string "a\n"} {operator ++} {string.special ~ff} {operator ++} {string.special ~~aGk=}`},
		{"#some x |> # none", "{constructor #some} x {operator |>} {constructor #} {constructor none}"},
		{`$"lib/x" ++ $sha256:ab12`, `{keyword $}{string "lib/x"} {operator ++} {keyword $sha256:}{string.special ab12}`},
		{"| () -> a.b ; c = [..d]", "{punctuation.special |} {constant.builtin ()} {operator ->} a{punctuation.delimiter .}b {punctuation.delimiter ;} c {operator =} {punctuation.bracket [}{punctuation.delimiter ..}d{punctuation.bracket ]}"},
		{"-- note\n--- doc\n---\n---- rule\nx -- end", "{comment -- note}\n{comment.doc --- doc}\n{comment.doc ---}\n{comment ---- rule}\nx {comment -- end}"},
		{"x\r\n--- doc\r\ny", "x\r\n{comment.doc --- doc\r}\ny"},
		// A malformed token gets no span; scanning resumes after it.
		{"~x 1", "~x {number 1}"},
	}
	for _, tt := range tests {
		if got := mark(tt.src); got != tt.want {
			t.Errorf("Highlight(%q)\n got %q\nwant %q", tt.src, got, tt.want)
		}
	}
}

func TestANSI(t *testing.T) {
	src := []byte("#x\n\"a\" ++ 1\n")
	// Only the second line, without its newline; identifiers stay plain.
	want := "\033[32m\"a\"\033[m \033[34m++\033[m \033[35m1\033[m"
	if got := ANSI(src, 3, 11); got != want {
		t.Errorf("ANSI = %q, want %q", got, want)
	}
}
