package eval

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Victorystick/scrapscript/types"
)

func bindBuiltIns(reg *types.Registry) (types.TypeScope, variables) {
	var scope types.TypeScope
	var builtIns = make(variables)

	define := func(name string, typ types.TypeRef, one func(value) value) {
		builtIns[name] = builtinVal(&builtinFunc{name: name, arity: 1, one: one})
		scope = scope.Bind(name, typ)
	}
	define2 := func(name string, typ types.TypeRef, two func(value, value) value) {
		builtIns[name] = builtinVal(&builtinFunc{name: name, arity: 2, two: two})
		scope = scope.Bind(name, typ)
	}
	define3 := func(name string, typ types.TypeRef, three func(value, value, value) value) {
		builtIns[name] = builtinVal(&builtinFunc{name: name, arity: 3, three: three})
		scope = scope.Bind(name, typ)
	}

	a := reg.Gen()
	b := reg.Gen()
	aToB := reg.Func(a, b)
	aList := reg.List(a)
	bList := reg.List(b)
	textList := reg.List(types.TextRef)

	// Enums that builtins return.
	tag := func(name string) value { return tagVal(reg.Labels().Of(name)) }
	ltID, gtID := reg.Labels().Of("lt"), reg.Labels().Of("gt")
	lt, eq, gt := tag("lt"), tag("eq"), tag("gt")
	order := func() types.TypeRef {
		return reg.Enum(map[string]types.TypeRef{"lt": types.HoleRef, "eq": types.HoleRef, "gt": types.HoleRef})
	}
	nothing, some := tag("none"), reg.Labels().Of("some")
	maybe := func(t types.TypeRef) types.TypeRef {
		return reg.Enum(map[string]types.TypeRef{"some": t, "none": types.HoleRef})
	}
	ordered := func(c int) value {
		switch {
		case c < 0:
			return lt
		case c > 0:
			return gt
		}
		return eq
	}

	// The checker guarantees the arguments' types, so builtins only fail on
	// their values.

	// Lists
	define("list/length", reg.Func(aList, types.IntRef), func(ls value) value {
		return intVal(len(ls.asList()))
	})
	define2("list/map", reg.Func(aToB, reg.Func(aList, bList)), func(fn, list value) value {
		elems := list.asList()
		results := make([]value, len(elems))
		for i, v := range elems {
			results[i] = apply(fn, v)
		}
		return listVal(results)
	})
	accum := reg.Func(a, reg.Func(b, a))
	define3("list/fold", reg.Func(a, reg.Func(accum, reg.Func(bList, a))), func(acc, fn, list value) value {
		for _, v := range list.asList() {
			acc = call2(fn, acc, v)
		}
		return acc
	})
	define2("list/repeat", reg.Func(types.IntRef, reg.Func(a, aList)), func(n, v value) value {
		count := n.asInt()
		if count < 0 {
			panic(fail("list/repeat: negative count %d", count))
		}
		elems := make([]value, count)
		for i := range elems {
			elems[i] = v
		}
		return listVal(elems)
	})

	// Sorts stably, by a comparison like int/cmp.
	define2("list/sort", reg.Func(reg.Func(a, reg.Func(a, order())), reg.Func(aList, aList)), func(fn, list value) value {
		elems := slices.Clone(list.asList())
		slices.SortStableFunc(elems, func(x, y value) int {
			switch call2(fn, x, y).tag() {
			case ltID:
				return -1
			case gtID:
				return 1
			}
			return 0
		})
		return listVal(elems)
	})

	// Text
	define("text/length", reg.Func(types.TextRef, types.IntRef), func(text value) value {
		return intVal(len(text.asText()))
	})
	define2("text/repeat", reg.Func(types.IntRef, reg.Func(types.TextRef, types.TextRef)), func(n, text value) value {
		count := n.asInt()
		if count < 0 {
			panic(fail("text/repeat: negative count %d", count))
		}
		return textVal(strings.Repeat(text.asText(), count))
	})
	define2("text/join", reg.Func(types.TextRef, reg.Func(textList, types.TextRef)), func(sep, list value) value {
		elems := list.asList()
		texts := make([]string, len(elems))
		for i, v := range elems {
			texts[i] = v.asText()
		}
		return textVal(strings.Join(texts, sep.asText()))
	})
	define2("text/cmp", reg.Func(types.TextRef, reg.Func(types.TextRef, order())), func(a, b value) value {
		return ordered(strings.Compare(a.asText(), b.asText()))
	})
	// Splits the text around each separator; an empty one splits it into
	// its characters.
	define2("text/split", reg.Func(types.TextRef, reg.Func(types.TextRef, textList)), func(sep, text value) value {
		parts := strings.Split(text.asText(), sep.asText())
		elems := make([]value, len(parts))
		for i, part := range parts {
			elems[i] = textVal(part)
		}
		return listVal(elems)
	})
	define("text/to-int", reg.Func(types.TextRef, maybe(types.IntRef)), func(text value) value {
		i, err := strconv.ParseInt(text.asText(), 10, 64)
		if err != nil {
			return nothing
		}
		return variantVal(some, intVal(int(i)))
	})

	// Ints
	define2("int/cmp", reg.Func(types.IntRef, reg.Func(types.IntRef, order())), func(a, b value) value {
		return ordered(cmp.Compare(a.asInt(), b.asInt()))
	})
	// Division truncates towards zero, as in Go.
	define2("int/div", reg.Func(types.IntRef, reg.Func(types.IntRef, maybe(types.IntRef))), func(a, b value) value {
		if b.asInt() == 0 {
			return nothing
		}
		return variantVal(some, intVal(a.asInt()/b.asInt()))
	})
	define("int/to-text", reg.Func(types.IntRef, types.TextRef), func(i value) value {
		return textVal(strconv.Itoa(i.asInt()))
	})

	// int -> float
	define("to-float", reg.Func(types.IntRef, types.FloatRef), func(i value) value {
		return floatVal(float64(i.asInt()))
	})

	// float -> int
	floatToInt := reg.Func(types.FloatRef, types.IntRef)
	define("round", floatToInt, roundFunc(math.Round))
	define("ceil", floatToInt, roundFunc(math.Ceil))
	define("floor", floatToInt, roundFunc(math.Floor))

	// bytes <-> text
	define("bytes/to-utf8-text", reg.Func(types.BytesRef, types.TextRef), func(v value) value {
		if !utf8.Valid(v.asBytes()) {
			// Bytes print without labels, so without a table.
			panic(fail("bytes/to-utf8-text: %s is not valid UTF-8", printValue(nil, v)))
		}
		return textVal(string(v.asBytes()))
	})
	define("bytes/from-utf8-text", reg.Func(types.TextRef, types.BytesRef), func(text value) value {
		return bytesVal([]byte(text.asText()))
	})

	return scope, builtIns
}

func roundFunc(round func(float64) float64) func(value) value {
	return func(f value) value {
		return intVal(int(round(f.asFloat())))
	}
}

// fail returns a builtin's failure, for it to panic with.
func fail(format string, args ...any) runtimeError {
	return runtimeError{fmt.Errorf(format, args...)}
}
