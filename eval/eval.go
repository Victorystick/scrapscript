package eval

import (
	"encoding/base64"
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

type context struct {
	source *token.Source
	reg    *types.Registry
	vars   Vars
	parent *context
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

func (c *context) ident(x *ast.Ident) (Value, error) {
	name := c.name(x)

	// Traverse the context stack.
	context := c
	for context != nil {
		val := context.vars.Get(name)
		if val != nil {
			return val, nil
		}
		context = context.parent
	}

	return nil, c.error(x.Pos, fmt.Sprintf("unknown variable %s", name))
}

func (c *context) name(id *ast.Ident) string {
	return c.source.GetString(id.Pos)
}

func (c *context) sub(vars Vars) *context {
	return &context{c.source, c.reg, vars, c}
}

func (c *context) error(span token.Span, msg string) error {
	return c.source.Error(span, msg)
}

// Eval evaluates a SourceExpr in the context of a set of variables.
func Eval(se ast.SourceExpr, reg *types.Registry, vars Vars) (Value, error) {
	ctx := &context{&se.Source, reg, vars, nil}

	return ctx.eval(se.Expr)
}

func (c *context) eval(x ast.Node) (Value, error) {
	switch x := x.(type) {
	case *ast.Literal:
		return Literal(c.source, x)
	case *ast.Ident:
		return c.ident(x)
	case *ast.BinaryExpr:
		return c.binary(x)
	case *ast.CallExpr:
		return c.call(x)
	case *ast.WhereExpr:
		return c.where(x)
	case ast.EnumExpr:
		return c.enum(x)
	case *ast.RecordExpr:
		return c.recordExpr(x)
	case *ast.ListExpr:
		return c.listExpr(x)
	case *ast.FuncExpr:
		return c.createFunc(x)
	case ast.MatchFuncExpr:
		return c.createMatchFunc(x)
	case *ast.AccessExpr:
		return c.access(x)
	}

	return nil, c.error(x.Span(), fmt.Sprintf("unhandled node %#v", x))
}

func Literal(source *token.Source, x *ast.Literal) (Value, error) {
	switch x.Kind {
	case token.HOLE:
		return Hole{}, nil
	case token.INT:
		i, err := strconv.Atoi(source.GetString(x.Pos))
		if err != nil {
			return nil, err
		}
		return Int(i), nil
	case token.FLOAT:
		f, err := strconv.ParseFloat(source.GetString(x.Pos), 64)
		if err != nil {
			return nil, err
		}
		return Float(f), nil
	case token.TEXT:
		return Text(source.GetString(x.Pos.TrimBoth())), nil
	case token.BYTES:
		str := source.GetString(x.Pos.TrimStart(2))
		dst := make([]byte, base64.StdEncoding.DecodedLen(len(str)))
		n, err := base64.StdEncoding.Decode(dst, []byte(str))
		if err != nil {
			return nil, err
		}
		return Bytes(dst[:n]), nil
	case token.BYTE:
		val, err := strconv.ParseUint(source.GetString(x.Pos.TrimStart(1)), 16, 8)
		if err != nil {
			return nil, err
		}
		return Byte(byte(val)), nil
	}

	return nil, source.Error(x.Pos, fmt.Sprintf("unhandled literal kind %s", x.Kind))
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

func (c *context) binary(x *ast.BinaryExpr) (Value, error) {
	switch x.Op {
	case token.ADD, token.SUB, token.MUL:
		l, err := c.eval(x.Left)
		if err != nil {
			return nil, err
		}
		if lf, ok := l.(Float); ok {
			rf, err := c.float(x.Right)
			if err != nil {
				return nil, err
			}
			return binop(x.Op, lf, rf)
		}
		if lf, ok := l.(Int); ok {
			rf, err := c.int(x.Right)
			if err != nil {
				return nil, err
			}
			return binop(x.Op, lf, rf)
		}
		return nil, c.error(x.Span(),
			fmt.Sprintf("cannot perform addition on %s",
				c.reg.String(l.Type())))

	case token.APPEND:
		l, err := c.eval(x.Left)
		if err != nil {
			return nil, err
		}

		if bs, ok := l.(Bytes); ok {
			r, err := c.byte(x.Right)
			if err != nil {
				return nil, err
			}
			return append(bs, byte(r)), nil
		}

		if ls, ok := l.(List); ok {
			r, err := c.eval(x.Right)
			if err != nil {
				return nil, err
			}
			typ := c.reg.GetList(ls.typ)
			if r.Type() != typ {
				// Special-case empty lists, which have type never.
				if typ == types.NeverRef {
					typ = r.Type()
				} else {
					return nil, c.error(x.Right.Span(),
						fmt.Sprintf("cannot append %s to %s",
							c.reg.String(r.Type()), c.reg.String(ls.typ)))
				}
			}
			return List{c.reg.List(typ), append(ls.elements, r)}, nil
		}

		return nil, fmt.Errorf("cannot append to non-list %s", reflect.TypeOf(l))

	case token.PREPEND:
		r, err := c.eval(x.Right)
		if err != nil {
			return nil, err
		}

		if bs, ok := r.(Bytes); ok {
			l, err := c.byte(x.Left)
			if err != nil {
				return nil, err
			}
			return append(Bytes{byte(l)}, bs...), nil
		}

		if ls, ok := r.(List); ok {
			l, err := c.eval(x.Left)
			if err != nil {
				return nil, err
			}
			typ := c.reg.GetList(ls.typ)
			if l.Type() != typ {
				// Special-case empty lists, which have type never.
				if typ == types.NeverRef {
					typ = r.Type()
				} else {
					return nil, c.error(x.Left.Span(),
						fmt.Sprintf("cannot prepend %s to %s",
							c.reg.String(l.Type()), c.reg.String(ls.typ)))
				}
			}
			return List{c.reg.List(typ), append([]Value{l}, ls.elements...)}, nil
		}

		return nil, fmt.Errorf("cannot prepend to non-list %s", reflect.TypeOf(r))

	case token.CONCAT:
		l, err := c.eval(x.Left)
		if err != nil {
			return nil, err
		}

		if bs, ok := l.(Bytes); ok {
			r, err := c.bytes(x.Right)
			if err != nil {
				return nil, err
			}
			return append(bs, r...), nil
		}

		if ls, ok := l.(List); ok {
			r, err := c.list(x.Right)
			if err != nil {
				return nil, err
			}

			// Special-case empty lists.
			typ := ls.typ // a list type
			if typ != r.typ {
				if c.reg.GetList(typ) == types.NeverRef {
					typ = r.typ
				} else if c.reg.GetList(r.typ) != types.NeverRef {
					return nil, c.error(x.Left.Span(), fmt.Sprintf("cannot concat %s to %s", c.reg.String(ls.typ), c.reg.String(r.typ)))
				}
			}
			return List{typ, append(ls.elements, r.elements...)}, nil
		}

		if tx, ok := l.(Text); ok {
			r, err := c.text(x.Right)
			if err != nil {
				return nil, err
			}
			return tx + r, nil
		}

		return nil, fmt.Errorf("cannot append to non-list %s", reflect.TypeOf(l))

	case token.RPIPE:
		// Construct a call.
		call := ast.CallExpr{
			Fn:  x.Right,
			Arg: x.Left,
		}
		return c.call(&call)
	case token.LPIPE:
		// Construct a call.
		call := ast.CallExpr{
			Fn:  x.Left,
			Arg: x.Right,
		}
		return c.call(&call)

	case token.RCOMP: // a >> b
		return c.compose(x.Left, x.Right)
	case token.LCOMP: // a << b
		return c.compose(x.Right, x.Left)

	case token.PICK:
		return c.pick(x, nil)
	}

	return nil, c.error(x.Span(), fmt.Sprintf("unhandled %s operator", x.Op))
}

func (c *context) call(x *ast.CallExpr) (Value, error) {
	if bin, ok := x.Fn.(*ast.BinaryExpr); ok {
		if bin.Op == token.PICK {
			return c.pick(bin, x.Arg)
		}
	}

	fn, err := c.fn(x.Fn)
	if err != nil {
		return nil, err
	}
	arg, err := c.eval(x.Arg)
	if err != nil {
		return nil, err
	}
	return fn(arg)
}

func (c *context) compose(first, second ast.Expr) (Value, error) {
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

func (c *context) enum(typ ast.EnumExpr) (Type, error) {
	enum := make(types.MapRef, len(typ))
	for _, v := range typ {
		tag := c.name(&v.Tag)
		if _, ok := enum[tag]; ok {
			return Type(types.NeverRef), c.error(v.Tag.Pos, fmt.Sprintf("cannot define tag #%s more than once", tag))
		}

		ref := types.NeverRef
		if v.Typ != nil {
			val, err := c.typeRef(v.Typ)
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
func (c *context) typeRef(x ast.Expr) (ref types.TypeRef, err error) {
	switch x := x.(type) {
	case *ast.Ident:
		var val Value
		val, err = c.ident(x)
		if err != nil {
			return
		}
		t, ok := val.(Type)
		if !ok {
			return ref, c.error(x.Span(), fmt.Sprintf("required a type, got %s", val))
		}
		ref = types.TypeRef(t)
		return

	case *ast.FuncExpr:
		var argRef, bodyRef types.TypeRef
		argRef, err = c.typeRef(x.Arg)
		if err != nil {
			return
		}
		bodyRef, err = c.typeRef(x.Body)
		if err != nil {
			return
		}
		ref = c.reg.Func(argRef, bodyRef)
		return

		// TODO: Handle other expression types.
	}

	err = c.error(x.Span(), fmt.Sprintf("%s does not evaluate to a type", c.source.GetString(x.Span())))
	return
}

func (c *context) recordExpr(x *ast.RecordExpr) (r Record, err error) {
	// New record.
	if x.Rest == nil {
		ref := make(types.MapRef, len(x.Entries))
		values := make(map[string]Value, len(x.Entries))

		for tag, x := range x.Entries {
			var val Value
			val, err = c.eval(x)
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
	other, err = c.record(x.Rest)
	if err != nil {
		return
	}
	ref := c.reg.GetRecord(other.typ)
	values := maps.Clone(other.values)

	for tag, x := range x.Entries {
		var val Value
		val, err = c.eval(x)
		if err != nil {
			return
		}

		typ, ok := ref[tag]
		if !ok {
			err = c.error(x.Span(),
				fmt.Sprintf("cannot set key %s not in the base record", tag))
			return
		}
		if val.Type() != typ {
			err = c.error(x.Span(),
				fmt.Sprintf("cannot change type of key %s from %s to %s",
					tag, c.reg.String(typ), c.reg.String(val.Type())))
			return
		}

		values[tag] = val
	}

	return Record{other.typ, values}, nil
}

func (c *context) access(x *ast.AccessExpr) (Value, error) {
	r, err := c.record(x.Rec)
	if err != nil {
		return nil, err
	}
	key := c.name(&x.Key)
	val, ok := r.values[key]
	if !ok {
		return nil, c.error(x.Key.Pos,
			fmt.Sprintf("record %s has no key %s", r, key))
	}
	return val, nil
}

func (c *context) listExpr(x *ast.ListExpr) (ls List, err error) {
	elements := make([]Value, len(x.Elements))
	typ := types.NeverRef
	for i, x := range x.Elements {
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
				err = c.error(x.Span(), fmt.Sprintf("list elements must all be of type %s, got %s", c.reg.String(typ), c.reg.String(val.Type())))
				return
			}
		}
	}
	return List{c.reg.List(typ), elements}, nil
}

func (c *context) pick(pick *ast.BinaryExpr, x ast.Expr) (Value, error) {
	ref, err := c.typeRef(pick.Left)
	if err != nil {
		return nil, err
	}
	// This is always the case for a pick expression. Let's update the ast/parser.
	id, ok := pick.Right.(*ast.Ident)
	if !ok {
		return nil, c.error(pick.Right.Span(), fmt.Sprintf("cannot pick using non-identifier %#v", pick))
	}
	tag := c.name(id)
	enum := c.reg.GetEnum(ref)
	if tagTyp, ok := enum[tag]; ok {
		if tagTyp == types.NeverRef {
			if x == nil {
				return Variant{ref, tag, nil}, nil
			} else {
				return nil, c.error(x.Span(), fmt.Sprintf("#%s does not take a value", tag))
			}
		} else {
			if x == nil {
				return nil, c.error(pick.Right.Span(), fmt.Sprintf("#%s requires a value of type %s", tag, c.reg.String(tagTyp)))
			} else {
				val, err := c.eval(x)
				if err != nil {
					return nil, err
				}
				if val.Type() != tagTyp {
					return nil, c.error(pick.Right.Span(),
						fmt.Sprintf("#%s requires a value of type %s, got %s",
							tag, c.reg.String(tagTyp), c.reg.String(val.Type())))
				}
				return Variant{ref, tag, val}, nil
			}
		}
	}

	tags := strings.Join(slices.Sorted(maps.Keys(enum)), ", #")
	return nil, c.error(pick.Span(), fmt.Sprintf("#%s isn't one of the valid tags: #%s", tag, tags))
}

func (c *context) createFunc(x *ast.FuncExpr) (ScriptFunc, error) {
	id, ok := x.Arg.(*ast.Ident)
	if !ok {
		return ScriptFunc{}, c.error(x.Arg.Span(), "function parameter must be an identifier")
	}
	name := c.name(id)
	return ScriptFunc{
		source: c.source.GetString(x.Span()),
		fn: func(value Value) (Value, error) {
			return c.sub(Variables{name: value}).eval(x.Body)
		},
	}, nil
}

func (c *context) createMatchFunc(x ast.MatchFuncExpr) (ScriptFunc, error) {
	source := c.source.GetString(x.Span())
	return ScriptFunc{
		source: source,
		fn: func(a Value) (Value, error) {
			for _, alt := range x {
				matches, err := Match(c.source, c.reg, alt.Arg, a)
				if err != nil {
					if err == ErrNoMatch {
						continue
					}
					return nil, err
				}
				return c.sub(matches).eval(alt.Body)
			}
			return nil, fmt.Errorf("%s had no alternative for %s", source, a)
		},
	}, nil
}

func (c *context) where(x *ast.WhereExpr) (Value, error) {
	name := c.name(&x.Id)
	val, err := c.eval(x.Val)
	if err != nil {
		return nil, err
	}

	return c.sub(Binding{name, val}).eval(x.Expr)
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
	return nil, c.error(x.Span(), fmt.Sprintf("non-func value %s", val))
}

func (c *context) float(x ast.Node) (Float, error) {
	val, err := c.eval(x)
	if err != nil {
		return 0, err
	}
	if f, ok := val.(Float); ok {
		return f, nil
	}
	return 0, c.error(x.Span(), fmt.Sprintf("non-float value %s", val))
}

func (c *context) int(x ast.Node) (Int, error) {
	val, err := c.eval(x)
	if err != nil {
		return 0, err
	}
	if i, ok := val.(Int); ok {
		return i, nil
	}
	return 0, c.error(x.Span(), fmt.Sprintf("non-int value %s", val))
}

func (c *context) text(x ast.Node) (Text, error) {
	val, err := c.eval(x)
	if err != nil {
		return "", err
	}
	if i, ok := val.(Text); ok {
		return i, nil
	}
	return "", c.error(x.Span(), fmt.Sprintf("non-text value %s", val))
}

func (c *context) byte(x ast.Node) (Byte, error) {
	val, err := c.eval(x)
	if err != nil {
		return 0, err
	}
	if i, ok := val.(Byte); ok {
		return i, nil
	}
	return 0, c.error(x.Span(), fmt.Sprintf("non-byte value %s", val))
}

func (c *context) bytes(x ast.Node) (Bytes, error) {
	val, err := c.eval(x)
	if err != nil {
		return nil, err
	}
	if i, ok := val.(Bytes); ok {
		return i, nil
	}
	return nil, c.error(x.Span(), fmt.Sprintf("non-bytes value %s", val))
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
	err = c.error(x.Span(), fmt.Sprintf("non-list value %s", val))
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
	err = c.error(x.Span(), fmt.Sprintf("non-record value %s", val))
	return
}
