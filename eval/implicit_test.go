package eval

import "testing"

// An ordering as an instance, and functions constrained by it.
const ordering = `
; ord : type -> type = a => { cmp : a -> a -> [#lt, #eq, #gt] }
; cmp : a => ord a => a -> a -> [#lt, #eq, #gt] = o -> o.cmp
; max : a => ord a => a -> a -> a = o -> x -> y -> cmp x y |> | #lt -> y | _ -> x
; lex : a => ord a => list a -> list a -> [#lt, #eq, #gt] = o ->
    | []      -> []      -> #eq
    | []      -> _       -> #lt
    | _       -> []      -> #gt
    | x >+ xs -> y >+ ys -> (cmp x y |> | #eq -> lex xs ys | r -> r)`

const intOrd = `
; _ : ord int = { cmp = int/cmp }`

const listOrd = `
; _ : a => ord a => ord (list a) = o -> { cmp = lex }`

var implicits = []struct{ source, result string }{
	{`max 3 4` + ordering + intOrd, `4`},
	{`[max 3 4, max 2 1]` + ordering + intOrd, `[ 4, 2 ]`},
	// A constrained function's constraints are solved where it is used, by
	// its own instance.
	{`max-of 0 [3, 5, 1] ; max-of : a => ord a => a -> list a -> a = o -> z -> list/fold z max` + ordering + intOrd, `5`},
	// An instance of lists, for lists of anything ordered.
	{`max [1, 2] [1, 3]` + ordering + intOrd + listOrd, `[ 1, 3 ]`},
	{`max [[1], [2]] [[1], [1, 5]]` + ordering + intOrd + listOrd, `[ [ 1 ], [ 2 ] ]`},
	// The innermost instance wins, so a scope can choose another.
	{`(max 3 4 ; _ : ord int = { cmp = x -> y -> int/cmp y x })` + ordering, `3`},
	// An instance without an annotation is one of its value's type.
	{`max "a" "b" ; _ = { cmp = text/cmp }` + ordering, `"b"`},
	// A binding without a constraint takes the one instance that fits.
	{`f 1 2 ; f = x -> y -> max x y` + ordering + intOrd, `2`},
	// A record of constrained functions keeps their constraints, as a
	// library exports them.
	{`lib.max 3 4 ; lib = { max = max }` + ordering + intOrd, `4`},
	{`lib.max "a" "b" ; _ = { cmp = text/cmp } ; lib = { max = max }` + ordering + intOrd, `"b"`},
	// A binding that uses an instance is evaluated after it.
	{`m ; m = max 1 2 ; _ = { cmp = c } ; c = int/cmp` + ordering, `2`},
	// A function applied to its instances prints them as instances in scope.
	{`max 3` + ordering + intOrd, `(max 3 ; _ = { cmp = int/cmp })
; max : a => ord a => a -> a -> a = o -> x -> y -> cmp x y |> | #lt -> y | _ -> x
; ord : type -> type = a => { cmp : a -> a -> [ #lt, #eq, #gt ] }
; cmp : a => ord a => a -> a -> [ #lt, #eq, #gt ] = o -> o.cmp`},
}

func TestImplicits(t *testing.T) {
	for _, ex := range implicits {
		evalString(t, ex.source, ex.result)
	}
}

func TestImplicitRoundTrips(t *testing.T) {
	sources := []string{
		`max 3` + ordering + intOrd,
		`[max 3, x -> max x 3]` + ordering + intOrd,
		`max [1]` + ordering + intOrd + listOrd,
		`list/map (max [2]) [[1], [3]]` + ordering + intOrd + listOrd,
		`{ m = max }` + ordering,
	}
	for _, ex := range implicits {
		sources = append(sources, ex.source)
	}
	for _, source := range sources {
		if first, second, err := roundTrip(source); err != nil {
			t.Errorf("%s: %v", source, err)
		} else if first != second {
			t.Errorf("%s: printed %s, which prints %s", source, first, second)
		}
	}
}

var implicitFailures = []struct{ source, error string }{
	{`max 1 2` + ordering, `max has no instance of 'ord int' in scope`},
	{`max 1 2` + ordering + intOrd + `; _ : ord int = { cmp = int/cmp }`, `max has more than one instance of 'ord int' in scope`},
	{`max [1] [2]` + ordering + listOrd, `max has no instance of 'ord int' in scope`},
	// Instances are lexical: a binding sees only those around it.
	{`g ; g = (h ; _ : ord text = { cmp = text/cmp }) ; h = max "a" "b"` + ordering + intOrd, `max has no instance of 'ord text' in scope`},
	// A binding without a constraint can't choose between instances.
	{`f ; f = x -> y -> max x y` + ordering + intOrd + `; _ : ord text = { cmp = text/cmp }`, `max has more than one instance of 'ord a' in scope`},
	// Without its constraint, an annotated function can't use one.
	{`f ; f : a => a -> a -> a = x -> y -> max x y` + ordering, `max has no instance of 'ord a' in scope`},
	{`f ; f : a => ord a => a -> a = | x -> x` + ordering, `a constrained function takes its instance as its first parameter`},
	{`f ; f : (ord int => int) -> int = g -> 1` + ordering, `constraints must come first in an annotation`},
	{`_ ; _ : ord int = { cmp = int/cmp }` + ordering, `cannot use _ as a value`},
	// An instance that uses itself is a recursive value.
	{`max 1 2 ; _ : ord int = { cmp = x -> y -> cmp x y }` + ordering, `an instance is recursive through instances, so it must be a function`},
}

func TestImplicitFailures(t *testing.T) {
	for _, ex := range implicitFailures {
		evalFailure(t, ex.source, ex.error)
	}
}
