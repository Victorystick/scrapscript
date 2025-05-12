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

	// bytes -> text
	define("bytes-to-utf8-text", func(val Value) (Value, error) {
		if bytes, ok := val.(Bytes); ok {
			return Text(string([]byte(bytes))), nil
		}
		return nil, fmt.Errorf("cannot bytes-to-utf8-text on %T", val)
	})
}

func floatToInt(round func(float64) float64) Func {
	return func(val Value) (Value, error) {
		if f, ok := val.(Float); ok {
			return Float(round(float64(f))), nil
		}
		return Float(0), fmt.Errorf("non-float value %T", val)
	}
}
