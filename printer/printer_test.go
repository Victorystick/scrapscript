package printer

import (
	"bytes"
	"testing"

	"github.com/Victorystick/scrapscript/parser"
)

func TestPrint(t *testing.T) {
	expect(t, `f 1 . f = a -> a`, `f 1
. f = a -> a`)

	expect(t, `f "b" . f = | "a" -> 1 | "b" -> 2 | "c" -> 3 | x -> 0`, `f "b"
. f =
  | "a" -> 1
  | "b" -> 2
  | "c" -> 3
  | x -> 0`)

	expect(t, `| "hey" -> "" | "hello " ++ name -> name | _ -> ""`, `
| "hey" -> ""
| "hello " ++ name -> name
| _ -> ""`)

	expect(t, `a + b + c . a = 1 . b = 2 . c = 3`, `a + b + c
. a = 1
. b = 2
. c = 3`)
}

func expect(t *testing.T, source, expected string) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		t.Error(err)
	} else {
		var buf bytes.Buffer
		err = Fprint(&buf, []byte(source), expr)
		if err != nil {
			t.Error(err)
		} else {
			output := buf.String()
			if output != expected {
				t.Errorf("Expected:\n%s\nGot:\n%s ", expected, output)
			}
		}
	}
}
