package types

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/token"
)

// An alias is a sort binding: a type, or a type constructor whose parameters
// are the variables of its leading =>s. With value set, it is instead a value
// binding, which takes its name in type position, and whose static part
// paths may reach.
type alias struct {
	name       string
	node       ast.Node // the binding, for errors
	annotation ast.Node // its kind, or None
	params     []param
	body       ast.Node // the type after the parameters
	scope      *Scope[*alias]

	// A value binding's value, and its static part, once resolved.
	value    ast.Node
	static   static
	resolved bool

	// Whether the kinds of the parameters are known, or being found.
	kinded, kinding bool

	// The type of an alias without parameters, computed on first use.
	ref  TypeRef
	done bool

	// While the alias is being expanded: the arguments it is expanded with,
	// the payload depth it started at, and the node that a recursive use of
	// it refers back to, once there is one.
	pending bool
	args    []TypeRef
	depth   int
	back    TypeRef

	// Its definition, which its types print by, once it has one.
	def *typeDef
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
		// A recursive alias: its parameters' kinds are being found.
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
		// The kinds of the constructor's parameters, if it is known.
		var rows []bool
		var s static
		switch c.src.Kind(fn) {
		case ast.Ident:
			if index(fn) < 0 {
				s = c.static(fn)
			}
		case ast.Import, ast.Access:
			s = c.static(fn)
		}
		switch {
		case s.alias != nil:
			c.kinds(s.alias)
			for _, p := range s.alias.params {
				rows = append(rows, p.row)
			}
		case s.isType():
			for _, p := range s.imported.Params {
				rows = append(rows, p.Row)
			}
		}
		for i, arg := range args {
			if i < len(rows) && rows[i] {
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
		if !c.static(x).isNone() {
			return c.applyStatic(x, x, nil)
		}
		if ref, ok := primitiveTypes[name]; ok {
			return ref
		}
		if name == "list" {
			c.bail(x, fmt.Sprintf("list takes %s, not 0", typeArguments(1)))
		}
		c.bail(x, fmt.Sprintf("unknown type %s", name))
	case ast.Import, ast.Access:
		return c.applyStatic(x, x, nil)
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
				c.payloads++
				tags[tag] = c.typ(payload)
				c.payloads--
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

	switch c.src.Kind(fn) {
	case ast.Import, ast.Access:
		return c.applyStatic(x, fn, args)
	case ast.Ident:
		name := c.src.TokenText(c.src.Main(fn))
		if c.tvars.Get(name) == nil {
			if !c.static(fn).isNone() {
				return c.applyStatic(x, fn, args)
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
	rows := make([]bool, len(a.params))
	for i, p := range a.params {
		rows[i] = p.row
	}
	return c.expand(x, a, c.typeArgs(a.name, rows, args))
}

// typeArgs returns the types of the arguments to the constructor name,
// whose parameters are rows where rows says so. A row argument must be a
// row variable.
func (c *context) typeArgs(name string, rows []bool, args []ast.Node) []TypeRef {
	refs := make([]TypeRef, len(args))
	for i, arg := range args {
		if !rows[i] {
			refs[i] = c.typ(arg)
			continue
		}
		if c.src.Kind(arg) == ast.Ident {
			if v := c.tvars.Get(c.src.TokenText(c.src.Main(arg))); v != nil && v.val.row {
				refs[i] = v.val.ref
				continue
			}
		}
		c.bail(arg, fmt.Sprintf("type argument %d of %s must be a row variable, not %s",
			i+1, name, c.src.NodeText(arg)))
	}
	return refs
}

// applyStatic returns the type that the path fn denotes, applied to args if
// it is a type constructor.
func (c *context) applyStatic(x, fn ast.Node, args []ast.Node) TypeRef {
	s := c.constructor(fn)
	if s.alias != nil {
		return c.apply(x, s.alias, args)
	}
	return c.applyResult(x, c.src.NodeText(fn), *s.imported, args)
}

// applyResult returns the type that the imported type res, written name,
// denotes, applied to args if it is a type constructor.
func (c *context) applyResult(x ast.Node, name string, res Result, args []ast.Node) TypeRef {
	if len(args) != len(res.Params) {
		if len(res.Params) == 0 {
			c.bail(x, fmt.Sprintf("%s is not a type constructor", name))
		}
		c.bail(x, fmt.Sprintf("%s takes %s, not %d", name, typeArguments(len(res.Params)), len(args)))
	}
	if len(args) == 0 {
		return c.reg.reference(name, res.Type, nil)
	}
	vars := make([]TypeRef, len(res.Params))
	rows := make([]bool, len(res.Params))
	for i, p := range res.Params {
		vars[i], rows[i] = p.Var, p.Row
	}
	refs := c.typeArgs(name, rows, args)
	return c.reg.reference(name, c.reg.apply(vars, res.Type, refs), refs)
}

// export returns the result of a scrap whose body names the alias a: the
// type it denotes, or for a type constructor, its body with generalized
// variables for its parameters.
func (c *context) export(x ast.Node, a *alias) Result {
	if len(a.params) == 0 {
		return Result{Type: c.alias(x, a), Sort: true}
	}
	params := make([]Param, len(a.params))
	refs := make([]TypeRef, len(a.params))
	for i, p := range a.params {
		refs[i] = c.reg.Gen()
		params[i] = Param{Name: p.name, Var: refs[i], Row: p.row}
	}
	return Result{Type: c.expand(x, a, refs), Sort: true, Params: params}
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
	if a.done {
		return a.ref
	}
	ref := c.expand(x, a, nil)
	// A recursive use only refers back to the type being expanded.
	if !a.pending {
		a.ref, a.done = ref, true
	}
	return ref
}

// expand returns the type of the alias's body, in the scope of its binding,
// with its parameters bound to refs.
//
// A use of the alias within its own expansion makes it a recursive type: it
// refers back to the type being expanded, so that the type is a cycle in the
// registry (SPEC.md section 7.5). It must recur within a tag's payload, and
// apply the alias to the same arguments, or the type would be infinite.
func (c *context) expand(x ast.Node, a *alias, refs []TypeRef) TypeRef {
	if a.pending {
		return c.recur(x, a, refs)
	}
	a.pending, a.args, a.depth, a.back = true, refs, c.payloads, NoType
	c.expanding = append(c.expanding, a)
	before := c.reg.next()
	aliases, tvars := c.aliases, c.tvars
	c.aliases, c.tvars = a.scope, nil
	for i, p := range a.params {
		c.tvars = c.tvars.Bind(p.name, tvar{refs[i], p.row})
	}
	ref := c.typ(a.body)
	c.aliases, c.tvars = aliases, tvars
	c.expanding = c.expanding[:len(c.expanding)-1]
	back := a.back
	a.pending, a.args = false, nil
	if back != NoType {
		c.reg.link(back, ref)
	}
	// The type prints by the alias's name, unless it is a type that
	// existed before, such as a primitive or another alias's type, or an
	// alias in its body named it first.
	if _, named := c.reg.names[ref]; ref >= before && !named {
		c.reg.name(ref, typeName{c.define(x, a, ref), refs})
	}
	return ref
}

// define returns the definition of the alias a, whose type for refs is ref.
func (c *context) define(x ast.Node, a *alias, ref TypeRef) *typeDef {
	if a.def != nil {
		return a.def
	}
	a.def = &typeDef{name: a.name, body: ref}
	if len(a.params) > 0 {
		// The definition has generalized variables for its parameters.
		a.def.params = make([]TypeRef, len(a.params))
		for i := range a.params {
			a.def.params[i] = c.reg.Gen()
		}
		a.def.body = c.independently(func() TypeRef { return c.expand(x, a, a.def.params) })
	}
	return a.def
}

// independently returns f's type as if no alias were being expanded, so
// that a use of one in f expands it afresh rather than recurring with
// arguments that f's caller chose. Mutually recursive aliases define one
// another this way, each with its own generalized parameters.
func (c *context) independently(f func() TypeRef) TypeRef {
	type state struct {
		args  []TypeRef
		depth int
		back  TypeRef
	}
	outer := c.expanding
	saved := make([]state, len(outer))
	for i, a := range outer {
		saved[i] = state{a.args, a.depth, a.back}
		a.pending = false
	}
	c.expanding = nil
	ref := f()
	c.expanding = outer
	for i, a := range outer {
		a.pending, a.args, a.depth, a.back = true, saved[i].args, saved[i].depth, saved[i].back
	}
	return ref
}

// recur returns the type that a recursive use x of the alias a, applied to
// refs, refers back to.
func (c *context) recur(x ast.Node, a *alias, refs []TypeRef) TypeRef {
	if c.payloads == a.depth {
		c.bail(x, fmt.Sprintf("recursive type %s must recur within a tag's payload", a.name))
	}
	for i, ref := range refs {
		if c.reg.Resolve(ref) != c.reg.Resolve(a.args[i]) {
			c.bail(x, fmt.Sprintf("recursive type %s must recur with the same type arguments", a.name))
		}
	}
	if a.back == NoType {
		a.back = c.reg.Var()
	}
	return a.back
}
