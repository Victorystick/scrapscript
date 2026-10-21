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

type variables map[string]value

var ErrNoFloatMatch = errors.New("cannot match on floats")

func literal(src *ast.SourceExpr, x ast.Node) (value, error) {
	t := src.Main(x)
	switch kind := src.LiteralKind(x); kind {
	case token.HOLE:
		return hole, nil
	case token.INT:
		i, err := strconv.Atoi(src.TokenText(t))
		if err != nil {
			return value{}, err
		}
		return intVal(i), nil
	case token.FLOAT:
		f, err := strconv.ParseFloat(src.TokenText(t), 64)
		if err != nil {
			return value{}, err
		}
		return floatVal(f), nil
	case token.TEXT:
		return textVal(scanner.Unquote(src.TokenText(t))), nil
	case token.BYTES:
		str := src.Source.GetString(src.TokenSpan(t).TrimStart(2))
		dst := make([]byte, base64.StdEncoding.DecodedLen(len(str)))
		n, err := base64.StdEncoding.Decode(dst, []byte(str))
		if err != nil {
			return value{}, err
		}
		return bytesVal(dst[:n]), nil
	case token.BYTE:
		val, err := strconv.ParseUint(src.Source.GetString(src.TokenSpan(t).TrimStart(1)), 16, 8)
		if err != nil {
			return value{}, err
		}
		return byteVal(byte(val)), nil
	default:
		return value{}, src.Error(x, fmt.Sprintf("unhandled literal kind %s", kind))
	}
}
