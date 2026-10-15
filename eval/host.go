package eval

import (
	"fmt"

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

// A Field is a record field, for NewRecord.
type Field struct {
	Label label.ID
	Value Value
}

// NewRecord returns a record of the fields, whose labels must be distinct.
func (e *Environment) NewRecord(fields ...Field) Record {
	labels := make([]label.ID, len(fields))
	for i, f := range fields {
		labels[i] = f.Label
	}
	e.shapes.sort(labels)
	for i := 1; i < len(labels); i++ {
		if labels[i] == labels[i-1] {
			panic(fmt.Sprintf("eval.NewRecord: %s is set twice", e.reg.Labels().Name(labels[i])))
		}
	}
	s := e.shapes.of(labels)
	values := make([]Value, len(fields))
	for _, f := range fields {
		values[s.index(f.Label)] = f.Value
	}
	return Record{s, values}
}

// Get returns the value of a record's field, or nil if it has none.
func (r Record) Get(l label.ID) Value {
	if i := r.shape.index(l); i >= 0 {
		return r.values[i]
	}
	return nil
}

// NewList returns a list of the elements, which it takes ownership of.
func NewList(elems ...Value) List {
	return List{elems}
}

// Elements returns the elements of a list, which the caller must not
// modify.
func (l List) Elements() []Value {
	return l.elements
}

// NewVariant returns the tag with a payload, or without one if payload is
// nil.
func NewVariant(tag label.ID, payload Value) Variant {
	return Variant{tag, payload}
}

// Tag returns a variant's tag.
func (v Variant) Tag() label.ID {
	return v.tag
}

// Payload returns a variant's payload, or nil if it has none.
func (v Variant) Payload() Value {
	return v.value
}

// NewFunc returns a function implemented by the host, which takes arity
// arguments one at a time, as scraps call functions, and passes them to fn
// together. An error from fn fails the evaluation that called it. The
// function prints as name.
func NewFunc(name string, arity int, fn func(args []Value) (Value, error)) Value {
	if arity < 1 {
		panic(fmt.Sprintf("eval.NewFunc: %s has arity %d", name, arity))
	}
	call := func(args []Value) Value {
		v, err := fn(args)
		if err != nil {
			panic(runtimeError{fmt.Errorf("%s: %w", name, err)})
		}
		return v
	}
	if arity == 1 {
		return BuiltInFunc{name, func(arg Value) Value {
			return call([]Value{arg})
		}}
	}
	return BuiltInFunc{name, collect(name, nil, arity, call)}
}

// collect returns a function that adds its argument to args, and calls
// call once it has arity of them.
func collect(name string, args []Value, arity int, call func([]Value) Value) func(Value) Value {
	return func(arg Value) Value {
		// Each partial application has its own arguments.
		args := append(args[:len(args):len(args)], arg)
		if len(args) == arity {
			return call(args)
		}
		return Partial{name, args, collect(name, args, arity, call)}
	}
}

// Call applies the function fn to args in turn, without checking their
// types. It fails if the evaluation does, as when no arm matches or a
// builtin fails.
func Call(fn Value, args ...Value) (v Value, err error) {
	defer catch(&err)
	v = fn
	for _, arg := range args {
		v = apply(v, arg)
	}
	return v, nil
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
