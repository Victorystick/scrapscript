package eval

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/Victorystick/scrapscript/parser"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/types"
	"github.com/Victorystick/scrapscript/yards"
)

type Environment struct {
	reg  types.Registry
	vars Variables
}

func NewEnvironment(fetcher yards.Fetcher) *Environment {
	env := &Environment{}
	env.vars = bindBuiltIns(&env.reg)

	if fetcher != nil {
		// TODO: Don't inline this. :/
		env.vars["$sha256"] = BuiltInFunc{
			name: "$sha256",
			// We must special-case import functions, since their type is dependent
			// on their returned value.
			typ: env.reg.Func(types.BytesRef, types.NeverRef),
			fn: func(v Value) (Value, error) {
				bs, ok := v.(Bytes)
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

				return env.Eval(bytes)
			},
		}
	}

	return env
}

const sha256AsBase64Size = 48

func rescueSha256FromBase64(encoded []byte) ([]byte, error) {
	return hex.DecodeString(base64.StdEncoding.EncodeToString(encoded))
}

func (e *Environment) Eval(script []byte) (Value, error) {
	src := token.NewSource(script)
	se, err := parser.Parse(&src)

	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	return Eval(se, &e.reg, e.vars)
}

// Scrap renders a Value as self-contained scrapscript program.
func (e *Environment) Scrap(value Value) string {
	if vr, ok := value.(Variant); ok {
		if vr.value == nil {
			return fmt.Sprintf("(%s)::%s", e.reg.String(vr.typ), vr.tag)
		}
		return fmt.Sprintf("(%s)::%s %s", e.reg.String(vr.typ), vr.tag, e.Scrap(vr.value))
	}
	return value.String()
}
