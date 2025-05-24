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
	// Lists just have a TypeRef.
	lists []TypeRef
	// Functions map one TypeRef to another.
	funcs []FuncRef
	// Enums and records are maps to TypeRefs.
	enums   []MapRef
	records []MapRef
}

// Strings returns a string representation for TypeRef.
func (c *Registry) String(ref TypeRef) string {
	var b strings.Builder
	c.string(&b, ref, 0)
	return b.String()
}

func (c *Registry) string(b *strings.Builder, ref TypeRef, nesting int) {
	tag, index := ref.extract()
	switch tag {
	case primitiveTag:
		b.WriteString(primitiveNames[index])
	case listTag:
		if nesting > 1 {
			b.WriteByte('(')
		}
		b.WriteString("list ")
		c.string(b, c.lists[index], 2)
		if nesting > 1 {
			b.WriteByte(')')
		}
	case funcTag:
		fn := c.funcs[index]
		if nesting > 0 {
			b.WriteByte('(')
		}
		c.string(b, fn.Arg, 1)
		b.WriteString(" -> ")
		c.string(b, fn.Result, 0)
		if nesting > 0 {
			b.WriteByte(')')
		}
	case enumTag:
		c.enumStr(b, index)
	case recordTag:
		c.recordStr(b, index)
	default:
		// The invalid type.
		panic("bad type-ref")
	}
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

func (c *Registry) enumStr(b *strings.Builder, index int) {
	e := c.enums[index]
	space := len(e) - 1
	for _, key := range slices.Sorted(maps.Keys(e)) {
		b.WriteByte('#')
		b.WriteString(key)

		// Special-case never.
		if e[key] != NeverRef {
			b.WriteByte(' ')
			c.string(b, e[key], 1)
		}

		if space > 0 {
			space -= 1
			b.WriteByte(' ')
		}
	}
}

func (c *Registry) recordStr(b *strings.Builder, index int) {
	r := c.records[index]
	b.WriteString("{ ")
	comma := len(r) - 1
	for _, key := range slices.Sorted(maps.Keys(r)) {
		b.WriteString(key)
		b.WriteString(" : ")
		c.string(b, r[key], 1)

		if comma > 0 {
			comma -= 1
			b.WriteString(", ")
		}
	}
	b.WriteString(" }")
}
