package types

import (
	"encoding/hex"
	"fmt"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/token"
)

type Scope[T any] struct {
	parent *Scope[T]
	name   string
	val    T
}

func (s *Scope[T]) Lookup(name string) (res T) {
	if bound := s.Get(name); bound != nil {
		res = bound.val
	}
	return
}

func (s *Scope[T]) Get(name string) *Scope[T] {
	for s != nil {
		if s.name == name {
			return s
		}
		s = s.parent
	}
	return nil
}

func (s *Scope[T]) Bind(name string, val T) *Scope[T] {
	return &Scope[T]{s, name, val}
}

type TypeScope = *Scope[TypeRef]

type InferImport func(algo string, hash []byte) (TypeRef, error)

type context struct {
	src         *ast.SourceExpr
	reg         *Registry
	scope       TypeScope
	inferImport InferImport
}

func (c *context) bail(n ast.Node, msg string) {
	panic(c.src.Error(n, msg))
}

func (c *context) bind(name string, ref TypeRef) TypeScope {
	c.scope = c.scope.Bind(name, ref)
	return c.scope
}

// Unbinds the last bound variable.
func (c *context) unbind() {
	c.scope = c.scope.parent
}

func DefaultScope(reg *Registry) (scope TypeScope) {
	for _, p := range primitives {
		scope = scope.Bind(reg.String(p), p)
	}
	return
}

func Infer(reg *Registry, scope TypeScope, se ast.SourceExpr, inferImport InferImport) (ref TypeRef, err error) {
	context := context{
		src:         &se,
		reg:         reg,
		scope:       scope,
		inferImport: inferImport,
	}

	defer func() {
		if pnc := recover(); pnc != nil {
			if e, ok := pnc.(token.Error); ok {
				err = e
			} else {
				panic(pnc)
			}
		}
	}()

	ref = context.infer(se.Expr)
	return ref, err
}

type InferFunc func(expr ast.Node) TypeRef

func (c *context) infer(x ast.Node) TypeRef {
	switch c.src.Kind(x) {
	case ast.Literal:
		return literalTypeRef(c.src.LiteralKind(x))
	case ast.Ident:
		name := c.src.TokenText(c.src.Main(x))
		ref := c.scope.Lookup(name)
		if ref == NeverRef {
			c.bail(x, "unbound variable: "+name)
		}
		return c.reg.Instantiate(ref)
	case ast.Where:
		return c.where(x)
	case ast.List:
		return c.list(x)
	case ast.Record:
		return c.record(x)
	case ast.Enum:
		return c.enum(x, c.infer)

	case ast.Func:
		arg, body := c.src.Func(x)
		// Not sure how to juggle vars vs unbound. :/
		binder := c.reg.Var()
		c.bind(c.src.NodeText(arg), binder)
		defer c.unbind()
		ret := c.infer(body)
		return c.reg.Func(binder, ret)

	case ast.MatchFunc:
		argTy := c.reg.Var()
		bodyTy := c.reg.Var()
		for _, opt := range c.src.Arms(x) {
			arg, body := c.src.Func(opt)
			boundVars := c.match(&argTy, arg)
			bodyTy = c.ensure(body, bodyTy, c.infer(body))
			// Unbind all bound variables.
			for i := 0; i < boundVars; i++ {
				c.unbind()
			}
		}
		return c.reg.Func(argTy, bodyTy)

	case ast.Call:
		fn, arg := c.src.Call(x)
		// Special-case pick with a value.
		if c.src.Kind(fn) == ast.Binary {
			if op, _, _ := c.src.Binary(fn); op == token.PICK {
				return c.pick(fn, arg)
			}
		}

		return c.call(x, fn, arg)

	case ast.Binary:
		op, l, r := c.src.Binary(x)
		if op == token.PICK {
			return c.pick(x, ast.None)
		}

		left := c.infer(l)
		right := c.infer(r)
		switch op {
		case token.PREPEND:
			return c.pend(l, r, left, right)
		case token.APPEND:
			return c.pend(r, l, right, left)
		case token.CONCAT:
			if left == TextRef || right == TextRef {
				c.ensure(x, left, right)
				return TextRef
			}
			if left == BytesRef || right == BytesRef {
				c.ensure(x, left, right)
				return BytesRef
			}
			// Local var to ensure left and right are lists.
			a := c.reg.List(c.reg.Var())
			c.ensure(x, left, right)
			c.ensure(x, left, a)
			return a
		case token.ADD, token.SUB, token.MUL:
			if left == FloatRef || right == FloatRef {
				c.ensure(x, left, right)
				return FloatRef
			}
			// Assume int, like ML does.
			c.ensure(l, left, IntRef)
			return c.ensure(r, right, IntRef)

		// Pipes are essentially just calls.
		case token.LPIPE:
			return c.call(x, l, r)
		case token.RPIPE:
			return c.call(x, r, l)
		}
		panic(fmt.Sprintf("can't infer binary expression %s", op.String()))
	case ast.Import:
		if c.inferImport == nil {
			c.bail(x, "<internal error> missing infer import function")
		}
		algo, hash := c.src.Import(x)
		bs, err := hex.DecodeString(c.src.Source.GetString(c.src.TokenSpan(hash).TrimStart(2)))
		if err != nil {
			c.bail(x, fmt.Sprintf("bad import hash %s", c.src.NodeText(x)))
		}
		ref, err := c.inferImport(c.src.TokenText(algo), bs)
		if err != nil {
			c.bail(x, err.Error())
		}
		return ref
	}

	panic(fmt.Sprintf("can't infer node %s", c.src.Kind(x)))
}

func (c *context) ensure(x ast.Node, got, want TypeRef) TypeRef {
	if got != want {
		// Really? Must make this API better.
		defer func() {
			if pnc := recover(); pnc != nil {
				if msg, ok := pnc.(string); ok {
					c.bail(x, msg)
				} else {
					panic(pnc)
				}
			}
		}()

		return c.reg.unify(got, want)
	}
	return want
}

func (c *context) call(x, fn, arg ast.Node) TypeRef {
	res := c.reg.Var()
	fnTy := c.infer(fn)
	argTy := c.infer(arg)
	c.ensure(x, fnTy, c.reg.Func(argTy, res))
	return res
}

// The expression type is passed by pointer so that we can update it.
func (c *context) match(ty *TypeRef, x ast.Node) int {
	switch c.src.Kind(x) {
	case ast.Ident:
		name := c.src.TokenText(c.src.Main(x))
		// Ignore _.
		if name == "_" {
			return 0
		}

		c.bind(name, *ty)
		return 1

	case ast.Literal:
		c.ensure(x, *ty, literalTypeRef(c.src.LiteralKind(x)))
		return 0

	case ast.Binary:
		op, l, r := c.src.Binary(x)
		if op == token.PREPEND {
			val := c.reg.Var()
			valList := c.reg.List(val)
			c.ensure(x, *ty, valList)
			return c.match(&val, l) + c.match(&valList, r)
		}
		if op == token.APPEND {
			val := c.reg.Var()
			valList := c.reg.List(val)
			c.ensure(x, *ty, valList)
			return c.match(&valList, l) + c.match(&val, r)
		}
		if op == token.CONCAT {
			val := c.reg.Var()
			valList := c.reg.List(val)
			c.ensure(x, *ty, valList)
			return c.match(&valList, l) + c.match(&valList, r)
		}

	case ast.List:
		val := c.reg.Var()
		c.ensure(x, c.reg.List(val), *ty)

		bindings := 0
		for _, v := range c.src.List(x) {
			bindings += c.match(&val, v)
		}
		return bindings

	case ast.Variant:
		bindings := 0
		tag, typ := c.src.Variant(x)
		name := c.src.TokenText(tag)
		vRef := NeverRef
		if typ != ast.None {
			vRef = c.reg.Var()
			bindings += c.match(&vRef, typ)
		}
		ref := c.reg.Enum(map[string]TypeRef{name: vRef})
		*ty = c.ensure(x, *ty, ref)
		return bindings

	default:
		c.bail(x, fmt.Sprintf("cannot match on %s", c.src.Kind(x)))
	}
	// Unreachable.
	return 0
}

func (c *context) where(x ast.Node) TypeRef {
	body, id, typ, val := c.src.Where(x)
	name := c.src.TokenText(id)

	// This where is type-only; semantics TBD?
	if val == ast.None {
		c.bind(name, c.reg.generalize(c.typ(typ)))
		defer c.unbind()
		return c.infer(body)
	}

	tyVal := c.infer(val)

	// If there's an annotation, make sure it matches the inferred type.
	if typ != ast.None {
		c.ensure(typ, tyVal, c.typ(typ))
	}

	c.bind(name, c.reg.generalize(tyVal))
	defer c.unbind()
	return c.infer(body)
}

func (c *context) typ(x ast.Node) TypeRef {
	switch c.src.Kind(x) {
	case ast.Ident:
		name := c.src.TokenText(c.src.Main(x))
		ref := c.scope.Lookup(name)
		if ref == NeverRef {
			c.bail(x, fmt.Sprintf("unknown type %s", name))
		}
		return ref
	case ast.Func:
		arg, body := c.src.Func(x)
		return c.reg.Func(
			c.typ(arg),
			c.typ(body),
		)
	case ast.Enum:
		return c.enum(x, c.typ)
	}

	c.bail(x, fmt.Sprintf("cannot infer type of %s", c.src.Kind(x)))
	return NeverRef
}

func (c *context) list(x ast.Node) TypeRef {
	res := NeverRef

	for _, v := range c.src.List(x) {
		typ := c.infer(v)

		if res == NeverRef {
			res = typ
			continue
		}

		c.ensure(v, res, typ)
	}

	if res == NeverRef {
		res = c.reg.Var()
	}
	return c.reg.List(res)
}

func (c *context) record(x ast.Node) TypeRef {
	rest, fields := c.src.Record(x)

	// If there is a rest/spread, our type is equal to that.
	if rest != ast.None {
		restTy := c.infer(rest)
		rec := c.reg.GetRecord(restTy)
		if rec == nil {
			c.bail(rest, fmt.Sprintf("cannot spread from non-record type %s", c.reg.String(restTy)))
		}
		for i := range fields.Len() {
			k, v := c.src.TokenText(fields.Key(i)), fields.Value(i)
			expected, ok := rec[k]
			if !ok {
				c.bail(v, fmt.Sprintf("cannot set %s not in the base record", k))

			}
			actual := c.infer(v)
			if actual != expected {
				c.bail(v, fmt.Sprintf("type of %s must be %s, not %s", k, c.reg.String(expected), c.reg.String(actual)))
			}
		}
		return restTy
	}

	ref := make(MapRef, fields.Len())
	for i := range fields.Len() {
		ref[c.src.TokenText(fields.Key(i))] = c.infer(fields.Value(i))
	}
	return c.reg.Record(ref)
}

func (c *context) enum(x ast.Node, rec InferFunc) TypeRef {
	variants := c.src.Variants(x)
	ref := make(MapRef, len(variants))
	for _, v := range variants {
		tag, typ := c.src.Variant(v)
		vRef := NeverRef
		if typ != ast.None {
			vRef = rec(typ)
		}
		ref[c.src.TokenText(tag)] = vRef
	}
	return c.reg.Enum(ref)
}

func (c *context) pick(x ast.Node, val ast.Node) TypeRef {
	_, l, r := c.src.Binary(x)
	ref := c.infer(l)
	enum := c.reg.GetEnum(ref)
	if enum == nil {
		c.bail(l, fmt.Sprintf("%s isn't an enum", c.reg.String(ref)))
	}

	if c.src.Kind(r) == ast.Ident {
		tag := c.src.NodeText(r)
		typ, ok := enum[tag]
		if !ok {
			c.bail(r,
				fmt.Sprintf("#%s isn't a valid option for enum %s",
					tag, c.reg.String(ref)))
		}

		// We expect no value.
		if typ == NeverRef {
			// But there was one.
			if val != ast.None {
				c.bail(val, fmt.Sprintf("#%s doesn't take any value", tag))
			}
		} else {
			valRef := c.infer(val)
			c.ensure(val, valRef, typ)
		}

		return ref
	}

	// TODO: better error handling?
	return NeverRef
}

func literalTypeRef(tok token.Token) TypeRef {
	switch tok {
	case token.HOLE:
		return HoleRef
	case token.INT:
		return IntRef
	case token.FLOAT:
		return FloatRef
	case token.TEXT:
		return TextRef
	case token.BYTE:
		return ByteRef
	case token.BYTES:
		return BytesRef
	}

	return NeverRef
}

// Either pre-pend or ap-pend.
func (c *context) pend(singleX, listX ast.Node, single, list TypeRef) TypeRef {
	// Special-case bytes.
	if single == ByteRef || list == BytesRef {
		c.ensure(singleX, single, ByteRef)
		c.ensure(listX, list, BytesRef)
		return BytesRef
	}

	c.ensure(singleX, c.reg.List(single), list)
	return list
}
