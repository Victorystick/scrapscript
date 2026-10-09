package eval

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/types"
)

type EvalImport func(algo string, hash []byte) (Value, error)

type context struct {
	src        *ast.SourceExpr
	reg        *types.Registry
	vars       Vars
	evalImport EvalImport
	parent     *context
}

type Vars interface {
	Get(name string) Value
}

type Variables map[string]Value

func (v Variables) Get(name string) Value {
	return v[name]
}

type Binding struct {
	name  string
	value Value
}

func (b Binding) Get(name string) Value {
	if b.name == name {
		return b.value
	}
	return nil
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
	return &context{c.src, c.reg, vars, c.evalImport, c}
}

func (c *context) error(n ast.Node, msg string) error {
	return c.src.Error(n, msg)
}

func (c *context) tokenError(t ast.Tok, msg string) error {
	return c.src.TokenError(t, msg)
}

// Eval evaluates a SourceExpr in the context of a set of variables.
func Eval(se ast.SourceExpr, reg *types.Registry, vars Vars, evalImport EvalImport) (Value, error) {
	ctx := &context{&se, reg, vars, evalImport, nil}

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
	case ast.Enum:
		return c.enum(x)
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
		algo, hash := c.src.Import(x)
		bs, err := hex.DecodeString(c.src.Source.GetString(c.src.TokenSpan(hash).TrimStart(2)))
		if err != nil {
			return nil, c.error(x, fmt.Sprintf("bad import hash %s", c.src.NodeText(x)))
		}
		return c.evalImport(c.name(algo), bs)
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
		return Text(src.Source.GetString(src.TokenSpan(t).TrimBoth())), nil
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
		return nil, c.error(x,
			fmt.Sprintf("cannot perform addition on %s",
				c.reg.String(l.Type())))

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
			return append(bs, byte(r)), nil
		}

		if ls, ok := l.(List); ok {
			r, err := c.eval(right)
			if err != nil {
				return nil, err
			}
			typ := c.reg.GetList(ls.typ)
			if r.Type() != typ {
				// Special-case empty lists, which have type never.
				if typ == types.NeverRef {
					typ = r.Type()
				} else {
					return nil, c.error(right,
						fmt.Sprintf("cannot append %s to %s",
							c.reg.String(r.Type()), c.reg.String(ls.typ)))
				}
			}
			return List{c.reg.List(typ), append(ls.elements, r)}, nil
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
			typ := c.reg.GetList(ls.typ)
			if l.Type() != typ {
				// Special-case empty lists, which have type never.
				if typ == types.NeverRef {
					typ = r.Type()
				} else {
					return nil, c.error(left,
						fmt.Sprintf("cannot prepend %s to %s",
							c.reg.String(l.Type()), c.reg.String(ls.typ)))
				}
			}
			return List{c.reg.List(typ), append([]Value{l}, ls.elements...)}, nil
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
			return append(bs, r...), nil
		}

		if ls, ok := l.(List); ok {
			r, err := c.list(right)
			if err != nil {
				return nil, err
			}

			// Special-case empty lists.
			typ := ls.typ // a list type
			if typ != r.typ {
				if c.reg.GetList(typ) == types.NeverRef {
					typ = r.typ
				} else if c.reg.GetList(r.typ) != types.NeverRef {
					return nil, c.error(left, fmt.Sprintf("cannot concat %s to %s", c.reg.String(ls.typ), c.reg.String(r.typ)))
				}
			}
			return List{typ, append(ls.elements, r.elements...)}, nil
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
		return c.compose(left, right)
	case token.LCOMP: // a << b
		return c.compose(right, left)

	case token.PICK:
		return c.pick(x, ast.None)
	}

	return nil, c.error(x, fmt.Sprintf("unhandled %s operator", op))
}

func (c *context) call(fnX, argX ast.Node) (Value, error) {
	if c.src.Kind(fnX) == ast.Binary {
		if op, _, _ := c.src.Binary(fnX); op == token.PICK {
			return c.pick(fnX, argX)
		}
	}

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

func (c *context) compose(first, second ast.Node) (Value, error) {
	a, err := c.fn(first)
	if err != nil {
		return nil, err
	}
	b, err := c.fn(second)
	if err != nil {
		return nil, err
	}
	return ScriptFunc{
		// source: + "<<" + ,
		fn: func(v Value) (Value, error) {
			mid, err := a(v)
			if err != nil {
				return nil, err
			}
			return b(mid)
		},
	}, nil
}

func (c *context) enum(x ast.Node) (Type, error) {
	variants := c.src.Variants(x)
	enum := make(types.MapRef, len(variants))
	for _, v := range variants {
		tagTok, typ := c.src.Variant(v)
		tag := c.name(tagTok)
		if _, ok := enum[tag]; ok {
			return Type(types.NeverRef), c.tokenError(tagTok, fmt.Sprintf("cannot define tag #%s more than once", tag))
		}

		ref := types.NeverRef
		if typ != ast.None {
			val, err := c.typeRef(typ)
			if err != nil {
				return Type(types.NeverRef), err
			}
			ref = val
		}
		enum[tag] = ref
	}
	return Type(c.reg.Enum(enum)), nil
}

// typeRef returns the TypeRef of the type expression it is called with.
func (c *context) typeRef(x ast.Node) (ref types.TypeRef, err error) {
	switch c.src.Kind(x) {
	case ast.Ident:
		var val Value
		val, err = c.ident(x)
		if err != nil {
			return
		}
		t, ok := val.(Type)
		if !ok {
			return ref, c.error(x, fmt.Sprintf("required a type, got %s", val))
		}
		ref = types.TypeRef(t)
		return

	case ast.Func:
		arg, body := c.src.Func(x)
		var argRef, bodyRef types.TypeRef
		argRef, err = c.typeRef(arg)
		if err != nil {
			return
		}
		bodyRef, err = c.typeRef(body)
		if err != nil {
			return
		}
		ref = c.reg.Func(argRef, bodyRef)
		return

	case ast.Enum:
		mapRef := make(types.MapRef)
		for _, v := range c.src.Variants(x) {
			tag, typ := c.src.Variant(v)
			value := types.NeverRef
			if typ != ast.None {
				value, err = c.typeRef(typ)
				if err != nil {
					return
				}
			}
			mapRef[c.name(tag)] = value
		}
		ref = c.reg.Enum(mapRef)
		return
		// TODO: Handle other expression types.
	}

	err = c.error(x, fmt.Sprintf("%s does not evaluate to a type", c.src.NodeText(x)))
	return
}

func (c *context) recordExpr(x ast.Node) (r Record, err error) {
	rest, fields := c.src.Record(x)

	// New record.
	if rest == ast.None {
		ref := make(types.MapRef, fields.Len())
		values := make(map[string]Value, fields.Len())

		for i := range fields.Len() {
			tag := c.name(fields.Key(i))
			var val Value
			val, err = c.eval(fields.Value(i))
			if err != nil {
				return
			}

			ref[tag] = val.Type()
			values[tag] = val
		}
		r = Record{c.reg.Record(ref), values}
		return
	}

	// Record based on another.
	var other Record
	other, err = c.record(rest)
	if err != nil {
		return
	}
	ref := c.reg.GetRecord(other.typ)
	values := maps.Clone(other.values)

	for i := range fields.Len() {
		tag, x := c.name(fields.Key(i)), fields.Value(i)
		var val Value
		val, err = c.eval(x)
		if err != nil {
			return
		}

		typ, ok := ref[tag]
		if !ok {
			err = c.error(x,
				fmt.Sprintf("cannot set key %s not in the base record", tag))
			return
		}
		if val.Type() != typ {
			err = c.error(x,
				fmt.Sprintf("cannot change type of key %s from %s to %s",
					tag, c.reg.String(typ), c.reg.String(val.Type())))
			return
		}

		values[tag] = val
	}

	return Record{other.typ, values}, nil
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
	typ := types.NeverRef
	for i, x := range elems {
		var val Value
		val, err = c.eval(x)
		if err != nil {
			return
		}

		elements[i] = val
		if val.Type() != typ {
			if typ == types.NeverRef {
				typ = val.Type()
			} else {
				err = c.error(x, fmt.Sprintf("list elements must all be of type %s, got %s", c.reg.String(typ), c.reg.String(val.Type())))
				return
			}
		}
	}
	return List{c.reg.List(typ), elements}, nil
}

// pick evaluates `typ::tag`, applied to x unless it is None.
func (c *context) pick(pick ast.Node, x ast.Node) (Value, error) {
	_, left, right := c.src.Binary(pick)
	ref, err := c.typeRef(left)
	if err != nil {
		return nil, err
	}
	// This is always the case for a pick expression. Let's update the ast/parser.
	if c.src.Kind(right) != ast.Ident {
		return nil, c.error(right, fmt.Sprintf("cannot pick using non-identifier %s", c.src.NodeText(right)))
	}
	tag := c.name(c.src.Main(right))
	enum := c.reg.GetEnum(ref)
	if tagTyp, ok := enum[tag]; ok {
		if tagTyp == types.NeverRef {
			if x == ast.None {
				return Variant{ref, tag, nil}, nil
			} else {
				return nil, c.error(x, fmt.Sprintf("#%s does not take a value", tag))
			}
		} else {
			if x == ast.None {
				return nil, c.error(right, fmt.Sprintf("#%s requires a value of type %s", tag, c.reg.String(tagTyp)))
			} else {
				val, err := c.eval(x)
				if err != nil {
					return nil, err
				}
				return Variant{ref, tag, val}, nil
			}
		}
	}

	tags := strings.Join(slices.Sorted(maps.Keys(enum)), ", #")
	return nil, c.error(pick, fmt.Sprintf("#%s isn't one of the valid tags: #%s", tag, tags))
}

func (c *context) createFunc(x ast.Node) (ScriptFunc, error) {
	arg, body := c.src.Func(x)
	if c.src.Kind(arg) != ast.Ident {
		return ScriptFunc{}, c.error(arg, "function parameter must be an identifier")
	}
	name := c.name(c.src.Main(arg))
	return ScriptFunc{
		source: c.src.NodeText(x),
		fn: func(value Value) (Value, error) {
			return c.sub(Variables{name: value}).eval(body)
		},
	}, nil
}

func (c *context) createMatchFunc(x ast.Node) (ScriptFunc, error) {
	source := c.src.NodeText(x)
	return ScriptFunc{
		source: source,
		fn: func(a Value) (Value, error) {
			for _, alt := range c.src.Arms(x) {
				arg, body := c.src.Func(alt)
				matches, err := Match(c.src, c.reg, arg, a)
				if err != nil {
					if err == ErrNoMatch {
						continue
					}
					return nil, err
				}
				return c.sub(matches).eval(body)
			}
			return nil, fmt.Errorf("%s had no alternative for %s", source, a)
		},
	}, nil
}

func (c *context) where(x ast.Node) (Value, error) {
	body, id, typ, val := c.src.Where(x)
	name := c.name(id)

	// This where is type-only; semantics TBD?
	expr := val
	if expr == ast.None {
		expr = typ
	}

	v, err := c.eval(expr)
	if err != nil {
		return nil, err
	}

	return c.sub(Binding{name, v}).eval(body)
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
