package eval

import (
	"encoding/base64"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/scanner"
	"github.com/Victorystick/scrapscript/token"
)

// EvalImport returns the value of the scrap that an Import node refers to.
type EvalImport func(ref ast.Node) (Value, error)

// A context is a frame of variables, in a chain of frames that follows the
// lexical scopes of a source.
type context struct {
	src        *ast.SourceExpr
	vars       Vars
	evalImport EvalImport
	parent     *context
	// The Where nodes of a where-group's frame, whose bindings are its vars.
	// Functions print with them.
	binds []ast.Node
}

type Vars interface {
	Get(name string) Value
}

type Variables map[string]Value

func (v Variables) Get(name string) Value {
	return v[name]
}

func (c *context) ident(x ast.Node) (Value, error) {
	name := c.name(c.src.Main(x))

	// Traverse the context stack.
	context := c
	for context != nil {
		val := context.vars.Get(name)
		if val != nil {
			return val, nil
		}
		context = context.parent
	}

	return nil, c.error(x, fmt.Sprintf("unknown variable %s", name))
}

func (c *context) name(t ast.Tok) string {
	return c.src.TokenText(t)
}

func (c *context) sub(vars Vars) *context {
	return &context{c.src, vars, c.evalImport, c, nil}
}

func (c *context) error(n ast.Node, msg string) error {
	return c.src.Error(n, msg)
}

func (c *context) tokenError(t ast.Tok, msg string) error {
	return c.src.TokenError(t, msg)
}

// Eval evaluates a SourceExpr in the context of a set of variables. The
// SourceExpr must have been type-checked.
func Eval(se ast.SourceExpr, vars Vars, evalImport EvalImport) (Value, error) {
	ctx := &context{&se, vars, evalImport, nil, nil}

	return ctx.eval(se.Expr)
}

func (c *context) eval(x ast.Node) (Value, error) {
	switch c.src.Kind(x) {
	case ast.Literal:
		return Literal(c.src, x)
	case ast.Ident:
		return c.ident(x)
	case ast.Binary:
		return c.binary(x)
	case ast.Call:
		fn, arg := c.src.Call(x)
		return c.call(fn, arg)
	case ast.Where:
		return c.where(x)
	case ast.Variant:
		tag, payload := c.src.Variant(x)
		v := Variant{tag: c.name(tag)}
		if payload != ast.None {
			val, err := c.eval(payload)
			if err != nil {
				return nil, err
			}
			v.value = val
		}
		return v, nil
	case ast.Record:
		return c.recordExpr(x)
	case ast.List:
		return c.listExpr(x)
	case ast.Func:
		return c.createFunc(x)
	case ast.MatchFunc:
		return c.createMatchFunc(x)
	case ast.Access:
		return c.access(x)
	case ast.Import:
		return c.evalImport(x)
	}

	return nil, c.error(x, fmt.Sprintf("unhandled node %s", c.src.Kind(x)))
}

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

func binop[T ~int | ~float64](t token.Token, a, b T) (T, error) {
	switch t {
	case token.ADD:
		return a + b, nil
	case token.SUB:
		return a - b, nil
	case token.MUL:
		return a * b, nil
	}

	return 0, fmt.Errorf("unhandled binop %s", t)
}

// binary evaluates a binary operator. Values are immutable, so appending
// clips the left operand first: append must copy rather than write into
// spare capacity that other values share.
func (c *context) binary(x ast.Node) (Value, error) {
	op, left, right := c.src.Binary(x)
	switch op {
	case token.ADD, token.SUB, token.MUL:
		l, err := c.eval(left)
		if err != nil {
			return nil, err
		}
		if lf, ok := l.(Float); ok {
			rf, err := c.float(right)
			if err != nil {
				return nil, err
			}
			return binop(op, lf, rf)
		}
		if lf, ok := l.(Int); ok {
			rf, err := c.int(right)
			if err != nil {
				return nil, err
			}
			return binop(op, lf, rf)
		}
		return nil, c.error(x, fmt.Sprintf("cannot perform arithmetic on %s", l))

	case token.APPEND:
		l, err := c.eval(left)
		if err != nil {
			return nil, err
		}

		if bs, ok := l.(Bytes); ok {
			r, err := c.byte(right)
			if err != nil {
				return nil, err
			}
			return append(slices.Clip(bs), byte(r)), nil
		}

		if ls, ok := l.(List); ok {
			r, err := c.eval(right)
			if err != nil {
				return nil, err
			}
			return List{append(slices.Clip(ls.elements), r)}, nil
		}

		return nil, fmt.Errorf("cannot append to non-list %s", reflect.TypeOf(l))

	case token.PREPEND:
		r, err := c.eval(right)
		if err != nil {
			return nil, err
		}

		if bs, ok := r.(Bytes); ok {
			l, err := c.byte(left)
			if err != nil {
				return nil, err
			}
			return append(Bytes{byte(l)}, bs...), nil
		}

		if ls, ok := r.(List); ok {
			l, err := c.eval(left)
			if err != nil {
				return nil, err
			}
			return List{append([]Value{l}, ls.elements...)}, nil
		}

		return nil, fmt.Errorf("cannot prepend to non-list %s", reflect.TypeOf(r))

	case token.CONCAT:
		l, err := c.eval(left)
		if err != nil {
			return nil, err
		}

		if bs, ok := l.(Bytes); ok {
			r, err := c.bytes(right)
			if err != nil {
				return nil, err
			}
			return append(slices.Clip(bs), r...), nil
		}

		if ls, ok := l.(List); ok {
			r, err := c.list(right)
			if err != nil {
				return nil, err
			}

			return List{append(slices.Clip(ls.elements), r.elements...)}, nil
		}

		if tx, ok := l.(Text); ok {
			r, err := c.text(right)
			if err != nil {
				return nil, err
			}
			return tx + r, nil
		}

		return nil, fmt.Errorf("cannot append to non-list %s", reflect.TypeOf(l))

	// Pipes are calls.
	case token.RPIPE:
		return c.call(right, left)
	case token.LPIPE:
		return c.call(left, right)

	case token.RCOMP: // a >> b
		return c.compose(x, left, right)
	case token.LCOMP: // a << b
		return c.compose(x, right, left)
	}

	return nil, c.error(x, fmt.Sprintf("unhandled %s operator", op))
}

func (c *context) call(fnX, argX ast.Node) (Value, error) {
	fn, err := c.fn(fnX)
	if err != nil {
		return nil, err
	}
	arg, err := c.eval(argX)
	if err != nil {
		return nil, err
	}
	return fn(arg)
}

// compose evaluates the composition x of two functions, first and second.
func (c *context) compose(x, first, second ast.Node) (Value, error) {
	a, err := c.fn(first)
	if err != nil {
		return nil, err
	}
	b, err := c.fn(second)
	if err != nil {
		return nil, err
	}
	return ScriptFunc{
		ctx:  c,
		node: x,
		fn: func(v Value) (Value, error) {
			mid, err := a(v)
			if err != nil {
				return nil, err
			}
			return b(mid)
		},
	}, nil
}

func (c *context) recordExpr(x ast.Node) (r Record, err error) {
	rest, fields := c.src.Record(x)

	var values map[string]Value
	if rest == ast.None {
		values = make(map[string]Value, fields.Len())
	} else {
		// A record based on another.
		var other Record
		other, err = c.record(rest)
		if err != nil {
			return
		}
		values = maps.Clone(other.values)
	}

	for i := range fields.Len() {
		var val Value
		val, err = c.eval(fields.Value(i))
		if err != nil {
			return
		}
		values[c.name(fields.Key(i))] = val
	}
	return Record{values}, nil
}

func (c *context) access(x ast.Node) (Value, error) {
	recX, keyTok := c.src.Access(x)
	r, err := c.record(recX)
	if err != nil {
		return nil, err
	}
	key := c.name(keyTok)
	val, ok := r.values[key]
	if !ok {
		return nil, c.tokenError(keyTok,
			fmt.Sprintf("record %s has no key %s", r, key))
	}
	return val, nil
}

func (c *context) listExpr(x ast.Node) (ls List, err error) {
	elems := c.src.List(x)
	elements := make([]Value, len(elems))
	for i, x := range elems {
		elements[i], err = c.eval(x)
		if err != nil {
			return
		}
	}
	return List{elements}, nil
}

func (c *context) createFunc(x ast.Node) (ScriptFunc, error) {
	arg, body := c.src.Func(x)
	if c.src.Kind(arg) != ast.Ident {
		return ScriptFunc{}, c.error(arg, "function parameter must be an identifier")
	}
	name := c.name(c.src.Main(arg))
	return ScriptFunc{
		ctx:  c,
		node: x,
		fn: func(value Value) (Value, error) {
			return c.sub(Variables{name: value}).eval(body)
		},
	}, nil
}

func (c *context) createMatchFunc(x ast.Node) (ScriptFunc, error) {
	return ScriptFunc{
		ctx:  c,
		node: x,
		fn: func(a Value) (Value, error) {
			for _, alt := range c.src.Arms(x) {
				arg, body := c.src.Func(alt)
				matches, err := Match(c.src, arg, a)
				if err != nil {
					if err == ErrNoMatch {
						continue
					}
					return nil, err
				}
				return c.sub(matches).eval(body)
			}
			return nil, c.error(x, fmt.Sprintf("no arm matches %s", a))
		},
	}, nil
}

// where evaluates a where-group, whose bindings are mutually recursive.
// Bindings are evaluated after the ones they refer to; functions refer to
// the group's bindings when called, so they can recurse.
func (c *context) where(x ast.Node) (Value, error) {
	body, binds := c.src.Group(x)
	vars := make(Variables, len(binds))
	sub := c.sub(vars)
	sub.binds = binds
	for _, comp := range c.src.Components(binds, nil) {
		for _, b := range comp.Binds {
			_, id, _, val := c.src.Where(b)
			v, err := sub.eval(val)
			if err != nil {
				return nil, err
			}
			vars[c.name(id)] = v
		}
	}
	return sub.eval(body)
}

// Evaluates a value, requiring a certain type.

func (c *context) fn(x ast.Node) (Func, error) {
	val, err := c.eval(x)
	if err != nil {
		return nil, err
	}
	fn := Callable(val)
	if fn != nil {
		return fn, nil
	}
	return nil, c.error(x, fmt.Sprintf("non-func value %s", val))
}

func (c *context) float(x ast.Node) (Float, error) {
	val, err := c.eval(x)
	if err != nil {
		return 0, err
	}
	if f, ok := val.(Float); ok {
		return f, nil
	}
	return 0, c.error(x, fmt.Sprintf("non-float value %s", val))
}

func (c *context) int(x ast.Node) (Int, error) {
	val, err := c.eval(x)
	if err != nil {
		return 0, err
	}
	if i, ok := val.(Int); ok {
		return i, nil
	}
	return 0, c.error(x, fmt.Sprintf("non-int value %s", val))
}

func (c *context) text(x ast.Node) (Text, error) {
	val, err := c.eval(x)
	if err != nil {
		return "", err
	}
	if i, ok := val.(Text); ok {
		return i, nil
	}
	return "", c.error(x, fmt.Sprintf("non-text value %s", val))
}

func (c *context) byte(x ast.Node) (Byte, error) {
	val, err := c.eval(x)
	if err != nil {
		return 0, err
	}
	if i, ok := val.(Byte); ok {
		return i, nil
	}
	return 0, c.error(x, fmt.Sprintf("non-byte value %s", val))
}

func (c *context) bytes(x ast.Node) (Bytes, error) {
	val, err := c.eval(x)
	if err != nil {
		return nil, err
	}
	if i, ok := val.(Bytes); ok {
		return i, nil
	}
	return nil, c.error(x, fmt.Sprintf("non-bytes value %s", val))
}

func (c *context) list(x ast.Node) (l List, err error) {
	var val Value
	val, err = c.eval(x)
	if err != nil {
		return
	}
	if i, ok := val.(List); ok {
		return i, nil
	}
	err = c.error(x, fmt.Sprintf("non-list value %s", val))
	return
}

func (c *context) record(x ast.Node) (r Record, err error) {
	var val Value
	val, err = c.eval(x)
	if err != nil {
		return
	}
	if r, ok := val.(Record); ok {
		return r, nil
	}
	err = c.error(x, fmt.Sprintf("non-record value %s", val))
	return
}
