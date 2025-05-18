package scrapscript

// Here to get `go test ./...` to work.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
		bs, ok := v.(eval.Bytes)
		if !ok {
			return nil, fmt.Errorf("cannot import non-bytes %s", v)
		}

		// Must convert from `eval.Byte` to `[]byte`.
		hash := []byte(bs)

		// Funnily enough; any lower-cased, hex-encoded sha256 hash can be parsed
		// as base64. Users reading the official documentation at
		// https://scrapscript.org/guide may be frustrated if this doesn't work.
		// We detect this and convert back via base64 to the original hex string.
		var err error
		if len(hash) == sha256AsBase64Size {
			hash, err = rescueSha256FromBase64(hash)
			if err != nil {
				return nil, err
			}
		}

		if len(hash) != sha256.Size {
			return nil, fmt.Errorf("cannot import sha256 bytes of length %d, must be %d", len(hash), sha256.Size)
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

const sha256AsBase64Size = 48

func rescueSha256FromBase64(encoded []byte) ([]byte, error) {
	return hex.DecodeString(base64.StdEncoding.EncodeToString(encoded))
}
