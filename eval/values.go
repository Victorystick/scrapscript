package eval

import (
	"bytes"
	"maps"
	"slices"
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

type Record struct {
	values map[string]Value
}

type List struct {
	elements []Value
}

type Variant struct {
	tag   string
	value Value
}

// A function value, as hosts call it.
type Func func(Value) (Value, error)

// A built-in function.
type BuiltInFunc struct {
	name string
	fn   func(Value) Value
}

// A built-in function applied to some, but not all, of its arguments.
type Partial struct {
	name string
	args []Value
	fn   func(Value) Value
}

// A user-defined function: a lambda or composition node of a program,
// closed over the frame it was made in.
type ScriptFunc struct {
	prog *program
	node cnode
	env  *frame
	// A composition's functions, in the order they are applied.
	composed []Value
	// The hash reference that it was imported by, which it prints as, if it
	// is the value of a published scrap.
	ref string
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
	case Record:
		return a.eq(b)
	case List:
		return a.eq(b)
	case Variant:
		return a.eq(b)
	case BuiltInFunc:
		return a.eq(b)
	case Partial:
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
func (i Record) eq(other Value) bool {
	o, ok := other.(Record)
	return ok && maps.EqualFunc(i.values, o.values, Equals)
}
func (l List) eq(other Value) bool {
	o, ok := other.(List)
	return ok && slices.EqualFunc(l.elements, o.elements, Equals)
}
func (v Variant) eq(other Value) bool {
	o, ok := other.(Variant)
	return ok && v.tag == o.tag && Equals(v.value, o.value)
}
func (bf BuiltInFunc) eq(other Value) bool {
	o, ok := other.(BuiltInFunc)
	return ok && bf.name == o.name
}

// Functions are equal if they print the same.
func (p Partial) eq(other Value) bool {
	o, ok := other.(Partial)
	return ok && p.String() == o.String()
}
func (sf ScriptFunc) eq(other Value) bool {
	o, ok := other.(ScriptFunc)
	return ok && sf.String() == o.String()
}

// String returns a value as a scrap that evaluates to the same value; see
// print.go.

func (h Hole) String() string         { return Print(h) }
func (i Int) String() string          { return Print(i) }
func (f Float) String() string        { return Print(f) }
func (t Text) String() string         { return Print(t) }
func (b Byte) String() string         { return Print(b) }
func (bs Bytes) String() string       { return Print(bs) }
func (r Record) String() string       { return Print(r) }
func (l List) String() string         { return Print(l) }
func (v Variant) String() string      { return Print(v) }
func (bf BuiltInFunc) String() string { return Print(bf) }
func (p Partial) String() string      { return Print(p) }
func (sf ScriptFunc) String() string  { return Print(sf) }

// Callable returns a function value as a Func, or nil if it isn't one.
func Callable(val Value) Func {
	switch val.(type) {
	case ScriptFunc, BuiltInFunc, Partial:
		return func(arg Value) (v Value, err error) {
			defer catch(&err)
			return apply(val, arg), nil
		}
	}
	return nil
}
