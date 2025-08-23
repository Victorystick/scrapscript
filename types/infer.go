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

// Unbinds the last bound variable.
func (c *context) unbind() {
	c.scope = c.scope.parent
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

	_, ref = context.infer(se.Expr)
	return ref, err
}

func (c *context) infer(expr ast.Expr) (Subst, TypeRef) {
	switch x := expr.(type) {
	case *ast.Literal:
		return nil, literalTypeRef(x.Kind)
	case *ast.Ident:
		name := c.source.GetString(x.Pos)
		ref := c.scope.Lookup(name)
		if ref == NeverRef {
			c.bail(x.Pos, "unbound variable: "+name)
		}
		return nil, c.reg.Instantiate(ref)
	case *ast.WhereExpr:
		return c.where(x)
	case *ast.ListExpr:
		return c.list(x)
	case *ast.RecordExpr:
		return c.record(x)
	case ast.EnumExpr:
		// 	return nil, c.enum(x)
		return nil, NeverRef

	case *ast.FuncExpr:
		// Not sure how to juggle vars vs unbound. :/
		binder := c.reg.Var()
		c.bind(c.source.GetString(x.Arg.Span()), binder)
		defer c.unbind()
		subs, ret := c.infer(x.Body)
		return subs, c.reg.Func(c.reg.substitute(binder, subs), ret)

	case *ast.CallExpr:
		// return nil, NeverRef
		// 	// Special-case pick with a value.
		// 	if pick, ok := x.Fn.(*ast.BinaryExpr); ok && pick.Op == token.PICK {
		// 		return nil, c.pick(pick, x.Arg)
		// 	}

		res := c.reg.Var()
		s1, fn := c.infer(x.Fn)
		s2, arg := c.infer(x.Arg)
		s3 := c.reg.Unify(c.reg.substitute(fn, s2), c.reg.Func(arg, res))
		s4 := c.reg.Compose(s3, c.reg.Compose(s2, s1))
		return s4, c.reg.substitute(res, s3)

	case *ast.BinaryExpr:
		_, left := c.infer(x.Left)
		_, right := c.infer(x.Right)
		switch x.Op {
		// 	case token.PICK:
		// 		return c.pick(x, nil)
		// 	case token.CONCAT:
		// 		if left == TextRef {
		// 			if right == TextRef {
		// 				return TextRef
		// 			} else if right.IsUnbound() {

		// 			} else {
		// 				return NeverRef
		// 			}
		// 		}
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

func (c *context) ensure(x ast.Expr, got, want TypeRef) (Subst, TypeRef) {
	if got == want {
		return nil, got
	}

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

	return c.reg.Unify(got, want), want
}

func (c *context) where(x *ast.WhereExpr) (Subst, TypeRef) {
	name := c.source.GetString(x.Id.Pos)

	s1, tyVal := c.infer(x.Val)

	// If there's an annotation, make sure it matches the inferred type.
	if x.Typ != nil {
		s2, _ := c.ensure(x.Typ, tyVal, c.typ(x.Typ))
		c.reg.apply(s2)
	}

	c.bind(name, tyVal)
	defer c.unbind()
	c.reg.apply(s1) // Apply anything learned about vars.
	s2, tyExpr := c.infer(x.Expr)
	return c.reg.Compose(s1, s2), tyExpr
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

func (c *context) list(x *ast.ListExpr) (Subst, TypeRef) {
	var sub Subst
	res := NeverRef

	for _, v := range x.Elements {
		s, typ := c.infer(v)
		sub = c.reg.Compose(s, sub)

		if res == NeverRef {
			res = typ
			continue
		}

		s, res = c.ensure(v, res, typ)
		sub = c.reg.Compose(s, sub)
	}

	if res == NeverRef {
		res = c.reg.Var()
	}
	return sub, c.reg.List(res)
}

func (c *context) record(x *ast.RecordExpr) (Subst, TypeRef) {
	// If there is a rest/spread, our type is equal to that.
	// if x.Rest != nil {
	// 	rest := c.infer(x.Rest)
	// 	rec := c.reg.GetRecord(rest)
	// 	if rec == nil {
	// 		c.bail(x.Rest.Span(), fmt.Sprintf("cannot spread from non-record type %s", c.reg.String(rest)))
	// 	}
	// 	for k, v := range x.Entries {
	// 		expected, ok := rec[k]
	// 		if !ok {
	// 			c.bail(v.Span(), fmt.Sprintf("cannot set %s not in the base record", k))

	// 		}
	// 		actual := c.infer(v)
	// 		if actual != expected {
	// 			c.bail(v.Span(), fmt.Sprintf("type of %s must be %s, not %s", k, c.reg.String(expected), c.reg.String(actual)))
	// 		}
	// 	}
	// 	return s, rest
	// }

	var s, s2 Subst
	ref := make(MapRef, len(x.Entries))
	for k, v := range x.Entries {
		s2, ref[k] = c.infer(v)
		s = c.reg.Compose(s, s2)
	}
	return s, c.reg.Record(ref)
}

// func (c *context) enum(x ast.EnumExpr) TypeRef {
// 	ref := make(MapRef, len(x))
// 	for _, v := range x {
// 		name := c.source.GetString(v.Tag.Pos)
// 		vRef := NeverRef
// 		if v.Typ != nil {
// 			vRef = c.infer(v.Typ)
// 		}
// 		ref[name] = vRef
// 	}
// 	return c.reg.Enum(ref)
// }

// func (c *context) pick(x *ast.BinaryExpr, val ast.Expr) TypeRef {
// 	// TODO: A binary expr for pick is annoying.
// 	name := c.source.GetString(x.Left.Span())
// 	ref := c.scope.Lookup(name)
// 	enum := c.reg.GetEnum(ref)
// 	if enum == nil {
// 		c.bail(x.Left.Span(), fmt.Sprintf("%s isn't an enum", name))
// 	}

// 	if id, ok := x.Right.(*ast.Ident); ok {
// 		tag := c.source.GetString(id.Span())
// 		typ, ok := enum[tag]
// 		if !ok {
// 			c.bail(id.Span(),
// 				fmt.Sprintf("#%s isn't a valid option for enum %s",
// 					tag, c.reg.String(ref)))
// 		}

// 		// We expect no value.
// 		if typ == NeverRef {
// 			// But there was one.
// 			if val != nil {
// 				c.bail(val.Span(), fmt.Sprintf("#%s doesn't take any value", tag))
// 			}
// 		} else {
// 			valRef := c.infer(val)
// 			// TODO: check assignability instead
// 			if typ != valRef {
// 				// Wrong type.
// 				c.bail(val.Span(),
// 					fmt.Sprintf("cannot assign %s to #%s which needs %s",
// 						c.reg.String(valRef), tag, c.reg.String(typ)))
// 				return NeverRef
// 			}
// 		}

// 		return ref
// 	}

// 	// TODO: better error handling?
// 	return NeverRef
// }

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
