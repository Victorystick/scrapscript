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
		{`list/length`, `list a -> int`},
		{`list/map`, `(a -> b) -> list a -> list b`},
		{`list/map (a -> a + 1)`, `list int -> list int`},
		{`list/fold`, `a -> (a -> b -> a) -> list b -> a`},
		{`list/repeat`, `int -> a -> list a`},

		// text
		{`text/length`, `text -> int`},
		{`text/repeat`, `int -> text -> text`},
		{`text/join`, `text -> list text -> text`},

		{`list/fold 0 (a -> b -> a + text/length b)`, `list text -> int`},
		{`list/fold 0 (a -> b -> a + text/length b) ["hey", "beautiful"]`, `int`},

		{`| 0 -> [0] | n -> seq (n - 1) +< n ; seq = x -> [x]`, `int -> list int`},
		{`seq ; seq = | 0 -> [0] | n -> seq (n - 1) +< n`, `int -> list int`},
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
