package eval

import (
	"bytes"
	"encoding/base64"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Values

type Value interface {
	String() string
	eq(other Value) bool
}

type Hole struct{}
type Int int
type Float float64
type Text string
type Byte byte
type Bytes []byte

// Is expected to contain functions.
type Enum map[string]Value

type Record map[string]Value

type List []Value

type Variant struct {
	tag   string
	value Value
}

// The type of a function that can be evaluated.
type Func func(Value) (Value, error)

// A built-in function.
type BuiltInFunc struct {
	name string
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
	case Enum:
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
func (i Enum) eq(other Value) bool {
	o, ok := other.(Enum)
	return ok && maps.EqualFunc(i, o, Equals)
}
func (i Record) eq(other Value) bool {
	o, ok := other.(Record)
	return ok && maps.EqualFunc(i, o, Equals)
}
func (l List) eq(other Value) bool {
	o, ok := other.(List)
	return ok && slices.EqualFunc(l, o, Equals)
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

// String

func (h Hole) String() string {
	return "()"
}
func (i Int) String() string {
	return strconv.FormatInt(int64(i), 10)
}
func (f Float) String() string {
	return strconv.FormatFloat(float64(f), 'f', -1, 64)
}
func (t Text) String() string {
	return strconv.QuoteToGraphic(string(t))
}
func (b Byte) String() string {
	return "~" + strconv.FormatInt(int64(b), 16)
}
func (bs Bytes) String() string {
	return "~~" + base64.StdEncoding.EncodeToString(bs)
}
func (e Enum) String() string {
	var b strings.Builder
	space := len(e) - 1
	for _, key := range slices.Sorted(maps.Keys(e)) {
		val := e[key]
		b.WriteString(val.String())

		if space > 0 {
			space -= 1
			b.WriteByte(' ')
		}
	}
	return b.String()
}
func (r Record) String() string {
	var b strings.Builder
	b.WriteString("{ ")
	comma := len(r) - 1
	for key, val := range r {
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
	if len(l) == 0 {
		return "[]"
	}

	var b strings.Builder
	b.WriteString("[ ")
	comma := len(l) - 1
	for _, val := range l {
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
