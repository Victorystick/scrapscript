package types

import (
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/parser"
)

func TestExhaustive(t *testing.T) {
	examples := []string{
		`| _ -> 1`,
		`| () -> 1`,
		`| #a -> 1 | #b -> 2`,
		`| #a #x -> 1 | #a #y -> 2 | #b -> 3`,
		`| { ..r, p = #a } -> 1 | { ..r, p = #b } -> 2`,
		`| [] -> 0 | [_] -> 1 | _ >+ _ >+ _ -> 2`,
		`| [] -> 0 | x >+ _ -> x`,
		`| [] -> 0 | _ +< x -> x`,
		`| [] -> 0 | [a] -> a | [a] ++ _ ++ [b] -> 3`,
		`| [] -> 0 | [1] ++ _ -> 1 | _ ++ [2] -> 2 | _ -> 3`,
		`| 0 -> 1 | n -> n`,
		// Without a catch-all, the enums are closed.
		`| #a #x -> 1 | #b -> 2 | #a #y -> 3 | #a #x -> 1`,
		`| #a (#b x) -> 1 | #a (#c y) -> 2`,
		`| [#a] -> 1 | [#b] -> 2 | _ -> 3`,
		// The annotation closes the enum that the catch-all leaves open.
		`f ; f : { p : [#a, #b], q : [#c, #d] } -> int = | { p = #a, q = _ } -> 1 | { p = _, q = #c } -> 2 | { p = #b, q = #d } -> 3`,
		// Recursive types.
		`size ; size : tree -> int = | #leaf -> 0 | #node { left = l, right = r } -> 1 + size l + size r
; tree : type = [#leaf, #node { left : tree, right : tree }]`,
	}
	for _, source := range examples {
		var reg Registry
		if _, err := Infer(&reg, nil, must(parser.ParseExpr(source)), nil); err != nil {
			t.Errorf("%s: %v", source, err)
		}
	}
}

func TestNotExhaustive(t *testing.T) {
	examples := []struct{ source, missing string }{
		{`| 0 -> 1`, `_`},
		{`| "a" -> 1`, `_`},
		{`| { p = #a, q = #a } -> 1 | { p = #b, q = #b } -> 2`, `{ p = #a, q = #b }`},
		{`| { p = #a, q = _ } -> 1 | { p = _, q = #b } -> 2`, `{ p = _, q = _ }`},
		// A catch-all at q leaves its enum open.
		{`| { p = #a, q = _ } -> 1 | { p = #b, q = #c } -> 2`, `{ p = #b, q = _ }`},
		{`| #a { x = #b } -> 1 | #c -> 2 | #a { x = #d 1 } -> 3`, `#a { x = #d _ }`},
		{`| [] -> 1`, `[ _ ] ++ _`},
		{`| x >+ _ -> x`, `[]`},
		{`| [] -> 0 | [a] -> a | [a, b] ++ _ ++ [c] -> c`, `[ _, _ ]`},
		{`| [] -> 0 | [1] ++ _ -> 1 | _ ++ [2] -> 2`, `[ _ ]`},
		{`| [#a] -> 1 | [#b] -> 2 | [] -> 3`, `[ _, _ ] ++ _`},
		// The rest is a catch-all for the elements, so their enum stays open.
		{`| [] -> 0 | [#a] ++ _ -> 1`, `[ _ ] ++ _`},
		{`| { p = 0 } -> 1`, `{ p = _ }`},
		// Inner match functions are checked too.
		{`| #a -> (| 1 -> 1) | #b -> (x -> x)`, `_`},
		// Annotations close enums that catch-alls leave open, or keep them open.
		{`f ; f : { p : [#a, #b], q : [#c, #d] } -> int = | { p = #a, q = _ } -> 1 | { p = _, q = #c } -> 2`, `{ p = #b, q = #d }`},
		{`f ; f : r => { p : [#a, ..r], q : int } -> int = | { p = #a, q = _ } -> 1 | { p = _, q = 0 } -> 2`, `{ p = _, q = _ }`},
	}
	for _, ex := range examples {
		var reg Registry
		_, err := Infer(&reg, nil, must(parser.ParseExpr(ex.source)), nil)
		want := "match is not exhaustive: no arm matches " + ex.missing + "\n"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", ex.source, err, want)
		}
	}
}
