package eval

import (
	"bytes"
	"encoding/base64"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/Victorystick/scrapscript/types"
)

// Values

type Value interface {
	Type() types.TypeRef
	String() string
	eq(other Value) bool
}

type Hole struct{}
type Int int
type Float float64
type Text string
type Byte byte
type Bytes []byte

// A named type that may be referenced in e.g. a pick expression.
type Type types.TypeRef

type Record struct {
	typ    types.TypeRef
	values map[string]Value
}

func (r Record) Get(key string) Value {
	if val, ok := r.values[key]; ok {
		return val
	}
	return Hole{}
}

type List struct {
	typ      types.TypeRef
	elements []Value
}

type Variant struct {
	typ   types.TypeRef
	tag   string
	value Value
}

// The type of a function that can be evaluated.
type Func func(Value) (Value, error)

// A built-in function.
type BuiltInFunc struct {
	name string
	typ  types.TypeRef
	fn   Func
}

// A user-defined function.
type ScriptFunc struct {
	source string
	fn     Func
}

func Equals(a, b Value) bool {
	switch a.(type) {
	case Hole:
		return a.eq(b)
	case Int:
		return a.eq(b)
	case Float:
		return a.eq(b)
	case Text:
		return a.eq(b)
	case Byte:
		return a.eq(b)
	case Bytes:
		return a.eq(b)
	case Type:
		return a.eq(b)
	case Record:
		return a.eq(b)
	case List:
		return a.eq(b)
	case Variant:
		return a.eq(b)
	case BuiltInFunc:
		return a.eq(b)
	case ScriptFunc:
		return a.eq(b)
	}
	return false
}

func (h Hole) eq(other Value) bool {
	_, ok := other.(Hole)
	return ok
}
func (i Int) eq(other Value) bool {
	o, ok := other.(Int)
	return ok && i == o
}
func (f Float) eq(other Value) bool {
	o, ok := other.(Float)
	return ok && f == o
}
func (t Text) eq(other Value) bool {
	o, ok := other.(Text)
	return ok && t == o
}
func (b Byte) eq(other Value) bool {
	o, ok := other.(Byte)
	return ok && b == o
}
func (bs Bytes) eq(other Value) bool {
	o, ok := other.(Bytes)
	return ok && bytes.Equal(bs, o)
}
func (t Type) eq(other Value) bool {
	o, ok := other.(Type)
	return ok && t == o
}
func (i Record) eq(other Value) bool {
	o, ok := other.(Record)
	return ok && i.typ == o.typ &&
		maps.EqualFunc(i.values, o.values, Equals)
}
func (l List) eq(other Value) bool {
	o, ok := other.(List)
	return ok && l.typ == o.typ &&
		slices.EqualFunc(l.elements, o.elements, Equals)
}
func (v Variant) eq(other Value) bool {
	o, ok := other.(Variant)
	return ok && v.tag == o.tag && Equals(v.value, o.value)
}
func (bf BuiltInFunc) eq(other Value) bool {
	o, ok := other.(BuiltInFunc)
	return ok && bf.name == o.name
}
func (sf ScriptFunc) eq(other Value) bool {
	o, ok := other.(ScriptFunc)
	// TODO: This is very incomplete.
	return ok && sf.source == o.source
}

// Type
func (h Hole) Type() types.TypeRef   { return types.HoleRef }
func (i Int) Type() types.TypeRef    { return types.IntRef }
func (f Float) Type() types.TypeRef  { return types.FloatRef }
func (t Text) Type() types.TypeRef   { return types.TextRef }
func (b Byte) Type() types.TypeRef   { return types.ByteRef }
func (bs Bytes) Type() types.TypeRef { return types.BytesRef }
func (t Type) Type() types.TypeRef {
	// TODO: Should a type return itself, or a special type?
	return types.NeverRef
}
func (r Record) Type() types.TypeRef       { return r.typ }
func (l List) Type() types.TypeRef         { return l.typ }
func (v Variant) Type() types.TypeRef      { return v.typ }
func (bf BuiltInFunc) Type() types.TypeRef { return bf.typ }
func (sf ScriptFunc) Type() types.TypeRef {
	// TODO: implement
	return types.NeverRef
}

// String

func (h Hole) String() string {
	return "()"
}
func (i Int) String() string {
	return strconv.FormatInt(int64(i), 10)
}
func (f Float) String() (res string) {
	res = strconv.FormatFloat(float64(f), 'f', -1, 64)
	// Ensure we always have a decimal.
	if strings.IndexByte(res, '.') < 0 {
		res += ".0"
	}
	return
}
func (t Text) String() string {
	return strconv.QuoteToGraphic(string(t))
}
func (b Byte) String() string {
	const chars = "0123456789ABCDEF"
	return string([]byte{'~', chars[b>>4], chars[b&0xf]})
}
func (bs Bytes) String() string {
	return "~~" + base64.StdEncoding.EncodeToString(bs)
}
func (t Type) String() string {
	return "<type>"
}
func (r Record) String() string {
	var b strings.Builder
	b.WriteString("{ ")
	comma := len(r.values) - 1
	for _, key := range slices.Sorted(maps.Keys(r.values)) {
		val := r.values[key]
		b.WriteString(key)
		b.WriteString(" = ")
		b.WriteString(val.String())

		if comma > 0 {
			comma -= 1
			b.WriteString(", ")
		}
	}
	b.WriteString(" }")
	return b.String()
}
func (l List) String() string {
	if len(l.elements) == 0 {
		return "[]"
	}

	var b strings.Builder
	b.WriteString("[ ")
	comma := len(l.elements) - 1
	for _, val := range l.elements {
		b.WriteString(val.String())

		if comma > 0 {
			comma -= 1
			b.WriteString(", ")
		}
	}
	b.WriteString(" ]")
	return b.String()
}
func (v Variant) String() string {
	value := ""
	if v.value != nil {
		value = " " + v.value.String()
	}
	return "#" + v.tag + value
}
func (bf BuiltInFunc) String() string {
	return bf.name
}
func (sf ScriptFunc) String() string {
	return sf.source
}

func Callable(val Value) Func {
	if f, ok := val.(ScriptFunc); ok {
		return f.fn
	}
	if f, ok := val.(BuiltInFunc); ok {
		return f.fn
	}
	return nil
}
