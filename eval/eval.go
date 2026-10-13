package eval

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/scanner"
	"github.com/Victorystick/scrapscript/token"
)

// EvalImport returns the value of the scrap that an Import node refers to.
type EvalImport func(ref ast.Node) (Value, error)

type Vars interface {
	Get(name string) Value
}

type Variables map[string]Value

func (v Variables) Get(name string) Value {
	return v[name]
}

var ErrNoFloatMatch = errors.New("cannot match on floats")

func Literal(src *ast.SourceExpr, x ast.Node) (Value, error) {
	t := src.Main(x)
	switch kind := src.LiteralKind(x); kind {
	case token.HOLE:
		return Hole{}, nil
	case token.INT:
		i, err := strconv.Atoi(src.TokenText(t))
		if err != nil {
			return nil, err
		}
		return Int(i), nil
	case token.FLOAT:
		f, err := strconv.ParseFloat(src.TokenText(t), 64)
		if err != nil {
			return nil, err
		}
		return Float(f), nil
	case token.TEXT:
		return Text(scanner.Unquote(src.TokenText(t))), nil
	case token.BYTES:
		str := src.Source.GetString(src.TokenSpan(t).TrimStart(2))
		dst := make([]byte, base64.StdEncoding.DecodedLen(len(str)))
		n, err := base64.StdEncoding.Decode(dst, []byte(str))
		if err != nil {
			return nil, err
		}
		return Bytes(dst[:n]), nil
	case token.BYTE:
		val, err := strconv.ParseUint(src.Source.GetString(src.TokenSpan(t).TrimStart(1)), 16, 8)
		if err != nil {
			return nil, err
		}
		return Byte(byte(val)), nil
	default:
		return nil, src.Error(x, fmt.Sprintf("unhandled literal kind %s", kind))
	}
}
