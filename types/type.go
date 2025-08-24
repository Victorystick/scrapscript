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

func (ref TypeRef) index() int {
	return int(ref >> 4)
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

// Resolve follows variables to their last bound var.
func (c *Registry) Resolve(ref TypeRef) TypeRef {
	// Ignore non-vars.
	if !ref.IsVar() {
		return ref
	}
	other := c.Resolve(c.vars[ref.index()])
	if other == NeverRef {
		return ref
	}
	c.vars[ref.index()] = other
	return other
}

// GetVar returns the TypeRef for an record type.
func (c *Registry) GetVar(ref TypeRef) TypeRef {
	c.Resolve(ref)
	tag, index := ref.extract()
	if tag != varTag {
		return ref
	}
	return c.vars[index]
}

func (c *Registry) IsFree(ref TypeRef) bool {
	return c.Resolve(ref).IsVar()
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

type Replacer func(ref TypeRef) TypeRef

func (c *Registry) replace(target TypeRef, f Replacer) TypeRef {
	tag, index := target.extract()
	switch tag {
	case listTag:
		return c.List(f(c.lists[index]))
	case funcTag:
		fn := c.funcs[index]
		return c.Func(f(fn.Arg), f(fn.Result))
	case enumTag:
		ref := make(MapRef, len(c.enums[index]))
		for k, v := range c.enums[index] {
			ref[k] = f(v)
		}
		return c.Enum(ref)
	case recordTag:
		ref := make(MapRef, len(c.records[index]))
		for k, v := range c.records[index] {
			ref[k] = f(v)
		}
		return c.Record(ref)
	}

	// Else, the target remains unchanged.
	return target
}

// bind binds a free variable to a type.
func (reg *Registry) bind(a, b TypeRef) {
	// Get to the bottom of `a`.
	a = reg.Resolve(a)

	if !a.IsVar() {
		panic("cannot bind non-free var " + reg.String(a))
	}
	reg.vars[a.index()] = b
}

// The opposite of instantiate.
func (c *Registry) generalize(target TypeRef) TypeRef {
	var subst Subst
	return c.replace(target, func(other TypeRef) TypeRef {
		if other.IsVar() {
			b := subst.bound(other)
			if b == NeverRef {
				b = c.Unbound()
				subst.bind(other, b)
			}
			return b
		}
		return other
	})
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

func (c *Registry) unify(a, b TypeRef) {
	a = c.Resolve(a)
	b = c.Resolve(b)

	tag, index := a.extract()
	if tag == unboundTag {
		panic("unexpected unbound var during unification")
	}

	if tag == varTag {
		c.traverse(b, func(ref TypeRef) {
			if a == ref {
				panic("occurs check failed")
			}
		})
		c.vars[index] = b
		return
	}

	if b.IsVar() {
		c.unify(b, a)
		return
	}

	bTag, bIndex := b.extract()
	if tag == bTag {
		switch tag {
		case funcTag:
			aFn := c.GetFunc(a)
			bFn := c.GetFunc(b)

			c.unify(aFn.Arg, bFn.Arg)
			c.unify(aFn.Result, bFn.Result)
		case listTag:
			c.unify(c.GetList(a), c.GetList(b))
		case recordTag:
			c.unify(c.GetList(a), c.GetList(b))
		case primitiveTag:
			if index != bIndex {
				panic("cannot unify '" + c.String(a) + "' with '" + c.String(b) + "'")
			}
		}
	} else {
		panic("cannot unify '" + c.String(a) + "' with '" + c.String(b) + "'")
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

// DebugString returns a string representation for TypeRef.
func (reg *Registry) DebugString() string {
	var s stringer
	s.reg = reg

	s.WriteString("Vars:\n")
	for i := range reg.vars {
		s.WriteString("  $")
		s.WriteString(strconv.Itoa(i))
		s.WriteString(": ")
		s.string(makeTypeRef(varTag, i), 0)
		s.WriteString("\n")
	}
	s.WriteString("Functions:\n")
	for i := range reg.funcs {
		s.WriteString("  ")
		s.WriteString(strconv.Itoa(i))
		s.WriteString(": ")
		s.string(makeTypeRef(funcTag, i), 0)
		s.WriteString("\n")
	}
	s.WriteString("Lists:\n")
	for i := range reg.lists {
		s.WriteString("  ")
		s.WriteString(strconv.Itoa(i))
		s.WriteString(": ")
		s.string(makeTypeRef(listTag, i), 0)
		s.WriteString("\n")
	}

	return s.String()
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
		b.unbound(index)
	case varTag:
		ref := b.reg.GetVar(ref)
		if ref == NeverRef {
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(index))
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
