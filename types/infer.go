package types

import (
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

func (s *Scope[T]) Rebind(name string, val T) bool {
	if bound := s.Get(name); bound != nil {
		bound.val = val
		return true
	}
	return false
}

type TypeScope = *Scope[TypeRef]

type context struct {
	source token.Source
	reg    *Registry
	scope  TypeScope
}

func (c *context) bail(span token.Span, msg string) {
	panic(c.source.Error(span, msg))
}

func (c *context) bind(name string, ref TypeRef) TypeScope {
	c.scope = c.scope.Bind(name, ref)
	return c.scope
}

func Infer(se ast.SourceExpr) (string, error) {
	var reg Registry
	var scope TypeScope

	for _, p := range primitives {
		scope = scope.Bind(reg.String(p), p)
	}

	ref, err := InferInScope(&reg, scope, se)
	if err != nil {
		return "", err
	}
	return reg.String(ref), nil
}

func InferInScope(reg *Registry, scope TypeScope, se ast.SourceExpr) (ref TypeRef, err error) {
	context := context{
		source: se.Source,
		reg:    reg,
		scope:  scope,
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

	return context.infer(se.Expr), err
}

func (c *context) infer(expr ast.Expr) TypeRef {
	switch x := expr.(type) {
	case *ast.Literal:
		return literalTypeRef(x.Kind)
	case *ast.Ident:
		return c.scope.Lookup(c.source.GetString(x.Pos))
	case *ast.WhereExpr:
		return c.where(x)
	case *ast.ListExpr:
		return c.list(x)
	case *ast.RecordExpr:
		return c.record(x)
	case ast.EnumExpr:
		return c.enum(x)
	case *ast.FuncExpr:
		unbound := c.reg.Unbound()
		// Hold onto the binding, in case inferring the body rebinds its type.
		binding := c.bind(c.source.GetString(x.Arg.Span()), unbound)
		ret := c.infer(x.Body)
		return c.reg.Func(binding.val, ret)
	case *ast.CallExpr:
		// Special-case pick with a value.
		if pick, ok := x.Fn.(*ast.BinaryExpr); ok && pick.Op == token.PICK {
			return c.pick(pick, x.Arg)
		}

		typ := c.infer(x.Fn)
		arg := c.infer(x.Arg)

		if !typ.IsFunction() {
			// If the argument is an unbound identifier, rebind it.
			id, ok := x.Fn.(*ast.Ident)
			if ok && typ.IsUnbound() {
				name := c.source.GetString(id.Pos)
				// Let's steal the now unused (?) unbound.
				fn := c.reg.Func(arg, typ)
				if c.scope.Rebind(name, fn) {
					return typ
				}

				// Let's try to rebind a type.
			}

			c.bail(x.Span(), fmt.Sprintf("cannot call non-function %s", c.reg.String(typ)))
		}
		fn := c.reg.GetFunc(typ)

		ref := c.call(fn, arg)
		if ref != NeverRef {
			return ref
		}

		c.bail(x.Span(), fmt.Sprintf("cannot call %s with %s", c.reg.String(typ), c.reg.String(arg)))

	case *ast.BinaryExpr:
		left := c.infer(x.Left)
		right := c.infer(x.Right)
		switch x.Op {
		case token.PICK:
			return c.pick(x, nil)
		case token.CONCAT:
			if left == TextRef {
				if right == TextRef {
					return TextRef
				} else if right.IsUnbound() {

				} else {
					return NeverRef
				}
			}
		case token.ADD:
			if left == IntRef {
				return c.ensure(x.Right, right, IntRef)
			}
			if right == IntRef {
				return c.ensure(x.Left, left, IntRef)
			}
		}
		panic(fmt.Sprintf("can't infer binary expression %s", x.Op.String()))
	}

	panic(fmt.Sprintf("can't infer node %T", expr))
}

func (c *context) ensure(x ast.Expr, got, want TypeRef) TypeRef {
	if got == want {
		return got
	}

	if got.IsUnbound() {
		c.rebind(x, want)
		return want
	}

	if c.isAssignable(want, got) {
		return got
	}

	c.bail(x.Span(), fmt.Sprintf("expected %s, got %s", c.reg.String(want), c.reg.String(got)))
	return NeverRef
}

func (c *context) call(fn FuncRef, arg TypeRef) TypeRef {
	if c.isAssignable(fn.Arg, arg) {
		return fn.Result
	}

	if fn.Arg.IsUnbound() {
		return c.reg.Bind(fn.Result, fn.Arg, arg)
	}

	if fn.Arg.IsFunction() && arg.IsFunction() {
		afn := c.reg.GetFunc(fn.Arg)
		bfn := c.reg.GetFunc(arg)

		// If completely unbound, replace with arg.
		if afn.Arg.IsUnbound() && afn.Result.IsUnbound() {
			res := c.reg.Bind(fn.Result, afn.Result, bfn.Result)
			return c.reg.Bind(res, afn.Arg, bfn.Arg)
		}
	}

	return NeverRef
}

func (c *context) isAssignable(a, b TypeRef) bool {
	if a == b {
		return true
	}

	aTag, _ := a.extract()
	switch aTag {
	case listTag:
		if b.IsList() && c.reg.GetList(b).IsUnbound() {
			return true
		}
	}

	return false
}

func (c *context) where(x *ast.WhereExpr) TypeRef {
	name := c.source.GetString(x.Id.Pos)
	if x.Typ == nil {
		// If there is no type annotation, we can infer it from the value.
		c.bind(name, c.infer(x.Val))
		return c.infer(x.Expr)
	}

	tRef := c.typ(x.Typ)
	vRef := c.infer(x.Val)
	if tRef != vRef {
		c.bail(x.Val.Span(), fmt.Sprintf("cannot assign %s to %s", c.reg.String(vRef), c.reg.String(tRef)))
	}

	c.bind(name, tRef)
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
	}

	c.bail(x.Span(), fmt.Sprintf("cannot infer type of %T", x))
	return NeverRef
}

func (c *context) list(x *ast.ListExpr) (res TypeRef) {
	for _, v := range x.Elements {
		typ := c.infer(v)
		if typ == res {
			continue
		} else if res == NeverRef {
			res = typ
		} else if typ.IsUnbound() {
			c.rebind(v, res)
		} else {
			c.bail(v.Span(), "list elements must all be of type "+c.reg.String(res))
			// Bad list.
			return NeverRef
		}
	}
	if res == NeverRef {
		res = c.reg.Unbound()
	}
	return c.reg.List(res)
}

// Re-binds the type of expresion x, or fails.
func (c *context) rebind(x ast.Expr, ref TypeRef) {
	name := c.source.GetString(x.Span())
	_, ok := x.(*ast.Ident)
	if ok {
		s := c.scope.Get(name)
		if s != nil {
			// Let's steal the now unused (?) generic.
			s.val = ref
			return
		}
	}
	c.bail(x.Span(), fmt.Sprintf("can't rebind type of non-identifier %s", name))
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

func (c *context) enum(x ast.EnumExpr) TypeRef {
	ref := make(MapRef, len(x))
	for _, v := range x {
		name := c.source.GetString(v.Tag.Pos)
		vRef := NeverRef
		if v.Typ != nil {
			vRef = c.infer(v.Typ)
		}
		ref[name] = vRef
	}
	return c.reg.Enum(ref)
}

func (c *context) pick(x *ast.BinaryExpr, val ast.Expr) TypeRef {
	// TODO: A binary expr for pick is annoying.
	name := c.source.GetString(x.Left.Span())
	ref := c.scope.Lookup(name)
	enum := c.reg.GetEnum(ref)
	if enum == nil {
		c.bail(x.Left.Span(), fmt.Sprintf("%s isn't an enum", name))
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
			// TODO: check assignability instead
			if typ != valRef {
				// Wrong type.
				c.bail(val.Span(),
					fmt.Sprintf("cannot assign %s to #%s which needs %s",
						c.reg.String(valRef), tag, c.reg.String(typ)))
				return NeverRef
			}
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
