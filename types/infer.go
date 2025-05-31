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

type TypeScope = *Scope[TypeRef]

type context struct {
	source   token.Source
	reg      *Registry
	scope    TypeScope
	generics int // The number of generics currently in use.
}

func (c *context) bail(span token.Span, msg string) {
	panic(c.source.Error(span, msg))
}

func (c *context) bind(name string, ref TypeRef) TypeScope {
	c.scope = c.scope.Bind(name, ref)
	return c.scope
}

func (c *context) generic() (ref TypeRef) {
	ref = c.reg.Generic(c.generics)
	c.generics += 1
	return
}

func (c *context) ungeneric() {
	c.generics -= 1
}

func Infer(se ast.SourceExpr) (string, error) {
	var reg Registry
	var scope TypeScope

	for _, p := range primitives {
		scope = scope.Bind(reg.String(p), p)
	}

	ref, err := InferInScope(&reg, scope, se)
	if err != nil {
		return "", nil
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
	case ast.TypeExpr:
		return c.enum(x)
	case *ast.FuncExpr:
		// Must track the depth of generics.
		generic := c.generic()
		defer c.ungeneric()
		// Hold onto the binding, in case inferring the body rebinds its type.
		binding := c.bind(c.source.GetString(x.Arg.Span()), generic)
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
			id, ok := x.Fn.(*ast.Ident)
			if ok && typ.IsGeneric() {
				name := c.source.GetString(id.Pos)
				s := c.scope.Get(name)
				if s != nil {
					// Let's steal the now unused (?) generic.
					s.val = c.reg.Func(arg, typ)
					// fmt.Fprintln(os.Stderr, "name", name, "is", c.reg.String(s.val))
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
				} else if right.IsGeneric() {

				} else {
					return NeverRef
				}
			}
		}
		panic(fmt.Sprintf("can't infer binary expression %s", x.Op.String()))
	}

	panic(fmt.Sprintf("can't infer node %T", expr))
}

func (c *context) call(fn FuncRef, arg TypeRef) TypeRef {
	if c.isAssignable(fn.Arg, arg) {
		return fn.Result
	}

	if fn.Arg.IsGeneric() {
		return c.reg.ResolveGeneric(fn.Result, fn.Arg, arg)
	}

	if fn.Arg.IsFunction() && arg.IsFunction() {
		afn := c.reg.GetFunc(fn.Arg)
		bfn := c.reg.GetFunc(arg)

		// If completely generic, replace with arg.
		if afn.Arg.IsGeneric() && afn.Result.IsGeneric() {
			res := c.reg.ResolveGeneric(fn.Result, afn.Result, bfn.Result)
			return c.reg.ResolveGeneric(res, afn.Arg, bfn.Arg)
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
		if b.IsList() && c.reg.GetList(b) == NeverRef {
			return true
		}
	}

	return false
}

func (c *context) where(x *ast.WhereExpr) TypeRef {
	name := c.source.GetString(x.Id.Pos)
	c.bind(name, c.infer(x.Val))
	return c.infer(x.Expr)
}

func (c *context) list(x *ast.ListExpr) (res TypeRef) {
	for _, v := range x.Elements {
		typ := c.infer(v)
		if typ == res {
			continue
		} else if res == NeverRef {
			res = typ
		} else if typ.IsGeneric() {
			c.rebind(v, res)
		} else {
			// Bad list.
			return NeverRef
		}
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
			// TODO: better error handling?
			return NeverRef
		}
		for k, v := range x.Entries {
			if ref, ok := rec[k]; !ok || ref != c.infer(v) {
				return NeverRef
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

func (c *context) enum(x ast.TypeExpr) TypeRef {
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
		// TODO: better error handling?
		return NeverRef
	}

	if id, ok := x.Right.(*ast.Ident); ok {
		tag := c.source.GetString(id.Span())
		typ, ok := enum[tag]
		if !ok {
			// TODO: better error handling?
			return NeverRef
		}

		// We expect no value.
		if typ == NeverRef {
			// But there was one.
			if val != nil {
				return NeverRef
			}
		} else if typ != c.infer(val) {
			// Wrong type.
			return NeverRef
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
