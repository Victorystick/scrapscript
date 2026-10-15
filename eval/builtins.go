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

	define := func(name string, typ types.TypeRef, val func(Value) Value) {
		builtIns[name] = BuiltInFunc{name, val}
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
	define("list/map", reg.Func(aToB, reg.Func(aList, bList)), func(fn Value) Value {
		return Partial{
			name: "list/map", args: []Value{fn},
			fn: func(ls Value) Value {
				elems := ls.(List).elements
				results := make([]Value, len(elems))
				for i, v := range elems {
					results[i] = apply(fn, v)
				}
				return List{results}
			},
		}
	})
	accum := reg.Func(a, reg.Func(b, a))
	define("list/fold", reg.Func(a, reg.Func(accum, reg.Func(bList, a))), func(init Value) Value {
		return Partial{
			name: "list/fold", args: []Value{init},
			fn: func(fn Value) Value {
				return Partial{
					name: "list/fold", args: []Value{init, fn},
					fn: func(ls Value) Value {
						acc := init
						for _, v := range ls.(List).elements {
							acc = apply(apply(fn, acc), v)
						}
						return acc
					},
				}
			},
		}
	})
	define("list/repeat", reg.Func(types.IntRef, reg.Func(a, aList)), func(n Value) Value {
		count := int(n.(Int))
		if count < 0 {
			panic(fail("list/repeat: negative count %d", count))
		}
		return Partial{
			name: "list/repeat", args: []Value{n},
			fn: func(v Value) Value {
				elems := make([]Value, count)
				for i := range elems {
					elems[i] = v
				}
				return List{elems}
			},
		}
	})

	// Text
	define("text/length", reg.Func(types.TextRef, types.IntRef), func(text Value) Value {
		return Int(len(text.(Text)))
	})
	define("text/repeat", reg.Func(types.IntRef, reg.Func(types.TextRef, types.TextRef)), func(n Value) Value {
		count := int(n.(Int))
		if count < 0 {
			panic(fail("text/repeat: negative count %d", count))
		}
		return Partial{
			name: "text/repeat", args: []Value{n},
			fn: func(text Value) Value {
				return Text(strings.Repeat(string(text.(Text)), count))
			},
		}
	})
	define("text/join", reg.Func(types.TextRef, reg.Func(textList, types.TextRef)), func(sep Value) Value {
		return Partial{
			name: "text/join", args: []Value{sep},
			fn: func(ls Value) Value {
				elems := ls.(List).elements
				texts := make([]string, len(elems))
				for i, v := range elems {
					texts[i] = string(v.(Text))
				}
				return Text(strings.Join(texts, string(sep.(Text))))
			},
		}
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
