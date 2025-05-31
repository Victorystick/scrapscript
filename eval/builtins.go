package eval

import (
	"fmt"
	"math"

	"github.com/Victorystick/scrapscript/types"
)

func bindBuiltIns(reg *types.Registry) Variables {
	var builtIns = make(Variables)

	define := func(name string, typ types.TypeRef, val Func) {
		builtIns[name] = BuiltInFunc{name, typ, val}
	}

	// Built-in types
	builtIns["()"] = Type(types.HoleRef)
	builtIns["int"] = Type(types.IntRef)
	builtIns["float"] = Type(types.FloatRef)
	builtIns["text"] = Type(types.TextRef)
	builtIns["byte"] = Type(types.ByteRef)
	builtIns["bytes"] = Type(types.BytesRef)

	a := reg.Unbound()
	b := reg.Unbound()
	aToB := reg.Func(a, b)
	aList := reg.List(a)
	bList := reg.List(b)

	// Lists
	define("list/map", reg.Func(aToB, reg.Func(aList, bList)), func(val Value) (Value, error) {
		fn := Callable(val)
		if fn == nil {
			// TODO: need more context to give better error messages.
			return nil, fmt.Errorf("needed function, but got %T", val)
		}
		return ScriptFunc{
			source: "list/map " + val.String(),
			fn: func(val Value) (v Value, err error) {
				ls, ok := val.(List)
				if !ok {
					return nil, fmt.Errorf("expected list, but got %T", val)
				}

				results := List{elements: make([]Value, len(ls.elements))}
				for i, v := range ls.elements {
					val, err = fn(v)
					if err != nil {
						return nil, err
					}
					results.elements[i] = val
					// TODO: propagate the new type.
				}
				return results, nil
			},
		}, nil
	})
	// TODO: type
	define("list/fold", types.NeverRef, func(acc Value) (Value, error) {
		source := "list/fold " + acc.String()
		return ScriptFunc{
			source: source,
			fn: func(val Value) (Value, error) {
				fn := Callable(val)
				if fn == nil {
					// TODO: need more context to give better error messages.
					return nil, fmt.Errorf("needed function, but got %T", val)
				}
				return ScriptFunc{
					source: source + " " + val.String(),
					fn: func(val Value) (res Value, err error) {
						ls, ok := val.(List)
						if !ok {
							return nil, fmt.Errorf("expected list, but got %T", val)
						}
						var mid Value
						for _, v := range ls.elements {
							mid, err = fn(acc)
							if err != nil {
								return nil, err
							}
							fn2 := Callable(mid)
							if fn2 == nil {
								// TODO: need more context to give better error messages.
								return nil, fmt.Errorf("needed function, but got %T", val)
							}
							acc, err = fn2(v)
							if err != nil {
								return nil, err
							}
						}
						return acc, nil
					},
				}, nil
			},
		}, nil
	})

	// Text
	define("text/length", reg.Func(types.TextRef, types.IntRef), func(val Value) (Value, error) {
		text, ok := val.(Text)
		if !ok {
			return nil, fmt.Errorf("expected text, but got %T", val)
		}
		return Int(len(text)), nil
	})

	// int -> float
	define("to-float", reg.Func(types.IntRef, types.FloatRef), func(val Value) (Value, error) {
		if i, ok := val.(Int); ok {
			return Float(float64(i)), nil
		}
		return Int(0), fmt.Errorf("non-int value %T", val)
	})

	// float -> int
	floatToInt := reg.Func(types.FloatRef, types.IntRef)
	define("round", floatToInt, roundFunc(math.Round))
	define("ceil", floatToInt, roundFunc(math.Ceil))
	define("floor", floatToInt, roundFunc(math.Floor))

	// bytes <-> text
	define("bytes/to-utf8-text", reg.Func(types.BytesRef, types.TextRef), func(val Value) (Value, error) {
		if bytes, ok := val.(Bytes); ok {
			return Text(string([]byte(bytes))), nil
		}
		return nil, fmt.Errorf("cannot bytes/to-utf8-text on %T", val)
	})
	define("bytes/from-utf8-text", reg.Func(types.TextRef, types.BytesRef), func(val Value) (Value, error) {
		if text, ok := val.(Text); ok {
			return Bytes(text), nil
		}
		return nil, fmt.Errorf("cannot bytes/from-utf8-text on %T", val)
	})

	return builtIns
}

func roundFunc(round func(float64) float64) Func {
	return func(val Value) (Value, error) {
		if f, ok := val.(Float); ok {
			return Int(round(float64(f))), nil
		}
		return Int(0), fmt.Errorf("non-float value %T", val)
	}
}
