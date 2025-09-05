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
	source      token.Source
	reg         *Registry
	scope       TypeScope
	inferImport InferImport
}

func (c *context) bail(span token.Span, msg string) {
	panic(c.source.Error(span, msg))
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
		source:      se.Source,
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

type InferFunc func(expr ast.Expr) TypeRef

func (c *context) infer(expr ast.Expr) TypeRef {
	switch x := expr.(type) {
	case *ast.Literal:
		return literalTypeRef(x.Kind)
	case *ast.Ident:
		name := c.source.GetString(x.Pos)
		ref := c.scope.Lookup(name)
		if ref == NeverRef {
			c.bail(x.Pos, "unbound variable: "+name)
		}
		return c.reg.Instantiate(ref)
	case *ast.WhereExpr:
		return c.where(x)
	case *ast.ListExpr:
		return c.list(x)
	case *ast.RecordExpr:
		return c.record(x)
	case ast.EnumExpr:
		return c.enum(x, func(expr ast.Expr) TypeRef {
			return c.infer(expr)
		})

	case *ast.FuncExpr:
		// Not sure how to juggle vars vs unbound. :/
		binder := c.reg.Var()
		c.bind(c.source.GetString(x.Arg.Span()), binder)
		defer c.unbind()
		ret := c.infer(x.Body)
		return c.reg.Func(binder, ret)

	case ast.MatchFuncExpr:
		argTy := c.reg.Var()
		bodyTy := c.reg.Var()
		for _, opt := range x {
			boundVars := c.match(argTy, opt.Arg)
			c.ensure(opt.Body, bodyTy, c.infer(opt.Body))
			// Unbind all bound variables.
			for i := 0; i < boundVars; i++ {
				c.unbind()
			}
		}
		return c.reg.Func(argTy, bodyTy)

	case *ast.CallExpr:
		// Special-case pick with a value.
		if pick, ok := x.Fn.(*ast.BinaryExpr); ok && pick.Op == token.PICK {
			return c.pick(pick, x.Arg)
		}

		return c.call(x, x.Fn, x.Arg)

	case *ast.BinaryExpr:
		if x.Op == token.PICK {
			return c.pick(x, nil)
		}

		left := c.infer(x.Left)
		right := c.infer(x.Right)
		switch x.Op {
		case token.PREPEND:
			return c.pend(x.Left, x.Right, left, right)
		case token.APPEND:
			return c.pend(x.Right, x.Left, right, left)
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
			c.ensure(x.Left, left, IntRef)
			return c.ensure(x.Right, right, IntRef)

		// Pipes are essentially just calls.
		case token.LPIPE:
			return c.call(x, x.Left, x.Right)
		case token.RPIPE:
			return c.call(x, x.Right, x.Left)
		}
		panic(fmt.Sprintf("can't infer binary expression %s", x.Op.String()))
	case *ast.ImportExpr:
		if c.inferImport == nil {
			c.bail(x.Span(), "<internal error> missing infer import function")
		}
		bs, err := hex.DecodeString(c.source.GetString(x.Value.Pos.TrimStart(2)))
		if err != nil {
			c.bail(x.Span(), fmt.Sprintf("bad import hash %#v", x))
		}
		ref, err := c.inferImport(x.HashAlgo, bs)
		if err != nil {
			c.bail(x.Span(), err.Error())
		}
		return ref
	}

	panic(fmt.Sprintf("can't infer node %T", expr))
}

func (c *context) ensure(x ast.Expr, got, want TypeRef) TypeRef {
	if got != want {
		// Really? Must make this API better.
		defer func() {
			if pnc := recover(); pnc != nil {
				if msg, ok := pnc.(string); ok {
					c.bail(x.Span(), msg)
				} else {
					panic(pnc)
				}
			}
		}()

		c.reg.unify(got, want)
	}
	return want
}

func (c *context) call(x, fn, arg ast.Expr) TypeRef {
	res := c.reg.Var()
	fnTy := c.infer(fn)
	argTy := c.infer(arg)
	c.ensure(x, fnTy, c.reg.Func(argTy, res))
	return res
}

func (c *context) match(argTy TypeRef, arg ast.Expr) int {
	switch arg := arg.(type) {
	case *ast.Ident:
		name := c.source.GetString(arg.Pos)
		// Ignore _.
		if name == "_" {
			return 0
		}

		c.bind(name, argTy)
		return 1

	case *ast.Literal:
		c.ensure(arg, argTy, literalTypeRef(arg.Kind))
		return 0

	case *ast.BinaryExpr:
		if arg.Op == token.PREPEND {
			val := c.reg.Var()
			valList := c.reg.List(val)
			c.ensure(arg, argTy, valList)
			return c.match(val, arg.Left) + c.match(valList, arg.Right)
		}
		if arg.Op == token.APPEND {
			val := c.reg.Var()
			valList := c.reg.List(val)
			c.ensure(arg, argTy, valList)
			return c.match(valList, arg.Left) + c.match(val, arg.Right)
		}
		if arg.Op == token.CONCAT {
			val := c.reg.Var()
			valList := c.reg.List(val)
			c.ensure(arg, argTy, valList)
			return c.match(valList, arg.Left) + c.match(valList, arg.Right)
		}

	case *ast.ListExpr:
		val := c.reg.Var()
		c.ensure(arg, c.reg.List(val), argTy)

		bindings := 0
		for _, v := range arg.Elements {
			bindings += c.match(val, v)
		}
		return bindings

	default:
		c.bail(arg.Span(), fmt.Sprintf("cannot match on %T", arg))
	}
	// Unreachable.
	return 0
}

func (c *context) where(x *ast.WhereExpr) TypeRef {
	name := c.source.GetString(x.Id.Pos)

	// This where is type-only; semantics TBD?
	if x.Val == nil {
		c.bind(name, c.reg.generalize(c.typ(x.Typ)))
		defer c.unbind()
		return c.infer(x.Expr)
	}

	tyVal := c.infer(x.Val)

	// If there's an annotation, make sure it matches the inferred type.
	if x.Typ != nil {
		c.ensure(x.Typ, tyVal, c.typ(x.Typ))
	}

	c.bind(name, c.reg.generalize(tyVal))
	defer c.unbind()
	return c.infer(x.Expr)
}

func (c *context) typ(x ast.Expr) TypeRef {
	switch x := x.(type) {
	case *ast.Ident:
		name := c.source.GetString(x.Pos)
		ref := c.scope.Lookup(name)
		if ref == NeverRef {
			c.bail(x.Span(), fmt.Sprintf("unknown type %s", name))
		}
		return ref
	case *ast.FuncExpr:
		return c.reg.Func(
			c.typ(x.Arg),
			c.typ(x.Body),
		)
	case ast.EnumExpr:
		return c.enum(x, func(expr ast.Expr) TypeRef {
			return c.typ(expr)
		})
	}

	c.bail(x.Span(), fmt.Sprintf("cannot infer type of %T", x))
	return NeverRef
}

func (c *context) list(x *ast.ListExpr) TypeRef {
	res := NeverRef

	for _, v := range x.Elements {
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

func (c *context) record(x *ast.RecordExpr) TypeRef {
	// If there is a rest/spread, our type is equal to that.
	if x.Rest != nil {
		rest := c.infer(x.Rest)
		rec := c.reg.GetRecord(rest)
		if rec == nil {
			c.bail(x.Rest.Span(), fmt.Sprintf("cannot spread from non-record type %s", c.reg.String(rest)))
		}
		for k, v := range x.Entries {
			expected, ok := rec[k]
			if !ok {
				c.bail(v.Span(), fmt.Sprintf("cannot set %s not in the base record", k))

			}
			actual := c.infer(v)
			if actual != expected {
				c.bail(v.Span(), fmt.Sprintf("type of %s must be %s, not %s", k, c.reg.String(expected), c.reg.String(actual)))
			}
		}
		return rest
	}

	ref := make(MapRef, len(x.Entries))
	for k, v := range x.Entries {
		ref[k] = c.infer(v)
	}
	return c.reg.Record(ref)
}

func (c *context) enum(x ast.EnumExpr, rec InferFunc) TypeRef {
	ref := make(MapRef, len(x))
	for _, v := range x {
		name := c.source.GetString(v.Tag.Pos)
		vRef := NeverRef
		if v.Typ != nil {
			vRef = rec(v.Typ)
		}
		ref[name] = vRef
	}
	return c.reg.Enum(ref)
}

func (c *context) pick(x *ast.BinaryExpr, val ast.Expr) TypeRef {
	ref := c.infer(x.Left)
	enum := c.reg.GetEnum(ref)
	if enum == nil {
		c.bail(x.Left.Span(), fmt.Sprintf("%s isn't an enum", c.reg.String(ref)))
	}

	if id, ok := x.Right.(*ast.Ident); ok {
		tag := c.source.GetString(id.Span())
		typ, ok := enum[tag]
		if !ok {
			c.bail(id.Span(),
				fmt.Sprintf("#%s isn't a valid option for enum %s",
					tag, c.reg.String(ref)))
		}

		// We expect no value.
		if typ == NeverRef {
			// But there was one.
			if val != nil {
				c.bail(val.Span(), fmt.Sprintf("#%s doesn't take any value", tag))
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
func (c *context) pend(singleX, listX ast.Expr, single, list TypeRef) TypeRef {
	// Special-case bytes.
	if single == ByteRef || list == BytesRef {
		c.ensure(singleX, single, ByteRef)
		c.ensure(listX, list, BytesRef)
		return BytesRef
	}

	c.ensure(singleX, c.reg.List(single), list)
	return list
}
