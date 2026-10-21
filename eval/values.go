package eval

import (
	"github.com/Victorystick/scrapscript/label"
)

// Values

// A value is the result of an evaluation. Records and variants hold labels
// as IDs in their environment's table, so they print through it, with
// Environment.Print.
type value interface {
	isValue()
}

type holeValue struct{}
type intValue int
type floatValue float64
type textValue string
type byteValue byte
type bytesValue []byte

// A record holds its values in the order of its shape's labels. Record
// is a pointer, so that a value holds one without boxing it.
type Record struct {
	*record
}

type record struct {
	shape  *shape
	values []value
}

// A record allocated together with its values.
type recordWith[A any] struct {
	record
	buf A
}

// newRecord returns a record of the shape s, whose values the caller sets.
// A record of up to eight values is one allocation.
func newRecord(s *shape) Record {
	switch len(s.labels) {
	case 1:
		return inlineRecord(s, func(a *[1]value) []value { return a[:] })
	case 2:
		return inlineRecord(s, func(a *[2]value) []value { return a[:] })
	case 3:
		return inlineRecord(s, func(a *[3]value) []value { return a[:] })
	case 4:
		return inlineRecord(s, func(a *[4]value) []value { return a[:] })
	case 5:
		return inlineRecord(s, func(a *[5]value) []value { return a[:] })
	case 6:
		return inlineRecord(s, func(a *[6]value) []value { return a[:] })
	case 7:
		return inlineRecord(s, func(a *[7]value) []value { return a[:] })
	case 8:
		return inlineRecord(s, func(a *[8]value) []value { return a[:] })
	}
	return Record{&record{s, make([]value, len(s.labels))}}
}

func inlineRecord[A any](s *shape, values func(*A) []value) Record {
	r := &recordWith[A]{}
	r.shape, r.values = s, values(&r.buf)
	return Record{&r.record}
}

type List struct {
	elements []value
}

type Variant struct {
	tag   label.ID
	value value
}

// A built-in function, or one that a host implements, which takes arity
// arguments. A builtin takes them as Go arguments, so that a call with all
// of them needs no slice; a host function of more than one takes a slice.
type builtinFunc struct {
	name  string
	arity int
	one   func(value) value               // if arity is 1
	two   func(value, value) value        // a builtin of arity 2
	three func(value, value, value) value // a builtin of arity 3
	many  func([]value) value             // a host function of arity 2 or more
}

// invoke calls bf, which takes more than one argument, with all of them.
func (bf builtinFunc) invoke(args []value) value {
	switch {
	case bf.two != nil:
		return bf.two(args[0], args[1])
	case bf.three != nil:
		return bf.three(args[0], args[1], args[2])
	}
	return bf.many(args)
}

// A built-in function applied to some, but not all, of its arguments.
type partial struct {
	fn   builtinFunc
	args []value
}

// A user-defined function: a lambda or composition node of a program,
// with what it captured when it was made.
type scriptFunc struct {
	prog *program
	node cnode
	// For a lambda, the values of the names it uses from outside it, in
	// the order of its captures. For a composition, its functions, in the
	// order they are applied.
	caps []value
	// For a lambda, the arguments it has been applied to, fewer than its
	// arity.
	args []value
}

// composition returns true if sf is a composition rather than a lambda.
func (sf scriptFunc) composition() bool {
	return sf.prog.ops[sf.node] == opCompose
}

func (holeValue) isValue()   {}
func (intValue) isValue()    {}
func (floatValue) isValue()  {}
func (textValue) isValue()   {}
func (byteValue) isValue()   {}
func (bytesValue) isValue()  {}
func (Record) isValue()      {}
func (List) isValue()        {}
func (Variant) isValue()     {}
func (builtinFunc) isValue() {}
func (partial) isValue()     {}
func (scriptFunc) isValue()  {}
