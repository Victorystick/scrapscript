package scrapscript

import (
	"errors"
	"fmt"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/yards"
)

var ErrEmptyScript = errors.New("empty script")

func Eval(script []byte, fetcher yards.Fetcher) (eval.Value, error) {
	if len(script) == 0 {
		return nil, ErrEmptyScript
	}

	return eval.NewEnvironment(fetcher).Eval(script)
}

func Call(toCall, val eval.Value) (eval.Value, error) {
	fn := eval.Callable(toCall)
	if fn != nil {
		return fn(val)
	}
	return nil, fmt.Errorf("non-func value %s", toCall)
}
