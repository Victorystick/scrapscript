package types

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/token"
)

// An alias is a sort binding: a type, or a type constructor whose parameters
// are the variables of its leading =>s.
type alias struct {
	name       string
	node       ast.Node // the binding, for errors
	annotation ast.Node // its kind, or None
	params     []param
	body       ast.Node // the type after the parameters
	scope      *Scope[*alias]

	// Whether the kinds of the parameters are known, or being found.
	kinded, kinding bool

	// The type of an alias without parameters, computed on first use.
	ref     TypeRef
	done    bool
	pending bool
}

// A param is a type constructor's parameter. Its kind is row if it is used
// as a row tail, and type otherwise.
type param struct {
	name string
	row  bool
}

// A tvar is a type variable in scope in a type expression.
type tvar struct {
	ref TypeRef
	row bool
}

// sortBinding returns the alias that a sort binding of name to t defines,
// with the kind annotation, if any.
func (c *context) sortBinding(b ast.Node, name string, annotation, t ast.Node) *alias {
	a := &alias{name: name, node: b, annotation: annotation}
	a.params, a.body = c.params(t)
	return a
}

// kinds finds the kinds of the alias's parameters, once the aliases it may
// use are in its scope, and checks them against its annotation, if any.
func (c *context) kinds(a *alias) {
	if a.kinded || a.kinding {
		// A recursive alias fails once expanded.
		return
	}
	a.kinding = true
	aliases := c.aliases
	c.aliases = a.scope
	c.markRows(a.params, a.body)
	c.aliases = aliases
	a.kinding, a.kinded = false, true

	if a.annotation != ast.None {
		if got, want := c.kind(a.annotation), a.kind(); got != want {
			c.bail(a.annotation, fmt.Sprintf("%s has kind %s, not %s", a.name, want, got))
		}
	}
}

// params returns the type variables that t's leading =>s bind, and the type
// after them. Their kinds are found by markRows.
func (c *context) params(t ast.Node) ([]param, ast.Node) {
	var params []param
	for c.src.Kind(t) == ast.Forall {
		tok, body := c.src.Forall(t)
		p := c.src.TokenText(tok)
		for _, q := range params {
			if q.name == p {
				c.bail(t, fmt.Sprintf("cannot bind type variable %s twice", p))
			}
		}
		params = append(params, param{name: p})
		t = body
	}
	return params, t
}

// annotation returns the type that a value binding's annotation x declares,
// with each of its type variables replaced by fresh(name).
func (c *context) annotation(x ast.Node, fresh func(name string) TypeRef) TypeRef {
	params, t := c.params(x)
	c.markRows(params, t)
	tvars := c.tvars
	for _, p := range params {
		c.tvars = c.tvars.Bind(p.name, tvar{fresh(p.name), p.row})
	}
	ref := c.typ(t)
	c.tvars = tvars
	return ref
}

// kind returns the kind of the alias, such as `type -> type`.
func (a *alias) kind() string {
	var b strings.Builder
	for _, p := range a.params {
		if p.row {
			b.WriteString("row -> ")
		} else {
			b.WriteString("type -> ")
		}
	}
	b.WriteString("type")
	return b.String()
}

// kind returns the kind that the annotation x denotes. Parameters may be
// types or rows, and the result is a type.
func (c *context) kind(x ast.Node) string {
	var b strings.Builder
	for c.src.Kind(x) == ast.Func {
		arg, result := c.src.Func(x)
		switch k := c.src.NodeText(arg); {
		case c.src.Kind(arg) == ast.Func:
			c.bail(arg, "higher kinds are not supported yet")
		case c.src.Kind(arg) != ast.Ident || k != "type" && k != "row":
			c.bail(arg, fmt.Sprintf("%s is not a kind", k))
		default:
			b.WriteString(k)
			b.WriteString(" -> ")
		}
		x = result
	}
	if c.src.Kind(x) != ast.Ident || c.src.NodeText(x) != "type" {
		c.bail(x, fmt.Sprintf("%s is not a kind of types", c.src.NodeText(x)))
	}
	b.WriteString("type")
	return b.String()
}

// markRows marks the params that t uses as rows: as row tails, or as the
// arguments of an alias's row parameters.
func (c *context) markRows(params []param, t ast.Node) {
	index := func(x ast.Node) int {
		if c.src.Kind(x) != ast.Ident {
			return -1
		}
		name := c.src.TokenText(c.src.Main(x))
		return slices.IndexFunc(params, func(p param) bool { return p.name == name })
	}
	mark := func(x ast.Node) {
		if i := index(x); i >= 0 {
			params[i].row = true
		}
	}
	switch c.src.Kind(t) {
	case ast.Call:
		fn, args := c.typeApp(t)
		var a *alias
		if c.src.Kind(fn) == ast.Ident && index(fn) < 0 {
			a = c.aliases.Lookup(c.src.TokenText(c.src.Main(fn)))
		}
		if a != nil {
			c.kinds(a)
		}
		for i, arg := range args {
			if a != nil && i < len(a.params) && a.params[i].row {
				mark(arg)
			}
			c.markRows(params, arg)
		}
	case ast.Func:
		arg, result := c.src.Func(t)
		c.markRows(params, arg)
		c.markRows(params, result)
	case ast.RecordType:
		tail, fields := c.src.Record(t)
		mark(tail)
		for i := range fields.Len() {
			c.markRows(params, fields.Value(i))
		}
	case ast.Enum:
		tail, variants := c.src.Enum(t)
		mark(tail)
		for _, v := range variants {
			_, payload := c.src.Variant(v)
			c.markRows(params, payload)
		}
	}
}

// checkAlias checks that the alias denotes a valid type, applying a type
// constructor to fresh variables.
func (c *context) checkAlias(a *alias) {
	c.kinds(a)
	if len(a.params) == 0 {
		c.alias(a.node, a)
		return
	}
	refs := make([]TypeRef, len(a.params))
	for i := range refs {
		refs[i] = c.reg.Var()
	}
	c.expand(a.node, a, refs)
}

var primitiveTypes = map[string]TypeRef{
	"int":   IntRef,
	"float": FloatRef,
	"text":  TextRef,
	"byte":  ByteRef,
	"bytes": BytesRef,
}

// typ returns the type that the type expression x denotes. Names are looked
// up among the type variables, then the aliases, then the builtins.
func (c *context) typ(x ast.Node) TypeRef {
	switch c.src.Kind(x) {
	case ast.Ident:
		name := c.src.TokenText(c.src.Main(x))
		if v := c.tvars.Get(name); v != nil {
			if v.val.row {
				c.bail(x, fmt.Sprintf("%s is a row, not a type", name))
			}
			return v.val.ref
		}
		if a := c.aliases.Lookup(name); a != nil {
			return c.apply(x, a, nil)
		}
		if ref, ok := primitiveTypes[name]; ok {
			return ref
		}
		if name == "list" {
			c.bail(x, fmt.Sprintf("list takes %s, not 0", typeArguments(1)))
		}
		c.bail(x, fmt.Sprintf("unknown type %s", name))
	case ast.Import:
		if ref, ok := c.reg.Denoted(c.imported(x)); ok {
			return ref
		}
		c.bail(x, fmt.Sprintf("%s is a value, not a type", c.src.NodeText(x)))
	case ast.Literal:
		if c.src.LiteralKind(x) == token.HOLE {
			return HoleRef
		}
	case ast.Call:
		return c.application(x)
	case ast.Func:
		arg, body := c.src.Func(x)
		return c.reg.Func(c.typ(arg), c.typ(body))
	case ast.Enum:
		tail, variants := c.src.Enum(x)
		tags := make(map[string]TypeRef)
		for _, v := range variants {
			tagTok, payload := c.src.Variant(v)
			tag := c.src.TokenText(tagTok)
			if _, ok := tags[tag]; ok {
				c.bail(v, fmt.Sprintf("cannot define tag #%s more than once", tag))
			}
			tags[tag] = HoleRef
			if payload != ast.None {
				tags[tag] = c.typ(payload)
			}
		}
		return c.reg.add(node{kind: enumKind, a: c.reg.row(tags, c.rowTail(tail))})
	case ast.RecordType:
		tail, fields := c.src.Record(x)
		ref := make(map[string]TypeRef, fields.Len())
		for i := range fields.Len() {
			k := c.src.TokenText(fields.Key(i))
			if _, ok := ref[k]; ok {
				c.bail(fields.Value(i), fmt.Sprintf("cannot define field %s more than once", k))
			}
			ref[k] = c.typ(fields.Value(i))
		}
		return c.reg.add(node{kind: recordKind, a: c.reg.row(ref, c.rowTail(tail))})
	case ast.Forall:
		c.bail(x, "type variables must be bound at the start of an annotation")
	}

	c.bail(x, fmt.Sprintf("%s is not a type", c.src.NodeText(x)))
	return NoType
}

// rowTail returns the row that tail names, or the empty row if it is None.
func (c *context) rowTail(tail ast.Node) TypeRef {
	if tail == ast.None {
		return emptyRef
	}
	name := c.src.TokenText(c.src.Main(tail))
	v := c.tvars.Get(name)
	if v == nil {
		c.bail(tail, fmt.Sprintf("unknown row variable %s", name))
	}
	if !v.val.row {
		c.bail(tail, fmt.Sprintf("%s is a type, not a row", name))
	}
	return v.val.ref
}

// typeApp returns the type constructor and arguments of an application.
func (c *context) typeApp(x ast.Node) (fn ast.Node, args []ast.Node) {
	fn = x
	for c.src.Kind(fn) == ast.Call {
		f, arg := c.src.Call(fn)
		args = append(args, arg)
		fn = f
	}
	slices.Reverse(args)
	return fn, args
}

// application returns the type that the type application x denotes.
func (c *context) application(x ast.Node) TypeRef {
	fn, args := c.typeApp(x)

	if c.src.Kind(fn) == ast.Ident {
		name := c.src.TokenText(c.src.Main(fn))
		if c.tvars.Get(name) == nil {
			if a := c.aliases.Lookup(name); a != nil {
				return c.apply(x, a, args)
			}
			if name == "list" {
				if len(args) != 1 {
					c.bail(x, fmt.Sprintf("list takes %s, not %d", typeArguments(1), len(args)))
				}
				return c.reg.List(c.typ(args[0]))
			}
		}
	}
	c.bail(fn, fmt.Sprintf("%s is not a type constructor", c.src.NodeText(fn)))
	return NoType
}

// apply returns the type that the alias a denotes, applied to args.
func (c *context) apply(x ast.Node, a *alias, args []ast.Node) TypeRef {
	if len(args) != len(a.params) {
		if len(a.params) == 0 {
			c.bail(x, fmt.Sprintf("%s is not a type constructor", a.name))
		}
		c.bail(x, fmt.Sprintf("%s takes %s, not %d", a.name, typeArguments(len(a.params)), len(args)))
	}
	if len(args) == 0 {
		return c.alias(x, a)
	}
	refs := make([]TypeRef, len(args))
	for i, p := range a.params {
		if !p.row {
			refs[i] = c.typ(args[i])
			continue
		}
		arg := args[i]
		if c.src.Kind(arg) == ast.Ident {
			if v := c.tvars.Get(c.src.TokenText(c.src.Main(arg))); v != nil && v.val.row {
				refs[i] = v.val.ref
				continue
			}
		}
		c.bail(arg, fmt.Sprintf("%s is not a row variable, as %s's parameter %s must be",
			c.src.NodeText(arg), a.name, p.name))
	}
	return c.expand(x, a, refs)
}

func typeArguments(n int) string {
	if n == 1 {
		return "1 type argument"
	}
	return fmt.Sprintf("%d type arguments", n)
}

// alias returns the type of an alias without parameters, computing it on
// first use. It has no type variables, so its type can be shared.
func (c *context) alias(x ast.Node, a *alias) TypeRef {
	if !a.done {
		a.ref = c.expand(x, a, nil)
		a.done = true
	}
	return a.ref
}

// expand returns the type of the alias's body, in the scope of its binding,
// with its parameters bound to refs.
func (c *context) expand(x ast.Node, a *alias, refs []TypeRef) TypeRef {
	if a.pending {
		c.bail(x, fmt.Sprintf("recursive type %s is not supported yet", a.name))
	}
	a.pending = true
	aliases, tvars := c.aliases, c.tvars
	c.aliases, c.tvars = a.scope, nil
	for i, p := range a.params {
		c.tvars = c.tvars.Bind(p.name, tvar{refs[i], p.row})
	}
	ref := c.typ(a.body)
	c.aliases, c.tvars = aliases, tvars
	a.pending = false
	return ref
}
