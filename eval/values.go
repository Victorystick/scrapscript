package eval

import (
	"bytes"
	"slices"

	"github.com/Victorystick/scrapscript/label"
)

// Values

// A Value is the result of an evaluation. Records and variants hold labels
// as IDs in their environment's table, so they print through it, with
// Environment.Print.
type Value interface {
	eq(other Value) bool
}

type Hole struct{}
type Int int
type Float float64
type Text string
type Byte byte
type Bytes []byte

// A record holds its values in the order of its shape's labels.
type Record struct {
	shape  *shape
	values []Value
}

type List struct {
	elements []Value
}

type Variant struct {
	tag   label.ID
	value Value
}

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
	return ok && i.shape == o.shape && slices.EqualFunc(i.values, o.values, Equals)
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
	return ok && p.name == o.name && slices.EqualFunc(p.args, o.args, Equals)
}
func (sf ScriptFunc) eq(other Value) bool {
	o, ok := other.(ScriptFunc)
	return ok && printValue(sf.prog.labels, sf) == printValue(o.prog.labels, o)
}
