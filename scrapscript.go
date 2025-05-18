package scrapscript

// Here to get `go test ./...` to work.

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/parser"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/yards"
)

var ErrEmptyScript = errors.New("empty script")

func Eval(script []byte, fetcher yards.Fetcher) (eval.Value, error) {
	if len(script) == 0 {
		return nil, ErrEmptyScript
	}

	src := token.NewSource(script)
	se, err := parser.Parse(&src)

	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	val, err := eval.Eval(se, WithImports(fetcher))
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

func WithImports(fetcher yards.Fetcher) eval.Vars {
	binding := eval.FuncBinding("$sha256", func(v eval.Value) (eval.Value, error) {
		hash, ok := v.(eval.Bytes)
		if !ok || len(hash) != sha256.Size {
			return nil, fmt.Errorf("cannot import non-sha256 bytes %s", v)
		}

		key := fmt.Sprintf("%x", hash)
		bytes, err := fetcher.FetchSha256(key)
		if err != nil {
			return nil, err
		}

		return Eval(bytes, fetcher)
	})

	return binding
}
