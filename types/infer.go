package types

import (
	"fmt"
	"strings"

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

// TypeScope maps names of values to their types, which may be schemes.
type TypeScope = *Scope[TypeRef]

// A Result is what checking a scrap gives: the type scheme of its value, or,
// for a type scrap, the type it denotes.
type Result struct {
	Type TypeRef
	Sort bool // whether the scrap denotes Type, rather than having a value of it
	// For a type constructor, its parameters, which are generalized variables
	// in Type.
	Params []Param
}

// A Param is a parameter of an exported type constructor.
type Param struct {
	Name string
	Var  TypeRef
	Row  bool
}

// Imports gives the results of the scraps that a scrap's Import nodes refer
// to. They are checked before it, so it has a result for each of them.
type Imports map[ast.Node]Result

type context struct {
	src     *ast.SourceExpr
	reg     *Registry
	scope   TypeScope
	aliases *Scope[*alias]
	tvars   *Scope[tvar]
	imports Imports
	// How many tag payloads the type expression being read is inside of.
	payloads int
	// The match functions, and their argument types, to check for
	// exhaustiveness once the whole scrap is inferred.
	matches []positioned
}

func (c *context) bail(n ast.Node, msg string) {
	panic(c.src.Error(n, msg))
}

func (c *context) bind(name string, ref TypeRef) {
	c.scope = c.scope.Bind(name, ref)
}

// Infer infers the result of the scrap se, given the types of the values in
// scope and the results of its imports. A value's type is generalized, so
// that it can be used as a type scheme.
func Infer(reg *Registry, scope TypeScope, se ast.SourceExpr, imports Imports) (res Result, err error) {
	context := context{
		src:     &se,
		reg:     reg,
		scope:   scope,
		imports: imports,
	}

	level := reg.level
	defer func() {
		reg.level = level
		if pnc := recover(); pnc != nil {
			if e, ok := pnc.(token.Error); ok {
				err = e
			} else {
				panic(pnc)
			}
		}
	}()

	reg.enter()
	res = context.scrap(se.Expr)
	for _, m := range context.matches {
		context.exhaustive(m.node, m.typ)
	}
	reg.exit()
	reg.generalize(res.Type)
	return res, err
}

func (c *context) infer(x ast.Node) TypeRef {
	switch c.src.Kind(x) {
	case ast.Literal:
		return literalTypeRef(c.src.LiteralKind(x))
	case ast.Ident:
		name := c.src.TokenText(c.src.Main(x))
		bound := c.scope.Get(name)
		if bound == nil {
			if c.aliases.Get(name) != nil {
				c.bail(x, fmt.Sprintf("cannot use type %s as a value", name))
			}
			c.bail(x, "unbound variable: "+name)
		}
		return c.reg.Instantiate(bound.val)
	case ast.Where:
		return where(c, x, c.infer)
	case ast.List:
		return c.list(x)
	case ast.Record:
		return c.record(x)
	case ast.Access:
		rec, key := c.src.Access(x)
		field := c.reg.Var()
		row := c.reg.extend(c.reg.intern(c.src.TokenText(key)), field, c.reg.Var())
		c.ensure(rec, c.infer(rec), c.reg.add(node{kind: recordKind, a: row}))
		return field
	case ast.Variant:
		tag, payload := c.src.Variant(x)
		typ := HoleRef
		if payload != ast.None {
			typ = c.infer(payload)
		}
		row := c.reg.extend(c.reg.intern(c.src.TokenText(tag)), typ, c.reg.Var())
		return c.reg.add(node{kind: enumKind, a: row})
	case ast.Enum:
		c.bail(x, "cannot use a type as a value")

	case ast.Func:
		arg, body := c.src.Func(x)
		if c.src.Kind(arg) != ast.Ident {
			c.bail(arg, "function parameter must be an identifier")
		}
		binder := c.reg.Var()
		scope := c.scope
		if name := c.src.NodeText(arg); name != "_" {
			c.bind(name, binder)
		}
		ret := c.infer(body)
		c.scope = scope
		return c.reg.Func(binder, ret)

	case ast.MatchFunc:
		return c.matchFunc(x)

	case ast.Call:
		fn, arg := c.src.Call(x)
		return c.call(x, fn, arg)

	case ast.Binary:
		return c.binary(x)

	case ast.Import:
		res := c.imported(x)
		if res.Sort {
			c.bail(x, fmt.Sprintf("cannot use type %s as a value", c.src.NodeText(x)))
		}
		return c.reg.Instantiate(res.Type)
	}

	panic(fmt.Sprintf("can't infer node %s", c.src.Kind(x)))
}

// scrap infers the result of a scrap's expression x. Unlike other
// expressions, a scrap may denote a type, so that other scraps can import it
// in type position.
func (c *context) scrap(x ast.Node) Result {
	switch c.src.Kind(x) {
	case ast.Where:
		return where(c, x, c.scrap)
	case ast.Ident:
		name := c.src.TokenText(c.src.Main(x))
		if c.scope.Get(name) == nil {
			if a := c.aliases.Lookup(name); a != nil {
				return c.export(x, a)
			}
		}
	case ast.Import:
		if res := c.imported(x); res.Sort {
			return res
		}
	}
	return Result{Type: c.infer(x)}
}

// imported returns the result of the scrap that the import x refers to.
func (c *context) imported(x ast.Node) Result {
	res, ok := c.imports[x]
	if !ok {
		c.bail(x, fmt.Sprintf("<internal error> %s was not loaded", c.src.NodeText(x)))
	}
	return res
}

// check checks that x has the type want. A function's parameter takes the
// argument type of want before its body is checked, so that an annotation
// informs the inference of the body, as for `++`.
func (c *context) check(x ast.Node, want TypeRef) {
	if c.src.Kind(x) == ast.Func {
		arg, body := c.src.Func(x)
		if n := c.reg.at(c.reg.Resolve(want)); n.kind == funcKind && c.src.Kind(arg) == ast.Ident {
			scope := c.scope
			if name := c.src.NodeText(arg); name != "_" {
				c.bind(name, n.a)
			}
			c.check(body, n.b)
			c.scope = scope
			return
		}
	}
	c.ensure(x, c.infer(x), want)
}

// ensure unifies got with want, failing at x if they differ.
func (c *context) ensure(x ast.Node, got, want TypeRef) {
	if err := c.reg.Unify(got, want); err != nil {
		c.bail(x, err.Error())
	}
}

func (c *context) call(x, fn, arg ast.Node) TypeRef {
	res := c.reg.Var()
	fnTy := c.infer(fn)
	argTy := c.infer(arg)
	c.ensure(x, fnTy, c.reg.Func(argTy, res))
	return res
}

func (c *context) binary(x ast.Node) TypeRef {
	op, l, r := c.src.Binary(x)
	switch op {
	// Pipes are just calls.
	case token.LPIPE:
		return c.call(x, l, r)
	case token.RPIPE:
		return c.call(x, r, l)
	}

	left := c.infer(l)
	right := c.infer(r)
	switch op {
	case token.PREPEND:
		return c.pend(l, r, left, right)
	case token.APPEND:
		return c.pend(r, l, right, left)
	case token.CONCAT:
		if c.either(left, right, TextRef) {
			c.ensure(x, left, right)
			return TextRef
		}
		if c.either(left, right, BytesRef) {
			c.ensure(x, left, right)
			return BytesRef
		}
		c.ensure(x, left, right)
		c.ensure(x, left, c.reg.List(c.reg.Var()))
		return left
	case token.ADD, token.SUB, token.MUL:
		if c.either(left, right, FloatRef) {
			c.ensure(x, left, right)
			return FloatRef
		}
		// Assume int, like ML does.
		c.ensure(l, left, IntRef)
		c.ensure(r, right, IntRef)
		return IntRef
	case token.RCOMP, token.LCOMP:
		// f >> g is x -> g (f x).
		f, g := left, right
		if op == token.LCOMP {
			f, g = right, left
		}
		a, b, res := c.reg.Var(), c.reg.Var(), c.reg.Var()
		c.ensure(x, f, c.reg.Func(a, b))
		c.ensure(x, g, c.reg.Func(b, res))
		return c.reg.Func(a, res)
	}
	c.bail(x, fmt.Sprintf("can't infer binary expression %s", op.String()))
	return NoType
}

// either returns true if a or b is already known to be the primitive typ.
func (c *context) either(a, b, typ TypeRef) bool {
	return c.reg.Resolve(a) == typ || c.reg.Resolve(b) == typ
}

// where infers the type of a where-group's body with inferBody. The group's
// bindings are mutually recursive. Each strongly connected component of them
// is inferred in turn, and generalized before the components that use it.
func where[T any](c *context, x ast.Node, inferBody func(ast.Node) T) T {
	body, binds := c.src.Group(x)
	scope, aliases := c.scope, c.aliases
	defer func() { c.scope, c.aliases = scope, aliases }()

	seen := make(map[string]bool, len(binds))
	var group []*alias
	for _, b := range binds {
		_, id, typ, val := c.src.Where(b)
		name := c.src.TokenText(id)
		if seen[name] {
			c.bail(b, fmt.Sprintf("cannot bind %s twice", name))
		}
		seen[name] = true

		if c.src.Sort(b) {
			a := c.sortBinding(b, name, typ, val)
			group = append(group, a)
			c.aliases = c.aliases.Bind(name, a)
		}
	}
	// Aliases see each other. Check them all, even those that aren't used.
	for _, a := range group {
		a.scope = c.aliases
	}
	for _, a := range group {
		c.checkAlias(a)
	}

	// An annotated binding has the type its annotation declares, everywhere
	// in the group, so references to it don't make it part of a component.
	annotated := make(map[ast.Node]bool)
	for _, b := range binds {
		if _, id, typ, _ := c.src.Where(b); typ != ast.None && !c.src.Sort(b) {
			annotated[b] = true
			c.bind(c.src.TokenText(id), c.annotation(typ, func(string) TypeRef { return c.reg.Gen() }))
		}
	}

	comps := c.src.Components(binds, func(b ast.Node) bool { return annotated[b] })
	if len(annotated) == 0 {
		c.functions(comps)
	} else {
		c.functions(c.src.Components(binds, nil))
	}

	for _, comp := range comps {
		if b := comp.Binds[0]; annotated[b] {
			// Check the value with the annotation's type variables rigid, so
			// that it can't assume anything about them.
			_, _, typ, val := c.src.Where(b)
			c.reg.enter()
			c.check(val, c.annotation(typ, c.reg.rigid))
			c.reg.exit()
			continue
		}

		c.reg.enter()
		vars := make([]TypeRef, len(comp.Binds))
		for i, b := range comp.Binds {
			vars[i] = c.reg.Var()
			c.bind(c.src.TokenText(c.src.Main(b)), vars[i])
		}
		for i, b := range comp.Binds {
			_, _, _, val := c.src.Where(b)
			c.ensure(val, vars[i], c.infer(val))
		}
		c.reg.exit()
		for _, v := range vars {
			c.reg.generalize(v)
		}
	}

	return inferBody(body)
}

// functions checks that the bindings of recursive components are
// functions. Only functions may be recursive, since a value would be used
// before it is evaluated (SPEC.md section 8).
func (c *context) functions(comps []ast.Component) {
	for _, comp := range comps {
		if !comp.Recursive {
			continue
		}
		for _, b := range comp.Binds {
			_, id, _, val := c.src.Where(b)
			if val, _ = c.src.Group(val); c.src.Kind(val) != ast.Func && c.src.Kind(val) != ast.MatchFunc {
				c.bail(b, fmt.Sprintf("%s is recursive, so it must be a function", c.src.TokenText(id)))
			}
		}
	}
}

func (c *context) list(x ast.Node) TypeRef {
	elem := c.reg.Var()
	for _, v := range c.src.List(x) {
		c.ensure(v, elem, c.infer(v))
	}
	return c.reg.List(elem)
}

func (c *context) record(x ast.Node) TypeRef {
	rest, fields := c.src.Record(x)

	// A spread record may only have its existing fields updated, keeping
	// their types.
	if rest != ast.None {
		restTy := c.infer(rest)
		row := c.reg.Var()
		for i := range fields.Len() {
			k, v := c.src.TokenText(fields.Key(i)), fields.Value(i)
			row = c.reg.extend(c.reg.intern(k), c.infer(v), row)
		}
		c.ensure(x, restTy, c.reg.add(node{kind: recordKind, a: row}))
		return restTy
	}

	ref := make(map[string]TypeRef, fields.Len())
	for i := range fields.Len() {
		k := c.src.TokenText(fields.Key(i))
		if _, ok := ref[k]; ok {
			c.bail(fields.Value(i), fmt.Sprintf("cannot set %s twice", k))
		}
		ref[k] = c.infer(fields.Value(i))
	}
	return c.reg.Record(ref)
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

	return NoType
}

// Either pre-pend or ap-pend.
func (c *context) pend(singleX, listX ast.Node, single, list TypeRef) TypeRef {
	// Special-case bytes.
	if c.reg.Resolve(single) == ByteRef || c.reg.Resolve(list) == BytesRef {
		c.ensure(singleX, single, ByteRef)
		c.ensure(listX, list, BytesRef)
		return BytesRef
	}

	c.ensure(listX, list, c.reg.List(single))
	return list
}

// A position in a match function's argument, as a path of /-separated steps
// into it. The root is the empty path.
type position string

func (p position) step(s string) position { return p + "/" + position(s) }

// within returns true if p is at or below other.
func (p position) within(other position) bool {
	return p == other || strings.HasPrefix(string(p), string(other)+"/")
}

// The positions in a match function's arms that matter for its type.
type arms struct {
	// The positions that some arm matches with a catch-all pattern.
	catchAll []position
	// The positions that some arm matches with a tag, and their types.
	enums []positioned
	// The names bound by the current arm.
	bound map[string]bool
}

type positioned struct {
	at   position
	node ast.Node
	typ  TypeRef
}

// matchFunc infers the type of a match function. A tag pattern only closes
// the enum it matches if no arm matches the same position, or one above it,
// with a catch-all pattern. So a match function without a catch-all accepts
// exactly the tags it lists.
func (c *context) matchFunc(x ast.Node) TypeRef {
	argTy := c.reg.Var()
	bodyTy := c.reg.Var()
	var m arms
	for _, arm := range c.src.Arms(x) {
		arg, body := c.src.Func(arm)
		scope := c.scope
		m.bound = make(map[string]bool)
		c.pattern(&m, arg, argTy, "")
		c.ensure(body, bodyTy, c.infer(body))
		c.scope = scope
	}

	for _, e := range m.enums {
		if !e.at.coveredBy(m.catchAll) {
			c.reg.closeEnum(e.typ)
		}
	}
	c.matches = append(c.matches, positioned{node: x, typ: argTy})
	return c.reg.Func(argTy, bodyTy)
}

func (p position) coveredBy(catchAll []position) bool {
	for _, c := range catchAll {
		if p.within(c) {
			return true
		}
	}
	return false
}

// pattern infers the type of the pattern x at position at, unifying it with
// ty and binding its names.
func (c *context) pattern(m *arms, x ast.Node, ty TypeRef, at position) {
	switch c.src.Kind(x) {
	case ast.Ident:
		m.catchAll = append(m.catchAll, at)
		name := c.src.TokenText(c.src.Main(x))
		if name == "_" {
			return
		}
		if m.bound[name] {
			c.bail(x, fmt.Sprintf("cannot bind %s twice", name))
		}
		m.bound[name] = true
		c.bind(name, ty)
		return

	case ast.Literal:
		kind := c.src.LiteralKind(x)
		if kind == token.FLOAT {
			c.bail(x, "cannot match on floats")
		}
		c.ensure(x, ty, literalTypeRef(kind))
		return

	case ast.Variant:
		tagTok, payload := c.src.Variant(x)
		tag := c.src.TokenText(tagTok)
		typ := HoleRef
		if payload != ast.None {
			typ = c.reg.Var()
		}
		row := c.reg.extend(c.reg.intern(tag), typ, c.reg.Var())
		c.ensure(x, ty, c.reg.add(node{kind: enumKind, a: row}))
		m.enums = append(m.enums, positioned{at: at, typ: ty})
		if payload != ast.None {
			c.pattern(m, payload, typ, at.step("#"+tag))
		}
		return

	case ast.Record:
		rest, fields := c.src.Record(x)
		tail := TypeRef(emptyRef)
		if rest != ast.None {
			tail = c.reg.Var()
		}
		row := tail
		types := make([]TypeRef, fields.Len())
		for i := range fields.Len() {
			types[i] = c.reg.Var()
			row = c.reg.extend(c.reg.intern(c.src.TokenText(fields.Key(i))), types[i], row)
		}
		c.ensure(x, ty, c.reg.add(node{kind: recordKind, a: row}))
		for i := range fields.Len() {
			c.pattern(m, fields.Value(i), types[i], at.step("."+c.src.TokenText(fields.Key(i))))
		}
		if rest != ast.None {
			// The rest is a record of the remaining fields, but not a
			// catch-all for the record.
			c.pattern(m, rest, c.reg.add(node{kind: recordKind, a: tail}), at.step(".."))
		}
		return

	case ast.List:
		elem := c.reg.Var()
		c.ensure(x, ty, c.reg.List(elem))
		for _, v := range c.src.List(x) {
			c.pattern(m, v, elem, at.step("[]"))
		}
		return

	case ast.Binary:
		// Parts of a list are lists, but don't catch all of it.
		op, l, r := c.src.Binary(x)
		elem := c.reg.Var()
		list := c.reg.List(elem)
		switch op {
		case token.PREPEND:
			c.ensure(x, ty, list)
			c.pattern(m, l, elem, at.step("[]"))
			c.pattern(m, r, list, at.step("[..]"))
			return
		case token.APPEND:
			c.ensure(x, ty, list)
			c.pattern(m, l, list, at.step("[..]"))
			c.pattern(m, r, elem, at.step("[]"))
			return
		case token.CONCAT:
			c.ensure(x, ty, list)
			c.pattern(m, l, list, at.step("[..]"))
			c.pattern(m, r, list, at.step("[..]"))
			return
		}
	}

	c.bail(x, fmt.Sprintf("cannot match on %s", c.src.Kind(x)))
}
