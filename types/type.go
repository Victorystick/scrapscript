package types

import (
	"maps"
	"slices"
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
)

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

func (ref TypeRef) hasTag(t tag) bool {
	return tag(ref&0x0f) == t
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
