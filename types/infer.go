package types

import (
	"encoding/hex"
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

// An alias is a type-only binding, whose type is computed on first use.
type alias struct {
	typ     ast.Node
	scope   *Scope[*alias]
	ref     TypeRef
	done    bool
	pending bool
}

type InferImport func(algo string, hash []byte) (TypeRef, error)

type context struct {
	src         *ast.SourceExpr
	reg         *Registry
	scope       TypeScope
	aliases     *Scope[*alias]
	inferImport InferImport
}

func (c *context) bail(n ast.Node, msg string) {
	panic(c.src.Error(n, msg))
}

func (c *context) bind(name string, ref TypeRef) {
	c.scope = c.scope.Bind(name, ref)
}

// Infer infers the type of se, given the types of the values in scope. The
// result is generalized, so that it can be used as a type scheme.
func Infer(reg *Registry, scope TypeScope, se ast.SourceExpr, inferImport InferImport) (ref TypeRef, err error) {
	context := context{
		src:         &se,
		reg:         reg,
		scope:       scope,
		inferImport: inferImport,
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
	ref = context.scrap(se.Expr)
	reg.exit()
	reg.generalize(ref)
	return ref, err
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
		return c.where(x, c.infer)
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
		ref := c.imported(x)
		if _, ok := c.reg.Denoted(ref); ok {
			c.bail(x, fmt.Sprintf("cannot use type %s as a value", c.src.NodeText(x)))
		}
		return c.reg.Instantiate(ref)
	}

	panic(fmt.Sprintf("can't infer node %s", c.src.Kind(x)))
}

// scrap infers the type of a scrap's expression x. Unlike other expressions,
// a scrap may denote a type, so that other scraps can import it in type
// position. Then its type is the sort of that type.
func (c *context) scrap(x ast.Node) TypeRef {
	switch c.src.Kind(x) {
	case ast.Where:
		return c.where(x, c.scrap)
	case ast.Ident:
		name := c.src.TokenText(c.src.Main(x))
		if c.scope.Get(name) == nil {
			if a := c.aliases.Lookup(name); a != nil {
				return c.reg.Type(c.alias(x, name, a))
			}
		}
	case ast.Import:
		ref := c.imported(x)
		if _, ok := c.reg.Denoted(ref); ok {
			return ref
		}
		return c.reg.Instantiate(ref)
	}
	return c.infer(x)
}

// imported returns the type of the scrap that the import x refers to, which
// may be a type scheme or the sort of a type.
func (c *context) imported(x ast.Node) TypeRef {
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
func (c *context) where(x ast.Node, inferBody func(ast.Node) TypeRef) TypeRef {
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
			if typ != ast.None && c.src.NodeText(typ) != "type" || c.src.Kind(val) == ast.Forall {
				c.bail(b, "type constructors are not supported yet")
			}
			a := &alias{typ: val}
			group = append(group, a)
			c.aliases = c.aliases.Bind(name, a)
		}
	}
	// Aliases see each other.
	for _, a := range group {
		a.scope = c.aliases
	}

	for _, comp := range c.src.Components(binds) {
		c.reg.enter()
		vars := make([]TypeRef, len(comp))
		for i, b := range comp {
			vars[i] = c.reg.Var()
			c.bind(c.src.TokenText(c.src.Main(b)), vars[i])
		}
		for i, b := range comp {
			_, _, typ, val := c.src.Where(b)
			ref := c.infer(val)
			// If there's an annotation, make sure it matches the inferred type.
			if typ != ast.None {
				c.ensure(typ, ref, c.typ(typ))
			}
			c.ensure(val, vars[i], ref)
		}
		c.reg.exit()
		for _, v := range vars {
			c.reg.generalize(v)
		}
	}

	return inferBody(body)
}

var primitiveTypes = map[string]TypeRef{
	"int":   IntRef,
	"float": FloatRef,
	"text":  TextRef,
	"byte":  ByteRef,
	"bytes": BytesRef,
}

// typ returns the type that the type expression x denotes.
func (c *context) typ(x ast.Node) TypeRef {
	switch c.src.Kind(x) {
	case ast.Ident:
		name := c.src.TokenText(c.src.Main(x))
		if ref, ok := primitiveTypes[name]; ok {
			return ref
		}
		if a := c.aliases.Lookup(name); a != nil {
			return c.alias(x, name, a)
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
		fn, arg := c.src.Call(x)
		if c.src.Kind(fn) == ast.Ident && c.src.NodeText(fn) == "list" {
			return c.reg.List(c.typ(arg))
		}
	case ast.Func:
		arg, body := c.src.Func(x)
		return c.reg.Func(c.typ(arg), c.typ(body))
	case ast.Enum:
		tail, variants := c.src.Enum(x)
		c.closedRow(tail)
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
		return c.reg.Enum(tags)
	case ast.RecordType:
		tail, fields := c.src.Record(x)
		c.closedRow(tail)
		ref := make(map[string]TypeRef, fields.Len())
		for i := range fields.Len() {
			k := c.src.TokenText(fields.Key(i))
			if _, ok := ref[k]; ok {
				c.bail(fields.Value(i), fmt.Sprintf("cannot define field %s more than once", k))
			}
			ref[k] = c.typ(fields.Value(i))
		}
		return c.reg.Record(ref)
	case ast.Forall:
		c.bail(x, "type variables are not supported yet")
	}

	c.bail(x, fmt.Sprintf("%s is not a type", c.src.NodeText(x)))
	return NoType
}

// closedRow fails at tail, unless it is None.
func (c *context) closedRow(tail ast.Node) {
	if tail != ast.None {
		c.bail(tail, "row variables are not supported yet")
	}
}

// alias returns the type of an alias, computing it on first use. Aliases
// have no type variables, so their types can be shared.
func (c *context) alias(x ast.Node, name string, a *alias) TypeRef {
	if a.done {
		return a.ref
	}
	if a.pending {
		c.bail(x, fmt.Sprintf("recursive type %s is not supported yet", name))
	}
	a.pending = true
	aliases := c.aliases
	c.aliases = a.scope
	a.ref = c.typ(a.typ)
	c.aliases = aliases
	a.pending, a.done = false, true
	return a.ref
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
	at  position
	typ TypeRef
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
		m.enums = append(m.enums, positioned{at, ty})
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
