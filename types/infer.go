package types

import (
	"fmt"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/token"
)

type ExprTypes map[ast.Expr]TypeRef

type Scope[T any] struct {
	parent *Scope[T]
	name   string
	val    T
}

func (s *Scope[T]) Lookup(name string) (res T) {
	for s != nil {
		if s.name == name {
			return s.val
		}
		s = s.parent
	}
	return
}

func (s *Scope[T]) Bind(name string, val T) *Scope[T] {
	return &Scope[T]{s, name, val}
}

type context struct {
	source   token.Source
	reg      Registry
	types    ExprTypes
	scope    *Scope[TypeRef]
	generics int // The number of generics currently in use.
}

func (c *context) bind(name string, ref TypeRef) {
	c.scope = c.scope.Bind(name, ref)
}

func (c *context) generic() (ref TypeRef) {
	ref = c.reg.Generic(c.generics)
	c.generics += 1
	return
}

func (c *context) ungeneric() {
	c.generics -= 1
}

func Infer(se ast.SourceExpr) (Registry, ExprTypes) {
	context := context{
		types:  make(ExprTypes),
		source: se.Source,
	}

	for _, p := range primitives {
		context.bind(context.reg.String(p), p)
	}

	context.types[se.Expr] = context.infer(se.Expr)

	return context.reg, context.types
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
		c.bind(c.source.GetString(x.Arg.Span()), generic)
		return c.reg.Func(generic, c.infer(x.Body))
	case *ast.CallExpr:
		// Special-case pick with a value.
		if pick, ok := x.Fn.(*ast.BinaryExpr); ok && pick.Op == token.PICK {
			return c.pick(pick, x.Arg)
		}

		typ := c.infer(x.Fn)
		if !typ.IsFunction() {
			panic(fmt.Sprintf("cannot call non function %s", c.reg.String(typ)))
		}
		fn := c.reg.GetFunc(typ)

		arg := c.infer(x.Arg)
		if fn.Arg == arg {
			return fn.Result
		} else if fn.Arg.IsGeneric() {
			return c.reg.ResolveGeneric(fn.Result, fn.Arg, arg)
		}

		panic("can't infer call expression")
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
		} else {
			// Bad list.
			return NeverRef
		}
	}
	return c.reg.List(res)
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
