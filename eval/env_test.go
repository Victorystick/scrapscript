package eval

import "testing"

func TestInferBuiltin(t *testing.T) {
	examples := []struct {
		source string
		result string
	}{
		// numeric conversions
		{`round`, `float -> int`},
		{`ceil`, `float -> int`},
		{`floor`, `float -> int`},
		{`to-float`, `int -> float`},

		// byte <-> text conversion
		{`bytes/to-utf8-text`, `bytes -> text`},
		{`bytes/from-utf8-text`, `text -> bytes`},

		// list
		{`list/length`, `list $0 -> int`},
		{`list/map`, `($0 -> $1) -> list $0 -> list $1`},
		{`list/map (a -> a + 1)`, `list int -> list int`},
		{`list/fold`, `$0 -> ($0 -> $1 -> $0) -> list $1 -> $0`},
		{`list/repeat`, `int -> $0 -> list $0`},

		// text
		{`text/length`, `text -> int`},
		{`text/repeat`, `int -> text -> text`},
		{`text/join`, `text -> list text -> text`},

		{`list/fold 0 (a -> b -> a + text/length b)`, `list text -> int`},
		{`list/fold 0 (a -> b -> a + text/length b) ["hey", "beautiful"]`, `int`},

		{`fix`, `($0 -> $1) -> $0 -> $1`},
		{`fix (a -> a)`, `$3 -> $3`},

		// TODO: These should be equivalent, from a type perspective.
		{`| 0 -> [0] | n -> seq (n - 1) +< n ; seq = x -> [x]`, `int -> list int`},
		{`fix (seq -> | 0 -> [0] | n -> seq (n - 1) +< n)`, `(int -> list int) -> int -> list int`},
	}

	for _, ex := range examples {
		env := NewEnvironment()
		scrap, err := env.Read([]byte(ex.source))

		if err != nil {
			t.Error(err)
			continue
		}

		res, err := env.Infer(scrap)

		if err != nil {
			t.Error(err)
		} else {
			if res != ex.result {
				t.Errorf("Invalid type for '%s'\n  expected: %s\n       got: %s", ex.source, ex.result, res)
			}
		}
	}
}
