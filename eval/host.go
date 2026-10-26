package eval

import (
	"fmt"
	"slices"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/label"
	"github.com/Victorystick/scrapscript/types"
)

// The API for hosts: Go code that runs scraps, such as a platform's
// bindings. A host checks a scrap against an interface type once, with
// Check, and then builds, reads and calls its values without checking
// them again. The types of the values it passes in must agree with the
// interface; the evaluator trusts them as it trusts the checker.

// Labels and tags are IDs in the environment's table; Environment.Label
// returns them by name.

// A Shape makes records with the same labels. A host makes one per kind
// of record, once, and gives the values of each record in the order that
// it gave the labels in.
type Shape struct {
	shape *shape
	// The index in a record of each value given to New, or nil if they are
	// in the record's order.
	order []int
}

// Shape returns the shape of records with the labels, which must be
// distinct.
func (e *Environment) Shape(labels ...label.ID) *Shape {
	sorted := slices.Clone(labels)
	e.shapes.sort(sorted)
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			panic(fmt.Sprintf("eval.Shape: %s is given twice", e.reg.Labels().Name(sorted[i])))
		}
	}
	s := &Shape{shape: e.shapes.of(sorted)}
	if !slices.Equal(labels, sorted) {
		s.order = make([]int, len(labels))
		for i, l := range labels {
			s.order[i] = s.shape.index(l)
		}
	}
	return s
}

// New returns a record of the shape with the values, one per label, in
// the order that Shape was given the labels in. It takes ownership of
// values.
func (s *Shape) New(values ...Value) Value {
	if len(values) != len(s.shape.labels) {
		panic(fmt.Sprintf("eval.Shape.New: %d values for %d labels", len(values), len(s.shape.labels)))
	}
	if s.order == nil {
		return Value{recordVal(s.shape, fromHost(values))}
	}
	r := make([]value, len(values))
	for i, v := range values {
		r[s.order[i]] = v.v
	}
	return Value{recordVal(s.shape, r)}
}

// A Value is a value of a scrap, as a host sees it. The host knows its type
// from the interface it checked the scrap against, and reads it as that
// type; reading it as another panics. The zero Value is no value.
type Value struct {
	v value
}

// IsValid returns true if v is a value, rather than the zero Value.
func (v Value) IsValid() bool {
	return v.v.valid()
}

// as panics unless v is of the kind k.
func (v Value) as(k kind) value {
	if got := v.v.kind(); got != k {
		panic(fmt.Sprintf("eval: reading %s as %s", got, k))
	}
	return v.v
}

// Hole returns ().
func Hole() Value {
	return Value{hole}
}

// Int returns the int i.
func Int(i int64) Value {
	return Value{intVal(int(i))}
}

// Int returns the int that v is.
func (v Value) Int() int64 {
	return int64(v.as(kindInt).asInt())
}

// Float returns the float f.
func Float(f float64) Value {
	return Value{floatVal(f)}
}

// Float returns the float that v is.
func (v Value) Float() float64 {
	return v.as(kindFloat).asFloat()
}

// Text returns the text t.
func Text(t string) Value {
	return Value{textVal(t)}
}

// Text returns the text that v is.
func (v Value) Text() string {
	return v.as(kindText).asText()
}

// Byte returns the byte b.
func Byte(b byte) Value {
	return Value{byteVal(b)}
}

// Byte returns the byte that v is.
func (v Value) Byte() byte {
	return v.as(kindByte).asByte()
}

// Bytes returns the bytes b, which it takes ownership of.
func Bytes(b []byte) Value {
	return Value{bytesVal(b)}
}

// Bytes returns the bytes that v is, which the caller must not modify.
func (v Value) Bytes() []byte {
	return v.as(kindBytes).asBytes()
}

// A Record is a record, as a host reads it.
type Record struct {
	r record
}

// Record returns the record that v, a value of this environment, is.
func (e *Environment) Record(v Value) Record {
	return Record{e.shapes.record(v.as(kindRecord))}
}

// Get returns the value of a record's field, or the zero Value if it has
// none.
func (r Record) Get(l label.ID) Value {
	if i := r.r.shape.index(l); i >= 0 {
		return Value{r.r.values[i]}
	}
	return Value{}
}

// NewList returns a list of the elements, which it takes ownership of.
func NewList(elems ...Value) Value {
	return Value{listVal(fromHost(elems))}
}

// A List is a list, as a host reads it.
type List struct {
	elems []value
}

// List returns the list that v is.
func (v Value) List() List {
	return List{v.as(kindList).asList()}
}

// Elements returns the elements of a list, which the caller must not
// modify.
func (l List) Elements() []Value {
	return toHost(l.elems)
}

// NewVariant returns the tag with a payload, or without one if payload is
// the zero Value.
func NewVariant(tag label.ID, payload Value) Value {
	return Value{variantVal(tag, payload.v)}
}

// A Variant is a tag with a payload or without one, as a host reads it.
type Variant struct {
	v value
}

// Variant returns the variant that v is.
func (v Value) Variant() Variant {
	return Variant{v.as(kindVariant)}
}

// Tag returns a variant's tag.
func (v Variant) Tag() label.ID {
	return v.v.tag()
}

// Payload returns a variant's payload, or the zero Value if it has none.
func (v Variant) Payload() Value {
	return Value{v.v.payload()}
}

// NewFunc returns a function implemented by the host, which takes arity
// arguments one at a time, as scraps call functions, and passes them to fn
// together. An error from fn fails the evaluation that called it. The
// function prints as name.
func NewFunc(name string, arity int, fn func(args []Value) (Value, error)) Value {
	if arity < 1 {
		panic(fmt.Sprintf("eval.NewFunc: %s has arity %d", name, arity))
	}
	call := func(args []value) value {
		v, err := fn(toHost(args))
		if err != nil {
			panic(runtimeError{fmt.Errorf("%s: %w", name, err)})
		}
		return v.v
	}
	if arity == 1 {
		return Value{builtinVal(&builtinFunc{name: name, arity: 1, one: func(arg value) value {
			return call([]value{arg})
		}})}
	}
	return Value{builtinVal(&builtinFunc{name: name, arity: arity, many: call})}
}

// Call applies the function fn to args, all at once if it takes as many,
// without checking their types. It fails if the evaluation does, as when no arm matches or a
// builtin fails.
func Call(fn Value, args ...Value) (v Value, err error) {
	defer catch(&err)
	if len(args) == 0 {
		return fn, nil
	}
	return Value{call(fn.v, fromHost(args)...)}, nil
}

// Check checks that the value of the scrap s has the type that the type
// scrap iface denotes. If iface is a type constructor, s chooses its
// arguments, as an echo program chooses the state of a terminal program.
func (e *Environment) Check(s, iface *Scrap) error {
	ires, err := e.infer(iface)
	if err != nil {
		return err
	}
	if !ires.Sort {
		return fmt.Errorf("cannot check against a value: the interface must be a type")
	}
	res, err := e.infer(s)
	if err != nil {
		return err
	}
	if res.Sort {
		return fmt.Errorf("cannot check a type against an interface")
	}
	want, got := e.reg.Instantiate(ires.Type), e.reg.Instantiate(res.Type)
	if e.reg.Unify(want, got) != nil {
		strs, where := e.reg.Strings(got, want)
		return fmt.Errorf("does not implement the interface: '%s' is not '%s'%s", strs[0], strs[1], where)
	}
	// The host passes the values that s makes back into it, so nothing else
	// can add to the types that s chose. A row still open would let a
	// catch-all arm take tags that no arm names, such as a misspelt one.
	if open, ok := e.reg.OpenRow(want); ok {
		strs, where := e.reg.Strings(open)
		return fmt.Errorf("does not implement the interface: '%s' is open; name its labels in an annotation, or match it without a catch-all arm%s", strs[0], where)
	}
	return nil
}

// TypeOf checks the scrap s and returns its result, in the environment's
// registry of types, which the caller must only read.
func (e *Environment) TypeOf(s *Scrap) (*types.Registry, types.Result, error) {
	res, err := e.infer(s)
	return &e.reg, res, err
}

// Fetch returns the published scrap with the hash digest in the scheme,
// such as "source".
func (e *Environment) Fetch(scheme, digest string) (*Scrap, error) {
	return e.fetch(scheme, digest)
}

// Source returns the source of a scrap.
func (s *Scrap) Source() []byte {
	return s.expr.Source.Bytes()
}

// Expr returns the parsed expression of a scrap, whose nodes index the Nodes
// of its result from TypeOf. The caller must only read it.
func (s *Scrap) Expr() *ast.SourceExpr {
	return &s.expr
}

// Closure returns the scraps that s refers to, directly or not, each once,
// after the scraps they refer to. All of them, and s itself, must refer to
// others only by hash, so that they can be published or embedded as they
// are.
func (e *Environment) Closure(s *Scrap) ([]*Scrap, error) {
	if err := e.load(s); err != nil {
		return nil, err
	}
	var order []*Scrap
	seen := make(map[*Scrap]bool)
	var visit func(s *Scrap) error
	visit = func(s *Scrap) error {
		for _, n := range s.expr.Imports() {
			if _, ok := s.expr.Path(n); ok {
				return s.expr.Error(n, "cannot embed a scrap that refers to a path")
			}
			dep := s.deps[n]
			if !seen[dep] {
				seen[dep] = true
				if err := visit(dep); err != nil {
					return err
				}
				order = append(order, dep)
			}
		}
		return nil
	}
	if err := visit(s); err != nil {
		return nil, err
	}
	return order, nil
}
