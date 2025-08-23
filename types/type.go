package types

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// The type's tag within a Registry. An implementation detail.
type tag int

const (
	primitiveTag tag = iota
	listTag
	funcTag
	enumTag
	recordTag
	unboundTag
	varTag
)

var tagNames = [...]string{
	primitiveTag: "primitive",
	listTag:      "list",
	funcTag:      "func",
	enumTag:      "enum",
	recordTag:    "record",
	unboundTag:   "unbound",
	varTag:       "var",
}

// Efficiently encodes a type reference within a Registry.
//
// The zero value references the impossible "never" type.
type TypeRef int

// We use the first 4 bits as the tag, and the remainder
// for the index.
func makeTypeRef(tag tag, index int) TypeRef {
	return TypeRef(int(tag) | (index << 4))
}

// Extracts a TypeRef into its tag and index.
func (ref TypeRef) extract() (tag, int) {
	tag := tag(ref & 0x0f)
	index := int(ref >> 4)
	return tag, index
}

func (ref TypeRef) tag() tag {
	return tag(ref & 0x0f)
}

func (ref TypeRef) hasTag(t tag) bool {
	return ref.tag() == t
}

// Returns true if both TypeRefs have the same tag.
func (ref TypeRef) SameTypeAs(other TypeRef) bool {
	return ref.hasTag(other.tag())
}

// IsList returns true if the TypeRef is a list.
func (ref TypeRef) IsList() bool {
	return ref.hasTag(listTag)
}

// IsFunction returns true if the TypeRef is a function.
func (ref TypeRef) IsFunction() bool {
	return ref.hasTag(funcTag)
}

// IsUnbound returns true if the TypeRef is an unbound type.
func (ref TypeRef) IsUnbound() bool {
	return ref.hasTag(unboundTag)
}

// IsVar returns true if the TypeRef is an var type.
func (ref TypeRef) IsVar() bool {
	return ref.hasTag(varTag)
}

const (
	// Shortcut to the TypeRef for Never.
	NeverRef TypeRef = TypeRef(int(primitiveTag) | (iota << 4)) // Inlined makeTypeRef
	HoleRef
	IntRef
	FloatRef
	TextRef
	ByteRef
	BytesRef
)

var primitives = [...]TypeRef{NeverRef, HoleRef, IntRef, FloatRef, TextRef, ByteRef, BytesRef}

var primitiveNames = [...]string{
	"never",
	"()",
	"int",
	"float",
	"text",
	"byte",
	"bytes",
}

type FuncRef struct {
	Arg, Result TypeRef
}

type MapRef map[string]TypeRef

// Contains the types of a running application.
type Registry struct {
	// The number of unique unbound types.
	unbound int
	// Lists just have a TypeRef.
	lists []TypeRef
	// Functions map one TypeRef to another.
	funcs []FuncRef
	// Enums and records are maps to TypeRefs.
	enums   []MapRef
	records []MapRef
	// Type variables that will point to another type,
	// or NeverRef if not yet assigned.
	//
	// Schemes are types with unbound TypeRefs. When instantiating a type,
	// all unbound types will be replaced with fresh vars instead.
	vars []TypeRef
}

// Returns the number of types in the registry, for debugging.
func (c *Registry) Size() int {
	return len(c.lists) + len(c.funcs) + len(c.enums) + len(c.records)
}

// Strings returns a string representation for TypeRef.
func (c *Registry) String(ref TypeRef) string {
	var s stringer
	s.reg = c
	s.string(ref, 0)
	return s.String()
}

// List returns the TypeRef for a list type.
func (c *Registry) List(ref TypeRef) TypeRef {
	return findOrAdd(&c.lists, listTag, ref)
}

// GetList returns the TypeRef for a list type.
// It returns `a` for any `list a`.
func (c *Registry) GetList(ref TypeRef) (res TypeRef) {
	tag, index := ref.extract()
	if tag != listTag {
		return
	}
	return c.lists[index]
}

// Func returns the TypeRef for a function type.
func (c *Registry) Func(from, to TypeRef) TypeRef {
	return findOrAdd(&c.funcs, funcTag, FuncRef{from, to})
}

// GetFunc returns the TypeRef for an function type.
func (c *Registry) GetFunc(ref TypeRef) (res FuncRef) {
	tag, index := ref.extract()
	if tag != funcTag {
		return
	}
	return c.funcs[index]
}

// Enum returns the TypeRef for an enum type.
func (c *Registry) Enum(ref MapRef) TypeRef {
	return findOrAddMap(&c.enums, enumTag, ref)
}

// GetEnum returns the TypeRef for an enum type.
func (c *Registry) GetEnum(ref TypeRef) MapRef {
	tag, index := ref.extract()
	if tag != enumTag {
		return nil
	}
	return c.enums[index]
}

// Record returns the TypeRef for a record type.
func (c *Registry) Record(ref MapRef) TypeRef {
	return findOrAddMap(&c.records, recordTag, ref)
}

// GetRecord returns the TypeRef for an record type.
func (c *Registry) GetRecord(ref TypeRef) MapRef {
	tag, index := ref.extract()
	if tag != recordTag {
		return nil
	}
	return c.records[index]
}

// Unbound returns a new unbound TypeRef.
func (c *Registry) Unbound() (ref TypeRef) {
	ref = makeTypeRef(unboundTag, c.unbound)
	c.unbound += 1
	// fmt.
	return
}

// Var returns a new variable TypeRef.
func (c *Registry) Var() (ref TypeRef) {
	i := len(c.vars)
	c.vars = append(c.vars, NeverRef)
	return makeTypeRef(varTag, i)
}

// GetVar returns the TypeRef for an record type.
func (c *Registry) GetVar(ref TypeRef) TypeRef {
	tag, index := ref.extract()
	if tag != varTag {
		return NeverRef
	}
	mid := c.vars[index]
	if mid.hasTag(varTag) {
		// Try to resolve one more layer.
		c.vars[index] = c.GetVar(mid)
	}
	return c.vars[index]
}

// VarString returns the string representation of an unresolved variable.
func VarString(ref TypeRef) string {
	tag, index := ref.extract()
	if tag != varTag {
		panic("VarString: got non-var tag " + tagNames[tag])
	}
	return "$" + strconv.FormatInt(int64(index), 10)
}

type MapTypeRef func(ref TypeRef)

func (c *Registry) traverse(target TypeRef, mtr MapTypeRef) {
	tag, index := target.extract()
	switch tag {
	case listTag:
		c.traverse(c.lists[index], mtr)
	case funcTag:
		fn := c.funcs[index]
		c.traverse(fn.Arg, mtr)
		c.traverse(fn.Result, mtr)
	case enumTag:
		for _, v := range c.enums[index] {
			c.traverse(v, mtr)
		}
	case recordTag:
		for _, v := range c.records[index] {
			c.traverse(v, mtr)
		}
	}

	mtr(target)
}

// Bind replaces all occurrences of `unbound` with `resolved` in the `target` type.
func (c *Registry) Bind(target, unbound, resolved TypeRef) TypeRef {
	// Base case: the target is the unbound we want to replace.
	if target == unbound {
		return resolved
	}

	tag, index := target.extract()
	switch tag {
	case listTag:
		return c.List(
			c.Bind(c.lists[index], unbound, resolved),
		)
	case funcTag:
		fn := c.funcs[index]
		return c.Func(
			c.Bind(fn.Arg, unbound, resolved),
			c.Bind(fn.Result, unbound, resolved),
		)
	case enumTag:
		ref := make(MapRef, len(c.enums[index]))
		for k, v := range c.enums[index] {
			ref[k] = c.Bind(v, unbound, resolved)
		}
		return c.Enum(ref)
	case recordTag:
		ref := make(MapRef, len(c.records[index]))
		for k, v := range c.records[index] {
			ref[k] = c.Bind(v, unbound, resolved)
		}
		return c.Record(ref)
	}

	// Else, the target remains unchanged.
	return target
}

func (c *Registry) Instantiate(target TypeRef) TypeRef {
	var subst Subst
	c.insertUnbound(target, &subst)
	return c.substitute(target, subst)
}

func (c *Registry) insertUnbound(target TypeRef, subst *Subst) {
	tag, index := target.extract()
	switch tag {
	case unboundTag:
		if !subst.binds(target) {
			subst.bind(target, c.Var())
		}
	case listTag:
		c.insertUnbound(c.lists[index], subst)
	case funcTag:
		fn := c.funcs[index]
		c.insertUnbound(fn.Arg, subst)
		c.insertUnbound(fn.Result, subst)
		// TODO: Other types
	}
}

func (c *Registry) Unify(a, b TypeRef) Subst {
	if a == b {
		return nil
	}
	if a.IsUnbound() || (a.IsVar() && c.GetVar(a) == NeverRef) {
		return c.BindVar(a, b)
	}
	if b.IsUnbound() || (b.IsVar() && c.GetVar(b) == NeverRef) {
		return c.BindVar(b, a)
	}

	if a.tag() == b.tag() {
		if a.IsFunction() {
			aFn := c.GetFunc(a)
			bFn := c.GetFunc(b)
			s1 := c.Unify(aFn.Arg, bFn.Arg)
			s2 := c.Unify(c.substitute(aFn.Result, s1), c.substitute(bFn.Result, s1))
			return c.Compose(s1, s2)
		}
		if a.IsList() {
			aEl := c.GetList(a)
			bEl := c.GetList(b)
			return c.Unify(aEl, bEl)
		}
	}

	panic("cannot unify '" + c.String(a) + "' with '" + c.String(b) + "'")
}

func (c *Registry) BindVar(a, b TypeRef) Subst {
	if a == b {
		return nil
	}
	c.traverse(b, func(ref TypeRef) {
		if a == ref {
			panic("occurs check failed")
		}
	})
	return Subst{{replace: a, with: b}}
}

func (c *Registry) Compose(a, b Subst) Subst {
	res := slices.Clone(b)
	for _, s := range res {
		// fmt.Fprintf(os.Stderr, "replace %s: %s\n", c.String(s.replace), c.String(s.with))
		s.with = c.substitute(s.with, a)
	}
	for _, s := range a {
		if !res.binds(s.replace) {
			res.bind(s.replace, s.with)
		}
	}

	return res
}

func (c *Registry) apply(subst Subst) {
	for _, s := range subst {
		c.substitute(s.replace, subst)
	}
}

func (c *Registry) substitute(target TypeRef, subst Subst) TypeRef {
	tag, index := target.extract()
	switch tag {
	case unboundTag:
		for _, s := range subst {
			if s.replace == target {
				return s.with
			}
		}
	case varTag:
		for _, s := range subst {
			if s.replace == target {
				c.vars[index] = s.with
				return s.with
			}
		}
	case listTag:
		return c.List(
			c.substitute(c.lists[index], subst),
		)
	case funcTag:
		fn := c.funcs[index]
		return c.Func(
			c.substitute(fn.Arg, subst),
			c.substitute(fn.Result, subst),
		)
	case enumTag:
		ref := make(MapRef, len(c.enums[index]))
		for k, v := range c.enums[index] {
			ref[k] = c.substitute(v, subst)
		}
		return c.Enum(ref)
	case recordTag:
		ref := make(MapRef, len(c.records[index]))
		for k, v := range c.records[index] {
			ref[k] = c.substitute(v, subst)
		}
		return c.Record(ref)
	}

	// Else, the target remains unchanged.
	return target
}

func findOrAdd[T comparable](ls *[]T, tag tag, el T) TypeRef {
	list := *ls
	for i, typ := range list {
		if el == typ {
			return makeTypeRef(tag, i)
		}
	}
	i := len(list)
	*ls = append(list, el)
	return makeTypeRef(tag, i)
}

func findOrAddMap(ls *[]MapRef, tag tag, el MapRef) TypeRef {
	list := *ls
	for i, typ := range list {
		if maps.Equal(el, typ) {
			return makeTypeRef(tag, i)
		}
	}
	i := len(list)
	*ls = append(list, el)
	return makeTypeRef(tag, i)
}

var unboundNames = "abcdefghijklmnopqrstuvwxyz"

type stringer struct {
	strings.Builder
	reg *Registry
	// Mapping from unbound index to
	unbounds []int
}

func (b *stringer) unbound(index int) {
	i := slices.Index(b.unbounds, index)
	if i == -1 {
		i = len(b.unbounds)
		b.unbounds = append(b.unbounds, index)
	}
	b.WriteByte(unboundNames[i])
	// b.WriteByte(unboundNames[index])
}

func (b *stringer) string(ref TypeRef, nesting int) {
	tag, index := ref.extract()
	switch tag {
	case primitiveTag:
		b.WriteString(primitiveNames[index])
	case listTag:
		if nesting > 1 {
			b.WriteByte('(')
		}
		b.WriteString("list ")
		b.string(b.reg.lists[index], 2)
		if nesting > 1 {
			b.WriteByte(')')
		}
	case funcTag:
		fn := b.reg.funcs[index]
		if nesting > 0 {
			b.WriteByte('(')
		}
		b.string(fn.Arg, 1)
		b.WriteString(" -> ")
		b.string(fn.Result, 0)
		if nesting > 0 {
			b.WriteByte(')')
		}
	case enumTag:
		b.enum(index)
	case recordTag:
		b.record(index)
	case unboundTag:
		// b.WriteByte('\'')
		// b.WriteString(strconv.FormatInt(int64(index), 10))
		b.unbound(index)
	case varTag:
		ref := b.reg.GetVar(ref)
		if ref == NeverRef {
			b.WriteByte('$')
			b.WriteString(strconv.FormatInt(int64(index), 10))
		} else {
			b.string(ref, nesting)
		}
	default:
		// The invalid type.
		panic("bad type-ref")
	}
}

func (b *stringer) enum(index int) {
	e := b.reg.enums[index]
	space := len(e) - 1
	for _, key := range slices.Sorted(maps.Keys(e)) {
		b.WriteByte('#')
		b.WriteString(key)

		// Special-case never.
		if e[key] != NeverRef {
			b.WriteByte(' ')
			b.string(e[key], 1)
		}

		if space > 0 {
			space -= 1
			b.WriteByte(' ')
		}
	}
}

func (b *stringer) record(index int) {
	r := b.reg.records[index]
	b.WriteString("{ ")
	comma := len(r) - 1
	for _, key := range slices.Sorted(maps.Keys(r)) {
		b.WriteString(key)
		b.WriteString(" : ")
		b.string(r[key], 1)

		if comma > 0 {
			comma -= 1
			b.WriteString(", ")
		}
	}
	b.WriteString(" }")
}
