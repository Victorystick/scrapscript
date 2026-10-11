package eval

import "testing"

// Sources whose results are known to break SPEC.md section 10, rule 9: a
// printed value must evaluate to the same value, which prints identically.
// TestRoundTrip checks that each still breaks, so a fix must remove it here.
var brokenRoundTrips = map[string]string{
	// Printing omits the parentheses the grammar needs.
	`#fun (x -> x * 2)`:             "prints #fun x -> x * 2",
	`list/fold 0 (a -> b -> a + b)`: "prints list/fold 0 a -> b -> a + b",
	// Functions don't print the bindings they capture.
	`3 |> a -> b -> a`: "prints b -> a, with a unbound",
	`f ; f = | 0 -> 1 | n -> n * f (n - 1)`: "prints 0 -> 1 | n -> n * f (n - 1, " +
		"without the leading | or closing ), and with f unbound",
	// The scanner doesn't decode escapes, so each round trip adds backslashes.
	`"a\nb"`: `prints "a\\nb", then "a\\\\nb"`,
	`"q\"q"`: "doesn't scan",
}

// More sources to round-trip, beyond those of the other eval tests.
var roundTrips = []string{
	`{ a = 1, b = [#x, #y 2] }`,
	`#a { b = ~ff, c = ~~aGVsbG8gd29ybGQ= }`,
	`-3 - 5`,
	`[ 1.5, 2.0 ]`,
	`()`,
	`x -> x`,
	`text/join " "`,
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
	val, err := eval(NewEnvironment(), source)
	if err != nil {
		return "", "", err
	}
	first = val.String()
	val, err = eval(NewEnvironment(), first)
	if err != nil {
		return first, "", err
	}
	return first, val.String(), nil
}
