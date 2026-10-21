package eval

// Values

// A value is the result of an evaluation; unsafe.go has its
// representation. Records and variants hold labels as IDs in their
// environment's table, so they print through it, with Environment.Print.

// A kind of value.
type kind uint8

const (
	kindNone kind = iota // the zero value
	kindHole
	kindInt
	kindFloat
	kindText
	kindByte
	kindBytes
	kindRecord
	kindList
	kindVariant
	kindBuiltin
	kindPartial
	kindScript
	numKinds
)

var kindNames = [numKinds]string{"no value", "()", "int", "float", "text", "byte", "bytes", "record", "list", "variant", "function", "function", "function"}

func (k kind) String() string {
	return kindNames[k]
}

// A record is a record's shape and values, as read from a record value.
type record struct {
	shape  *shape
	values []value
}

// record returns the record that v, a record of a shape in t, is.
func (t *shapeTable) record(v value) record {
	return record{t.at(v.shapeID()), v.fields()}
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
func (bf *builtinFunc) invoke(args []value) value {
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
	fn   *builtinFunc
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
func (sf *scriptFunc) composition() bool {
	return sf.prog.ops[sf.node] == opCompose
}
