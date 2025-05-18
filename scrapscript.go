package scrapscript

// Here to get `go test ./...` to work.

import (
	"errors"
	"fmt"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/parser"
	"github.com/Victorystick/scrapscript/token"
)

var ErrEmptyScript = errors.New("empty script")

func Eval(script []byte) (eval.Value, error) {
	if len(script) == 0 {
		return nil, ErrEmptyScript
	}

	src := token.NewSource(script)
	expr, err := parser.Parse(&src)

	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	val, err := eval.Eval(src, expr)
	if err != nil {
		return nil, fmt.Errorf("evaluation error: %w", err)
	}

	return val, nil
}

func Call(toCall, val eval.Value) (eval.Value, error) {
	fn := eval.Callable(toCall)
	if fn != nil {
		return fn(val)
	}
	return nil, fmt.Errorf("non-func value %s", toCall)
}
