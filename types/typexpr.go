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

	// A value binding's value, and where its static part goes once
	// resolved.
	value    ast.Node
	static   *static
	resolved bool

	// Whether the kinds of the parameters are known, or being found.
	kinded, kinding bool

	// The type of an alias without parameters, computed on first use.
	ref  TypeRef
	done bool

	// While the alias is being expanded: the arguments it is expanded with,
	// and the type that a recursive use of it refers back to and the first
	// such use, once there is one.
	pending bool
	args    []TypeRef
	back    TypeRef
	use     ast.Node

	// Its definition, which its types print by, once it has one.
	def *typeDef
}

// A param is a type constructor's parameter. Its kind is row if it is used
// as a row tail, a constructor of arity arguments if it is applied to them,
// and type otherwise.
type param struct {
	name  string
	row   bool
	arity int
}

// kind returns the kind of the parameter, such as `type` or
// `(type -> type)`.
func (p param) kind() string {
	switch {
	case p.row:
		return "row"
	case p.arity > 0:
		return "(" + strings.Repeat("type -> ", p.arity) + "type)"
	}
	return "type"
}

// A tvar is a type variable in scope in a type expression.
type tvar struct {
	ref   TypeRef
	row   bool
	arity int
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
		c.tvars = c.tvars.Bind(p.name, tvar{fresh(p.name), p.row, p.arity})
	}
	ref := c.typ(t)
	c.tvars = tvars
	return ref
}

// kind returns the kind of the alias, such as `type -> type`.
func (a *alias) kind() string {
	var b strings.Builder
	for _, p := range a.params {
		b.WriteString(p.kind())
		b.WriteString(" -> ")
	}
	b.WriteString("type")
	return b.String()
}

// kind returns the kind that the annotation x denotes. Parameters may be
// types, rows or constructors of types, and the result is a type.
func (c *context) kind(x ast.Node) string {
	var b strings.Builder
	for c.src.Kind(x) == ast.Func {
		arg, result := c.src.Func(x)
		switch k := c.src.NodeText(arg); {
		case c.src.Kind(arg) == ast.Func:
			// A constructor of types, as (type -> type).
			b.WriteString(param{arity: c.kindArity(arg)}.kind())
			b.WriteString(" -> ")
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

// kindArity returns the number of types that a constructor of the kind x takes,
// as 1 for `type -> type`.
func (c *context) kindArity(x ast.Node) int {
	n := 0
	for c.src.Kind(x) == ast.Func {
		arg, result := c.src.Func(x)
		if c.src.Kind(arg) != ast.Ident || c.src.NodeText(arg) != "type" {
			c.bail(arg, "a constructor's parameters must be types")
		}
		n++
		x = result
	}
	if c.src.Kind(x) != ast.Ident || c.src.NodeText(x) != "type" {
		c.bail(x, fmt.Sprintf("%s is not a kind of types", c.src.NodeText(x)))
	}
	return n
}

// markRows marks the params that t uses as rows: as row tails, or as the
// arguments of an alias's row parameters. It also finds the arity of those
// that t uses as constructors: applied to types, or as the arguments of an
// alias's constructor parameters.
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
	// constructor marks x as a constructor of n types, if it is a param.
	constructor := func(x, at ast.Node, n int) {
		i := index(x)
		if i < 0 || n == 0 {
			return
		}
		if a := params[i].arity; a != 0 && a != n {
			c.bail(at, fmt.Sprintf("%s takes %s, not %d", params[i].name, typeArguments(a), n))
		}
		params[i].arity = n
	}
	switch c.src.Kind(t) {
	case ast.Call:
		fn, args := c.typeApp(t)
		constructor(fn, t, len(args))
		// The kinds of the constructor's parameters, if it is known.
		var kinds []param
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
			kinds = s.alias.params
		case s.isType():
			for _, p := range s.res.Params {
				kinds = append(kinds, param{row: p.Row, arity: p.Arity})
			}
		}
		for i, arg := range args {
			if i < len(kinds) {
				if kinds[i].row {
					mark(arg)
				}
				constructor(arg, arg, kinds[i].arity)
			}
			c.markRows(params, arg)
		}
	case ast.Forall:
		// A polymorphic field's type, whose variables are its own.
		_, body := c.src.Forall(t)
		c.markRows(params, body)
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
			if v.val.arity > 0 {
				c.bail(x, fmt.Sprintf("%s takes %s, not 0", name, typeArguments(v.val.arity)))
			}
			return v.val.ref
		}
		if s := c.staticName(c.src.TokenBytes(c.src.Main(x))); !s.isNone() {
			return c.applyStatic(x, x, s, nil, nil)
		}
		if ref, ok := primitiveTypes[name]; ok {
			return ref
		}
		if name == "list" {
			c.bail(x, fmt.Sprintf("list takes %s, not 0", typeArguments(1)))
		}
		c.bail(x, fmt.Sprintf("unknown type %s", name))
	case ast.Import, ast.Access:
		return c.applyStatic(x, x, c.static(x), nil, nil)
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
			ref[k] = c.fieldType(fields.Value(i))
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

// fieldType returns the type of a record type's field, x. It may start
// with type variables, as `a => a -> m a`, for a polymorphic field: each use
// of the field may choose them anew.
func (c *context) fieldType(x ast.Node) TypeRef {
	if c.src.Kind(x) != ast.Forall {
		return c.typ(x)
	}
	params, body := c.params(x)
	c.markRows(params, body)
	tvars := c.tvars
	vars := make([]TypeRef, len(params))
	for i, p := range params {
		if p.row || p.arity > 0 {
			c.bail(x, fmt.Sprintf("a polymorphic field's variables must be types, not %s", p.kind()))
		}
		if c.tvars.Get(p.name) != nil {
			c.bail(x, fmt.Sprintf("cannot bind type variable %s twice", p.name))
		}
		vars[i] = c.reg.bound(p.name)
		c.tvars = c.tvars.Bind(p.name, tvar{ref: vars[i]})
	}
	ref := c.typ(body)
	c.tvars = tvars
	for i := len(vars) - 1; i >= 0; i-- {
		ref = c.reg.forall(vars[i], ref)
	}
	return ref
}

// application returns the type that the type application x denotes.
func (c *context) application(x ast.Node) TypeRef {
	return c.applied(x, nil)
}

// applied returns the type that the type expression x denotes, applied to
// extra as well as to the arguments that x applies it to.
func (c *context) applied(x ast.Node, extra []TypeRef) TypeRef {
	fn, args := c.typeApp(x)
	n := len(args) + len(extra)

	switch c.src.Kind(fn) {
	case ast.Import, ast.Access:
		return c.applyStatic(x, fn, c.static(fn), args, extra)
	case ast.Ident:
		name := c.src.TokenText(c.src.Main(fn))
		if v := c.tvars.Get(name); v != nil && v.val.arity > 0 {
			if n != v.val.arity {
				c.bail(x, fmt.Sprintf("%s takes %s, not %d", name, typeArguments(v.val.arity), n))
			}
			ref := v.val.ref
			for _, arg := range args {
				ref = c.reg.app(ref, c.typ(arg))
			}
			for _, arg := range extra {
				ref = c.reg.app(ref, arg)
			}
			return ref
		}
		if c.tvars.Get(name) == nil {
			if s := c.staticName(c.src.TokenBytes(c.src.Main(fn))); !s.isNone() {
				return c.applyStatic(x, fn, s, args, extra)
			}
			if name == "list" {
				if n != 1 {
					c.bail(x, fmt.Sprintf("list takes %s, not %d", typeArguments(1), n))
				}
				if len(extra) > 0 {
					return c.reg.List(extra[0])
				}
				return c.reg.List(c.typ(args[0]))
			}
		}
	}
	c.bail(fn, fmt.Sprintf("%s is not a type constructor", c.src.NodeText(fn)))
	return NoType
}

// constructor returns the constructor of arity types that x denotes, the
// argument of a constructor parameter: a variable of that arity, or a
// constructor applied to all but its last arity parameters, as `option` or
// `state s`.
func (c *context) constructor(x ast.Node, arity int) TypeRef {
	if c.src.Kind(x) == ast.Ident {
		if v := c.tvars.Get(c.src.TokenText(c.src.Main(x))); v != nil && v.val.arity == arity {
			return v.val.ref
		}
	}
	vars := make([]TypeRef, arity)
	for i := range vars {
		vars[i] = c.reg.bound(string(varNames[i%len(varNames)]))
	}
	ref := c.applied(x, vars)
	for i := len(vars) - 1; i >= 0; i-- {
		ref = c.reg.lam(vars[i], ref)
	}
	return ref
}

// apply returns the type that the alias a denotes, applied to args and then
// to extra.
func (c *context) apply(x ast.Node, a *alias, args []ast.Node, extra []TypeRef) TypeRef {
	if n := len(args) + len(extra); n != len(a.params) {
		if len(a.params) == 0 {
			c.bail(x, fmt.Sprintf("%s is not a type constructor", a.name))
		}
		c.bail(x, fmt.Sprintf("%s takes %s, not %d", a.name, typeArguments(len(a.params)), n))
	}
	if len(a.params) == 0 {
		return c.alias(x, a)
	}
	return c.expand(x, a, c.typeArgs(x, a.name, a.params, args, extra))
}

// typeArgs returns the types of the arguments to the constructor name,
// args followed by extra, whose parameters are params. A row argument must
// be a row variable, and a constructor argument a constructor of its arity.
func (c *context) typeArgs(x ast.Node, name string, params []param, args []ast.Node, extra []TypeRef) []TypeRef {
	refs := make([]TypeRef, len(args), len(params))
	for i, arg := range args {
		switch p := params[i]; {
		case p.arity > 0:
			refs[i] = c.constructor(arg, p.arity)
			continue
		case !p.row:
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
	for i, p := range params[len(args):] {
		if p.row || p.arity > 0 {
			c.bail(x, fmt.Sprintf("type argument %d of %s must be given, as it is a %s", len(args)+i+1, name, p.kind()))
		}
	}
	return append(refs, extra...)
}

// applyStatic returns the type that the path fn denotes, its static part s,
// applied to args and extra if it is a type constructor.
func (c *context) applyStatic(x, fn ast.Node, s static, args []ast.Node, extra []TypeRef) TypeRef {
	switch {
	case s.alias != nil:
		return c.apply(x, s.alias, args, extra)
	case s.isType():
		return c.applyResult(x, c.src.NodeText(fn), s.res, args, extra)
	}
	c.notType(fn)
	return NoType
}

// applyResult returns the type that the imported type res, written name,
// denotes, applied to args and extra if it is a type constructor.
func (c *context) applyResult(x ast.Node, name string, res Result, args []ast.Node, extra []TypeRef) TypeRef {
	if n := len(args) + len(extra); n != len(res.Params) {
		if len(res.Params) == 0 {
			c.bail(x, fmt.Sprintf("%s is not a type constructor", name))
		}
		c.bail(x, fmt.Sprintf("%s takes %s, not %d", name, typeArguments(len(res.Params)), n))
	}
	if len(res.Params) == 0 {
		return c.reg.reference(name, res.Type, nil)
	}
	vars := make([]TypeRef, len(res.Params))
	params := make([]param, len(res.Params))
	for i, p := range res.Params {
		vars[i], params[i] = p.Var, param{name: p.Name, row: p.Row, arity: p.Arity}
	}
	refs := c.typeArgs(x, name, params, args, extra)
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
		params[i] = Param{Name: p.name, Var: refs[i], Row: p.row, Arity: p.arity}
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
// registry (SPEC.md section 7.5). Every cycle must pass through a tag's
// payload, which is checked on the expanded type, so that an argument the
// alias passes to another may recur where that one puts it. A recursive use
// must also apply the alias to the same arguments, or the type would be
// infinite.
func (c *context) expand(x ast.Node, a *alias, refs []TypeRef) TypeRef {
	if a.pending {
		return c.recur(x, a, refs)
	}
	a.pending, a.args, a.back = true, refs, NoType
	c.expanding = append(c.expanding, a)
	before := c.reg.next()
	aliases, tvars := c.aliases, c.tvars
	c.aliases, c.tvars = a.scope, nil
	for i, p := range a.params {
		c.tvars = c.tvars.Bind(p.name, tvar{refs[i], p.row, p.arity})
	}
	ref := c.typ(a.body)
	c.aliases, c.tvars = aliases, tvars
	c.expanding = c.expanding[:len(c.expanding)-1]
	back := a.back
	a.pending, a.args = false, nil
	if back != NoType {
		if !c.reg.guarded(ref, back) {
			c.bail(a.use, fmt.Sprintf("recursive type %s must recur within a tag's payload", a.name))
		}
		c.reg.link(back, ref)
	}
	// The type prints by the alias's name, unless it is a type that
	// existed before, such as a primitive or another alias's type, or an
	// alias in its body named it first. A recursive alias names it anyway,
	// since the name the body gave it has the type itself as an argument,
	// as in `expr = expr-f expr`.
	if _, named := c.reg.names[ref]; ref >= before && (!named || back != NoType) {
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
		args []TypeRef
		back TypeRef
		use  ast.Node
	}
	outer := c.expanding
	saved := make([]state, len(outer))
	for i, a := range outer {
		saved[i] = state{a.args, a.back, a.use}
		a.pending = false
	}
	c.expanding = nil
	ref := f()
	c.expanding = outer
	for i, a := range outer {
		a.pending, a.args, a.back, a.use = true, saved[i].args, saved[i].back, saved[i].use
	}
	return ref
}

// recur returns the type that a recursive use x of the alias a, applied to
// refs, refers back to.
func (c *context) recur(x ast.Node, a *alias, refs []TypeRef) TypeRef {
	for i, ref := range refs {
		if c.reg.Resolve(ref) != c.reg.Resolve(a.args[i]) {
			c.bail(x, fmt.Sprintf("recursive type %s must recur with the same type arguments", a.name))
		}
	}
	if a.back == NoType {
		a.back, a.use = c.reg.Var(), x
	}
	return a.back
}
