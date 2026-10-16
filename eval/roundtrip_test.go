package eval

import "testing"

// Sources whose results are known to break SPEC.md section 10, rule 6: a
// printed value must evaluate to the same value, which prints identically.
// TestRoundTrip checks that each still breaks, so a fix must remove it here.
var brokenRoundTrips = map[string]string{
	// The bindings print as one where-group, in which r is recursive.
	`f ; f = (| 0 -> 0 | n -> r.k (n - 1) ; r = { k = m -> f m })`: "r is recursive, so it must be a function",
}

// More sources to round-trip, beyond those of the other eval tests.
var roundTrips = []string{
	// Closures of one lambda print with their own captures.
	`list/map mk [1, 2] ; mk = n -> (f ; f = x -> x + n)`,
	`list/fold (y -> y) (acc -> x -> (f ; f = y -> acc y + x)) [1, 2, 3]`,
	`[ f, g ] ; f = x -> x >> g ; g = (y -> y) << (z -> z)`,
	`{ a = 1, b = [#x, #y 2] }`,
	`#a { b = ~ff, c = ~~aGVsbG8gd29ybGQ= }`,
	`-3 - 5`,
	`[ 1.5, 2.0 ]`,
	`()`,
	`{}`,
	`x -> x`,
	`text/join " "`,
	`"a\nb\t\"q\" \\ \u{0}\u{e9}"`,
	`#some (#some -1)`,
	`#some (x -> x)`,
	`#fun (x -> x * 2)`,
	`list/fold 0 (a -> b -> a + b)`,
	`list/map (x -> x + n) ; n = 3`,
	`3 |> a -> b -> a`,
	`f ; f = | 0 -> 1 | n -> n * f (n - 1)`,
	`even ; even = | 0 -> #true | n -> odd (n - 1) ; odd = | 0 -> #false | n -> even (n - 1)`,
	`(n -> (f ; f = | 0 -> n | k -> f (k - 1))) 5`,
	`(x -> | 0 -> x | n -> n) 7`,
	`f >> g ; f = x -> x + 1 ; g = x -> x * 2`,
	`[ f, g ] ; f = x -> g x ; g = x -> x`,
	`{ f = (a -> b -> a) 1, g = (a -> b -> a) 2 }`,
	`{ f = (a -> b -> a) 1, g = (a -> a' -> a) 2 }`,
	`(a -> b -> a) (x -> y) ; y = 1`,
	`(a -> (x -> f x ; f = y -> a + y ; a = 1)) 2`,
	`get ; get : pair -> int = p -> p.a ; pair : type = { a : int, b : int }`,
	`{ a = m, b = list/map } ; m = (list/map ; list/map = x -> x)`,
	// A recursive function over a recursive type needs its annotation, and
	// the sort binding that it refers to.
	`size ; size : tree -> int = | #leaf -> 0 | #node n -> 1 + size n.left + size n.right ` +
		`; tree : type = [#leaf, #node { left : tree, right : tree }]`,
}

// Printed results, where their form matters.
var printed = []struct{ source, result string }{
	// A function applied to some of its arguments prints applied.
	{`3 |> a -> b -> a`, `(a -> b -> a) 3`},
	{`f 1 ; f = a -> b -> c -> a + b + c`, `f 1
; f = a -> b -> c -> a + b + c`},
	// A lambda in a where-clause is a function of its own.
	{`3 |> a -> (f ; f = b -> a)`, "f\n; f = b -> a\n; a = 3"},
	// A recursive function prints as its name, bound to its code.
	{`[ f ] ; f = | 0 -> 1 | n -> n * f (n - 1)`, `[ f ]
; f =
  | 0 -> 1
  | n -> n * f (n - 1)`},
	// A match function applied to some of its arguments prints applied.
	{`f 1 ; f = | 1 -> 1 -> 1 | a -> b -> a + b`, `f 1
; f =
  | 1 -> 1 -> 1
  | a -> b -> a + b`},
	{`(| 1 -> b -> b | a -> _ -> a) 2`, `(| 1 -> b -> b | a -> _ -> a) 2`},
	{`#some (f [1] ; f = | [] -> _ -> 0 | _ -> b -> b)`, `#some (f [ 1 ])
; f =
  | [] -> _ -> 0
  | _ -> b -> b`},
	// Bindings from different scopes with the same name get primes, which
	// avoid the names that the code binds.
	{`{ f = (a -> (h ; h = b -> a)) 1, g = (a -> (h ; h = a' -> a)) 2 }`, `{ f = h, g = h' }
; h = b -> a
; a = 1
; h' = a' -> a''
; a'' = 2`},
	// Annotations, and the sort bindings that they refer to, are kept.
	{`get ; get : pair -> int = p -> p.a ; pair : type = { a : int, b : int } ; unused = 1`, `get
; get : pair -> int = p -> p.a
; pair : type = { a : int, b : int }`},
	// Builtins print by name, so a binding that shadows one is renamed.
	{`{ a = text/join, b = list/map (x -> x) }`, `{ a = text/join, b = list/map (x -> x) }`},
	{`{ a = m, b = list/map } ; m = (list/map ; list/map = x -> x)`, `{ a = list/map', b = list/map }
; list/map' = x -> x`},
}

func TestPrinted(t *testing.T) {
	for _, ex := range printed {
		evalString(t, ex.source, ex.result)
	}
}

func TestRoundTrip(t *testing.T) {
	var sources []string
	for _, ex := range expressions {
		sources = append(sources, ex.source)
	}
	for _, ex := range exp2str {
		sources = append(sources, ex.source)
	}
	sources = append(sources, roundTrips...)

	for _, source := range sources {
		if _, ok := brokenRoundTrips[source]; ok {
			continue
		}
		if first, second, err := roundTrip(source); err != nil {
			t.Errorf("%s: %v", source, err)
		} else if first != second {
			t.Errorf("%s: printed %s, which prints %s", source, first, second)
		}
	}

	for source := range brokenRoundTrips {
		if first, second, err := roundTrip(source); err == nil && first == second {
			t.Errorf("%s now round-trips; remove it from brokenRoundTrips", source)
		}
	}
}

// roundTrip evaluates source, then evaluates the printed result again. It
// returns both printed results, or the first error.
func roundTrip(source string) (first, second string, err error) {
	env := NewEnvironment()
	val, err := eval(env, source)
	if err != nil {
		return "", "", err
	}
	first = env.Print(val)
	env = NewEnvironment()
	val, err = eval(env, first)
	if err != nil {
		return first, "", err
	}
	return first, env.Print(val), nil
}

func TestPrintedImports(t *testing.T) {
	const add = "$source:a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445"
	examples := []struct{ source, result string }{
		// A published function prints as the reference to it.
		{`{ f = ` + add + ` }`, `{ f = ` + add + ` }`},
		// Applying it gives a function of its code, with what it captures.
		{add + ` 1`, add + ` 1`},
		// Code that refers to it keeps the reference.
		{`x -> ` + add + ` x`, `x -> ` + add + ` x`},
	}
	for _, ex := range examples {
		env := NewEnvironment()
		env.UseFetcher(MapFetcher{add[len("$source:"):]: `a -> b -> a + b`})
		val, err := eval(env, ex.source)
		if err != nil {
			t.Errorf("%s: %v", ex.source, err)
		} else if got := env.Print(val); got != ex.result {
			t.Errorf("%s: expected %q, got %q", ex.source, ex.result, got)
		}
	}
}
