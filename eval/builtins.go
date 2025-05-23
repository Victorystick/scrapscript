package eval

import (
	"fmt"
	"math"
)

var builtIns = make(Variables)

func builtin(name string, val Value) {
	builtIns[name] = val
}

func define(name string, val Func) {
	builtin(name, BuiltInFunc{name, val})
}

func init() {
	// type validators
	define("int", func(val Value) (Value, error) {
		if _, ok := val.(Int); !ok {
			return nil, fmt.Errorf("passed %T where int was expected", val)
		}
		return val, nil
	})
	define("float", func(val Value) (Value, error) {
		if _, ok := val.(Float); !ok {
			return nil, fmt.Errorf("passed %T where float was expected", val)
		}
		return val, nil
	})
	define("text", func(val Value) (Value, error) {
		if _, ok := val.(Text); !ok {
			return nil, fmt.Errorf("passed %T where text was expected", val)
		}
		return val, nil
	})
	define("byte", func(val Value) (Value, error) {
		if _, ok := val.(Byte); !ok {
			return nil, fmt.Errorf("passed %T where byte was expected", val)
		}
		return val, nil
	})
	define("bytes", func(val Value) (Value, error) {
		if _, ok := val.(Bytes); !ok {
			return nil, fmt.Errorf("passed %T where bytes was expected", val)
		}
		return val, nil
	})

	// Lists
	define("list/map", func(val Value) (Value, error) {
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

				results := make(List, len(ls))
				for i, v := range ls {
					results[i], err = fn(v)
					if err != nil {
						return nil, err
					}
				}
				return List(results), nil
			},
		}, nil
	})
	define("list/fold", func(acc Value) (Value, error) {
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
						for _, v := range ls {
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
	define("text/length", func(val Value) (Value, error) {
		text, ok := val.(Text)
		if !ok {
			return nil, fmt.Errorf("expected text, but got %T", val)
		}
		return Int(len(text)), nil
	})

	// int -> float
	define("to-float", func(val Value) (Value, error) {
		if i, ok := val.(Int); ok {
			return Float(float64(i)), nil
		}
		return Int(0), fmt.Errorf("non-int value %T", val)
	})

	// float -> int
	define("round", floatToInt(math.Round))
	define("ceil", floatToInt(math.Ceil))
	define("floor", floatToInt(math.Floor))

	// bytes <-> text
	define("bytes/to-utf8-text", func(val Value) (Value, error) {
		if bytes, ok := val.(Bytes); ok {
			return Text(string([]byte(bytes))), nil
		}
		return nil, fmt.Errorf("cannot bytes/to-utf8-text on %T", val)
	})
	define("bytes/from-utf8-text", func(val Value) (Value, error) {
		if text, ok := val.(Text); ok {
			return Bytes(text), nil
		}
		return nil, fmt.Errorf("cannot bytes/from-utf8-text on %T", val)
	})
}

func floatToInt(round func(float64) float64) Func {
	return func(val Value) (Value, error) {
		if f, ok := val.(Float); ok {
			return Int(round(float64(f))), nil
		}
		return Int(0), fmt.Errorf("non-float value %T", val)
	}
}
