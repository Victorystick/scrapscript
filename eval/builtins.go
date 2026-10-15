package eval

import (
	"fmt"
	"math"
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
	// defineN defines a builtin that takes arity arguments, more than one.
	defineN := func(name string, typ types.TypeRef, arity int, call func([]Value) Value) {
		builtIns[name] = BuiltInFunc{name: name, arity: arity, call: call}
		scope = scope.Bind(name, typ)
	}

	a := reg.Gen()
	b := reg.Gen()
	aToB := reg.Func(a, b)
	aList := reg.List(a)
	bList := reg.List(b)
	textList := reg.List(types.TextRef)

	// The checker guarantees the arguments' types, so builtins only fail on
	// their values.

	// Lists
	define("list/length", reg.Func(aList, types.IntRef), func(ls Value) Value {
		return Int(len(ls.(List).elements))
	})
	defineN("list/map", reg.Func(aToB, reg.Func(aList, bList)), 2, func(args []Value) Value {
		fn, elems := args[0], args[1].(List).elements
		results := make([]Value, len(elems))
		for i, v := range elems {
			results[i] = apply(fn, v)
		}
		return List{results}
	})
	accum := reg.Func(a, reg.Func(b, a))
	defineN("list/fold", reg.Func(a, reg.Func(accum, reg.Func(bList, a))), 3, func(args []Value) Value {
		acc, fn := args[0], args[1]
		for _, v := range args[2].(List).elements {
			acc = call(fn, acc, v)
		}
		return acc
	})
	defineN("list/repeat", reg.Func(types.IntRef, reg.Func(a, aList)), 2, func(args []Value) Value {
		count := int(args[0].(Int))
		if count < 0 {
			panic(fail("list/repeat: negative count %d", count))
		}
		elems := make([]Value, count)
		for i := range elems {
			elems[i] = args[1]
		}
		return List{elems}
	})

	// Text
	define("text/length", reg.Func(types.TextRef, types.IntRef), func(text Value) Value {
		return Int(len(text.(Text)))
	})
	defineN("text/repeat", reg.Func(types.IntRef, reg.Func(types.TextRef, types.TextRef)), 2, func(args []Value) Value {
		count := int(args[0].(Int))
		if count < 0 {
			panic(fail("text/repeat: negative count %d", count))
		}
		return Text(strings.Repeat(string(args[1].(Text)), count))
	})
	defineN("text/join", reg.Func(types.TextRef, reg.Func(textList, types.TextRef)), 2, func(args []Value) Value {
		elems := args[1].(List).elements
		texts := make([]string, len(elems))
		for i, v := range elems {
			texts[i] = string(v.(Text))
		}
		return Text(strings.Join(texts, string(args[0].(Text))))
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
