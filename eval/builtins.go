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

func bindBuiltIns(reg *types.Registry) (types.TypeScope, Variables) {
	var scope types.TypeScope
	var builtIns = make(Variables)

	define := func(name string, typ types.TypeRef, one func(Value) Value) {
		builtIns[name] = BuiltInFunc{name: name, arity: 1, one: one}
		scope = scope.Bind(name, typ)
	}
	define2 := func(name string, typ types.TypeRef, two func(Value, Value) Value) {
		builtIns[name] = BuiltInFunc{name: name, arity: 2, two: two}
		scope = scope.Bind(name, typ)
	}
	define3 := func(name string, typ types.TypeRef, three func(Value, Value, Value) Value) {
		builtIns[name] = BuiltInFunc{name: name, arity: 3, three: three}
		scope = scope.Bind(name, typ)
	}

	a := reg.Gen()
	b := reg.Gen()
	aToB := reg.Func(a, b)
	aList := reg.List(a)
	bList := reg.List(b)
	textList := reg.List(types.TextRef)

	// Enums that builtins return. Nullary tags are boxed once, so returning
	// them doesn't allocate.
	tag := func(name string) Value { return Variant{tag: reg.Labels().Of(name)} }
	ltID, gtID := reg.Labels().Of("lt"), reg.Labels().Of("gt")
	lt, eq, gt := tag("lt"), tag("eq"), tag("gt")
	order := func() types.TypeRef {
		return reg.Enum(map[string]types.TypeRef{"lt": types.HoleRef, "eq": types.HoleRef, "gt": types.HoleRef})
	}
	nothing, some := tag("none"), reg.Labels().Of("some")
	maybe := func(t types.TypeRef) types.TypeRef {
		return reg.Enum(map[string]types.TypeRef{"some": t, "none": types.HoleRef})
	}
	ordered := func(c int) Value {
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
	define("list/length", reg.Func(aList, types.IntRef), func(ls Value) Value {
		return Int(len(ls.(List).elements))
	})
	define2("list/map", reg.Func(aToB, reg.Func(aList, bList)), func(fn, list Value) Value {
		elems := list.(List).elements
		results := make([]Value, len(elems))
		for i, v := range elems {
			results[i] = apply(fn, v)
		}
		return List{results}
	})
	accum := reg.Func(a, reg.Func(b, a))
	define3("list/fold", reg.Func(a, reg.Func(accum, reg.Func(bList, a))), func(acc, fn, list Value) Value {
		for _, v := range list.(List).elements {
			acc = call(fn, acc, v)
		}
		return acc
	})
	define2("list/repeat", reg.Func(types.IntRef, reg.Func(a, aList)), func(n, v Value) Value {
		count := int(n.(Int))
		if count < 0 {
			panic(fail("list/repeat: negative count %d", count))
		}
		elems := make([]Value, count)
		for i := range elems {
			elems[i] = v
		}
		return List{elems}
	})

	// Sorts stably, by a comparison like int/cmp.
	define2("list/sort", reg.Func(reg.Func(a, reg.Func(a, order())), reg.Func(aList, aList)), func(fn, list Value) Value {
		elems := slices.Clone(list.(List).elements)
		slices.SortStableFunc(elems, func(x, y Value) int {
			switch call(fn, x, y).(Variant).tag {
			case ltID:
				return -1
			case gtID:
				return 1
			}
			return 0
		})
		return List{elems}
	})

	// Text
	define("text/length", reg.Func(types.TextRef, types.IntRef), func(text Value) Value {
		return Int(len(text.(Text)))
	})
	define2("text/repeat", reg.Func(types.IntRef, reg.Func(types.TextRef, types.TextRef)), func(n, text Value) Value {
		count := int(n.(Int))
		if count < 0 {
			panic(fail("text/repeat: negative count %d", count))
		}
		return Text(strings.Repeat(string(text.(Text)), count))
	})
	define2("text/join", reg.Func(types.TextRef, reg.Func(textList, types.TextRef)), func(sep, list Value) Value {
		elems := list.(List).elements
		texts := make([]string, len(elems))
		for i, v := range elems {
			texts[i] = string(v.(Text))
		}
		return Text(strings.Join(texts, string(sep.(Text))))
	})
	define2("text/cmp", reg.Func(types.TextRef, reg.Func(types.TextRef, order())), func(a, b Value) Value {
		return ordered(strings.Compare(string(a.(Text)), string(b.(Text))))
	})
	// Splits the text around each separator; an empty one splits it into
	// its characters.
	define2("text/split", reg.Func(types.TextRef, reg.Func(types.TextRef, textList)), func(sep, text Value) Value {
		parts := strings.Split(string(text.(Text)), string(sep.(Text)))
		elems := make([]Value, len(parts))
		for i, part := range parts {
			elems[i] = Text(part)
		}
		return List{elems}
	})
	define("text/to-int", reg.Func(types.TextRef, maybe(types.IntRef)), func(text Value) Value {
		i, err := strconv.ParseInt(string(text.(Text)), 10, 64)
		if err != nil {
			return nothing
		}
		return Variant{tag: some, value: Int(i)}
	})

	// Ints
	define2("int/cmp", reg.Func(types.IntRef, reg.Func(types.IntRef, order())), func(a, b Value) Value {
		return ordered(cmp.Compare(a.(Int), b.(Int)))
	})
	// Division truncates towards zero, as in Go.
	define2("int/div", reg.Func(types.IntRef, reg.Func(types.IntRef, maybe(types.IntRef))), func(a, b Value) Value {
		if b.(Int) == 0 {
			return nothing
		}
		return Variant{tag: some, value: a.(Int) / b.(Int)}
	})
	define("int/to-text", reg.Func(types.IntRef, types.TextRef), func(i Value) Value {
		return Text(strconv.FormatInt(int64(i.(Int)), 10))
	})

	// int -> float
	define("to-float", reg.Func(types.IntRef, types.FloatRef), func(i Value) Value {
		return Float(i.(Int))
	})

	// float -> int
	floatToInt := reg.Func(types.FloatRef, types.IntRef)
	define("round", floatToInt, roundFunc(math.Round))
	define("ceil", floatToInt, roundFunc(math.Ceil))
	define("floor", floatToInt, roundFunc(math.Floor))

	// bytes <-> text
	define("bytes/to-utf8-text", reg.Func(types.BytesRef, types.TextRef), func(v Value) Value {
		bytes := v.(Bytes)
		if !utf8.Valid(bytes) {
			// Bytes print without labels, so without a table.
			panic(fail("bytes/to-utf8-text: %s is not valid UTF-8", printValue(nil, bytes)))
		}
		return Text(bytes)
	})
	define("bytes/from-utf8-text", reg.Func(types.TextRef, types.BytesRef), func(text Value) Value {
		return Bytes(text.(Text))
	})

	return scope, builtIns
}

func roundFunc(round func(float64) float64) func(Value) Value {
	return func(f Value) Value {
		return Int(round(float64(f.(Float))))
	}
}

// fail returns a builtin's failure, for it to panic with.
func fail(format string, args ...any) runtimeError {
	return runtimeError{fmt.Errorf(format, args...)}
}
