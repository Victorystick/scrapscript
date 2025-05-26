package scrapscript

import (
	"fmt"

	"github.com/Victorystick/scrapscript/eval"
)

func Call(toCall, val eval.Value) (eval.Value, error) {
	fn := eval.Callable(toCall)
	if fn != nil {
		return fn(val)
	}
	return nil, fmt.Errorf("non-func value %s", toCall)
}
