package types

import (
	"errors"
	"maps"
	"slices"
	"strings"
)

// TypeRef references a type node within a Registry.
//
// The zero value, NoType, references no type at all. It is invalid to use as
// a type.
type TypeRef uint32

type kind uint8

const (
	conKind    kind = iota // A primitive type, one of the fixed refs below.
	varKind                // An unbound type variable. aux: its level.
	linkKind               // A bound type variable. a: what it is bound to.
	genKind                // A generalized type variable of a type scheme.
	listKind               // a: the element type.
	funcKind               // a: the argument type. b: the result type.
	recordKind             // a: the row of fields.
	enumKind               // a: the row of tags.
	emptyKind              // The empty row, which closes a row.
	extendKind             // aux: the label. a: its type. b: the rest of the row.
	rigidKind              // A rigid type variable, equal only to itself. aux: its level. a: its name's label.
)

// A node is a type in a Registry. Its kind says how it uses the other fields.
type node struct {
	kind kind
	aux  uint32
	a, b TypeRef
}

// The fixed refs of the primitive types and the empty row.
const (
	NoType TypeRef = iota
	HoleRef
	IntRef
	FloatRef
	TextRef
	ByteRef
	BytesRef
	emptyRef
	numFixed
)

var primitives = [...]TypeRef{HoleRef, IntRef, FloatRef, TextRef, ByteRef, BytesRef}

// The names of the primitive types, by TypeRef.
var primitiveNames = [...]string{
	NoType: "",
	"()",
	"int",
	"float",
	"text",
	"byte",
	"bytes",
}

// Registry holds the types of a running application as a graph of nodes.
//
// Type variables are bound by overwriting them in place, so a TypeRef must
// be resolved before its node is inspected. Each variable has the level of
// the where-binding it was created in. When a binding is done, the variables
// above the current level are local to it, and are generalized.
type Registry struct {
	nodes  []node
	labels []string
	label  map[string]uint32
	level  uint32
	// While unifying, the nodes overwritten so far and their old contents,
	// so that a failed unification can be undone.
	unifying bool
	trail    []undo
}

type undo struct {
	ref TypeRef
	old node
}

// set overwrites a node, remembering its old contents.
func (r *Registry) set(ref TypeRef, n node) {
	if r.unifying {
		r.trail = append(r.trail, undo{ref, r.nodes[ref]})
	}
	r.nodes[ref] = n
}

func (r *Registry) at(ref TypeRef) node {
	if ref < numFixed {
		if ref == emptyRef {
			return node{kind: emptyKind}
		}
		return node{}
	}
	return r.nodes[ref]
}

func (r *Registry) add(n node) TypeRef {
	if len(r.nodes) == 0 {
		r.nodes = make([]node, numFixed, 64)
		r.nodes[emptyRef].kind = emptyKind
	}
	r.nodes = append(r.nodes, n)
	return TypeRef(len(r.nodes) - 1)
}

func (r *Registry) intern(label string) uint32 {
	if i, ok := r.label[label]; ok {
		return i
	}
	if r.label == nil {
		r.label = make(map[string]uint32)
	}
	i := uint32(len(r.labels))
	r.labels = append(r.labels, label)
	r.label[label] = i
	return i
}

// Size returns the number of nodes in the registry, for debugging.
func (r *Registry) Size() int {
	return len(r.nodes)
}

// List returns a list type.
func (r *Registry) List(elem TypeRef) TypeRef {
	return r.add(node{kind: listKind, a: elem})
}

// Func returns a function type.
func (r *Registry) Func(arg, result TypeRef) TypeRef {
	return r.add(node{kind: funcKind, a: arg, b: result})
}

// Record returns a closed record type with the given fields.
func (r *Registry) Record(fields map[string]TypeRef) TypeRef {
	return r.add(node{kind: recordKind, a: r.row(fields, emptyRef)})
}

// Enum returns a closed enum type with the given tags. A tag without a
// payload has the payload (), as in `#tag ()`.
func (r *Registry) Enum(tags map[string]TypeRef) TypeRef {
	return r.add(node{kind: enumKind, a: r.row(tags, emptyRef)})
}

func (r *Registry) row(labels map[string]TypeRef, tail TypeRef) TypeRef {
	for _, l := range slices.Sorted(maps.Keys(labels)) {
		tail = r.extend(r.intern(l), labels[l], tail)
	}
	return tail
}

func (r *Registry) extend(label uint32, typ, rest TypeRef) TypeRef {
	return r.add(node{kind: extendKind, aux: label, a: typ, b: rest})
}

// Var returns a new unbound type variable.
func (r *Registry) Var() TypeRef {
	return r.add(node{kind: varKind, aux: r.level})
}

// rigid returns a new rigid type variable named name, which stands for a type
// that an annotation leaves to its user, so it is equal only to itself.
func (r *Registry) rigid(name string) TypeRef {
	return r.add(node{kind: rigidKind, aux: r.level, a: TypeRef(r.intern(name))})
}

// Gen returns a new generalized type variable, for building type schemes.
// Each use of a scheme replaces its generalized variables with fresh ones.
func (r *Registry) Gen() TypeRef {
	return r.add(node{kind: genKind})
}

// enter starts a where-binding, whose new variables may be generalized.
func (r *Registry) enter() { r.level++ }

// exit ends a where-binding.
func (r *Registry) exit() { r.level-- }

// Resolve follows bound variables to the type they are bound to.
func (r *Registry) Resolve(ref TypeRef) TypeRef {
	n := r.at(ref)
	if n.kind != linkKind {
		return ref
	}
	target := r.Resolve(n.a)
	if target != n.a {
		n.a = target
		r.set(ref, n)
	}
	return target
}

func (r *Registry) link(v, to TypeRef) {
	r.set(v, node{kind: linkKind, a: to})
}

// generalize turns the variables of ref that are local to the current
// binding into generalized ones, in place.
func (r *Registry) generalize(ref TypeRef) {
	ref = r.Resolve(ref)
	n := r.at(ref)
	switch n.kind {
	case varKind:
		if n.aux > r.level {
			r.nodes[ref].kind = genKind
		}
	case listKind, recordKind, enumKind:
		r.generalize(n.a)
	case funcKind, extendKind:
		r.generalize(n.a)
		r.generalize(n.b)
	}
}

// Instantiate returns a copy of a type scheme with fresh variables in place
// of its generalized ones. Parts without generalized variables are shared.
func (r *Registry) Instantiate(ref TypeRef) TypeRef {
	var fresh map[TypeRef]TypeRef
	return r.instantiate(ref, &fresh)
}

func (r *Registry) instantiate(ref TypeRef, fresh *map[TypeRef]TypeRef) TypeRef {
	ref = r.Resolve(ref)
	n := r.at(ref)
	switch n.kind {
	case genKind:
		if *fresh == nil {
			*fresh = make(map[TypeRef]TypeRef)
		}
		v, ok := (*fresh)[ref]
		if !ok {
			v = r.Var()
			(*fresh)[ref] = v
		}
		return v
	case listKind, recordKind, enumKind:
		a := r.instantiate(n.a, fresh)
		if a == n.a {
			return ref
		}
		n.a = a
		return r.add(n)
	case funcKind, extendKind:
		a, b := r.instantiate(n.a, fresh), r.instantiate(n.b, fresh)
		if a == n.a && b == n.b {
			return ref
		}
		n.a, n.b = a, b
		return r.add(n)
	}
	return ref
}

// Errors from unifying rows, which the enclosing record or enum turns into
// messages, since rows have no syntax of their own.
var (
	// One row lacks a label of the other.
	errMissingLabel = errors.New("missing label")
	// The rows can only be equal if they are infinite.
	errInfiniteRow = errors.New("infinite row")
	// A variable occurs in the type it is bound to.
	errOccurs = errors.New("occurs")
)

// Unify makes a and b the same type by binding their variables, or returns
// an error if they can't be.
//
// Unification is atomic: if it fails, a and b are left as they were, so
// that the error shows them as they were.
func (r *Registry) Unify(a, b TypeRef) error {
	r.unifying = true
	err := r.rowError(a, b, r.unify(a, b))
	r.unifying = false
	if err != nil {
		for i := len(r.trail) - 1; i >= 0; i-- {
			r.nodes[r.trail[i].ref] = r.trail[i].old
		}
	}
	r.trail = r.trail[:0]
	if e, ok := err.(*typeError); ok {
		return e.format(r)
	}
	return err
}

// rowError turns an error from unifying the rows of a and b into a message.
func (r *Registry) rowError(a, b TypeRef, err error) error {
	switch err {
	case errMissingLabel:
		return r.mismatch(a, b)
	case errInfiniteRow:
		return r.errorf("cannot construct infinite type '%s' = '%s'", a, b)
	}
	return err
}

func (r *Registry) unify(a, b TypeRef) error {
	if a == NoType || b == NoType {
		panic("cannot unify NoType")
	}
	a, b = r.Resolve(a), r.Resolve(b)
	if a == b {
		return nil
	}

	na, nb := r.at(a), r.at(b)
	if na.kind == varKind {
		return r.bind(a, b)
	}
	if nb.kind == varKind {
		return r.bind(b, a)
	}
	if na.kind != nb.kind {
		return r.mismatch(a, b)
	}

	switch na.kind {
	case listKind:
		return r.unify(na.a, nb.a)
	case funcKind:
		if err := r.unify(na.a, nb.a); err != nil {
			return err
		}
		return r.unify(na.b, nb.b)
	case recordKind, enumKind:
		return r.rowError(a, b, r.unifyRows(na.a, nb.a))
	case extendKind:
		return r.unifyRows(a, b)
	}
	return r.mismatch(a, b)
}

// bind binds the variable v to the type t.
func (r *Registry) bind(v, t TypeRef) error {
	switch err := r.occurs(v, r.at(v).aux, t); err {
	case nil:
	case errOccurs:
		if k := r.at(t).kind; k == extendKind || k == emptyKind {
			return errInfiniteRow
		}
		return r.errorf("cannot construct infinite type '%s' = '%s'", v, t)
	default:
		return err
	}
	r.link(v, t)
	return nil
}

// occurs returns errOccurs if the variable v occurs in t. It also lowers
// the variables of t to level, since t becomes part of a type that lives as
// long as v. A rigid variable can't be lowered, so if t has one from a
// deeper level, it would escape the annotation that introduced it.
func (r *Registry) occurs(v TypeRef, level uint32, t TypeRef) error {
	t = r.Resolve(t)
	n := r.at(t)
	switch n.kind {
	case varKind:
		if t == v {
			return errOccurs
		}
		if n.aux > level {
			n.aux = level
			r.set(t, n)
		}
	case rigidKind:
		if n.aux > level {
			return r.errorf("type variable '%s' would escape its annotation through '%s'", t, v)
		}
	case listKind, recordKind, enumKind:
		return r.occurs(v, level, n.a)
	case funcKind, extendKind:
		if err := r.occurs(v, level, n.a); err != nil {
			return err
		}
		return r.occurs(v, level, n.b)
	}
	return nil
}

// unifyRows unifies two rows, which may list their labels in any order.
func (r *Registry) unifyRows(a, b TypeRef) error {
	a, b = r.Resolve(a), r.Resolve(b)
	if a == b {
		return nil
	}
	na := r.at(a)
	if na.kind != extendKind {
		if r.at(b).kind == extendKind {
			return r.unifyRows(b, a)
		}
		// Both are empty, variables or rigid; different closed ends differ.
		if na.kind == varKind || r.at(b).kind == varKind {
			return r.unify(a, b)
		}
		return errMissingLabel
	}

	// Find a's first label in b. If that binds the variable that ends a's
	// rest, the rows can only be equal if they are infinite.
	tail := r.rowTail(na.b)
	openTail := r.at(tail).kind == varKind
	typ, rest, err := r.rewrite(b, na.aux)
	if err != nil {
		return err
	}
	if openTail && r.at(tail).kind != varKind {
		return errInfiniteRow
	}
	if err := r.unify(na.a, typ); err != nil {
		return err
	}
	return r.unifyRows(na.b, rest)
}

// rewrite finds label in row, returning its type and the rest of the row.
// If the row is open and lacks the label, its tail is extended with it.
func (r *Registry) rewrite(row TypeRef, label uint32) (typ, rest TypeRef, err error) {
	row = r.Resolve(row)
	n := r.at(row)
	switch n.kind {
	case extendKind:
		if n.aux == label {
			return n.a, n.b, nil
		}
		typ, rest, err = r.rewrite(n.b, label)
		if err != nil {
			return
		}
		return typ, r.extend(n.aux, n.a, rest), nil
	case varKind:
		// The new variables live as long as the row's.
		level := r.level
		r.level = n.aux
		typ, rest = r.Var(), r.Var()
		r.level = level
		r.link(row, r.extend(label, typ, rest))
		return typ, rest, nil
	}
	return 0, 0, errMissingLabel
}

// rowTail returns the empty row or variable that ends a row.
func (r *Registry) rowTail(row TypeRef) TypeRef {
	for {
		row = r.Resolve(row)
		n := r.at(row)
		if n.kind != extendKind {
			return row
		}
		row = n.b
	}
}

// closeEnum closes ref's row if it is an enum whose row ends in a variable.
func (r *Registry) closeEnum(ref TypeRef) {
	n := r.at(r.Resolve(ref))
	if n.kind != enumKind {
		return
	}
	if tail := r.rowTail(n.a); r.at(tail).kind == varKind {
		r.link(tail, emptyRef)
	}
}

func (r *Registry) mismatch(a, b TypeRef) error {
	return r.errorf("cannot unify '%s' with '%s'", a, b)
}

// A typeError is an error about two types, which is formatted once the
// unification that failed has been undone.
type typeError struct {
	msg  string
	a, b TypeRef
}

func (e *typeError) Error() string { return e.msg }

func (r *Registry) errorf(format string, a, b TypeRef) error {
	return &typeError{format, a, b}
}

// format formats the error with its types, which share the names of their
// variables.
func (e *typeError) format(r *Registry) error {
	s := stringer{reg: r}
	s.reserve(e.a)
	s.reserve(e.b)
	msg := strings.Replace(e.msg, "%s", s.string(e.a), 1)
	msg = strings.Replace(msg, "%s", s.string(e.b), 1)
	return errors.New(msg)
}

// Describe returns a string representation of a scrap's result: its type,
// or for a type scrap, the type it denotes, as `type = T`.
func (r *Registry) Describe(res Result) string {
	if res.Sort {
		return "type = " + r.String(res.Type)
	}
	return r.String(res.Type)
}

// String returns a string representation of a type.
func (r *Registry) String(ref TypeRef) string {
	s := stringer{reg: r}
	s.reserve(ref)
	return s.string(ref)
}

const varNames = "abcdefghijklmnopqrstuvwxyz"

type stringer struct {
	strings.Builder
	reg *Registry
	// The names of the variables named so far.
	names map[TypeRef]string
	// The number of names tried so far.
	next int
	// The names of rigid variables, which other variables must not take.
	taken map[string]bool
}

func (s *stringer) string(ref TypeRef) string {
	s.Reset()
	s.write(ref, topLevel)
	return s.String()
}

// reserve reserves the names of the rigid variables in ref.
func (s *stringer) reserve(ref TypeRef) {
	ref = s.reg.Resolve(ref)
	n := s.reg.at(ref)
	switch n.kind {
	case rigidKind:
		if s.taken == nil {
			s.taken = make(map[string]bool)
		}
		s.taken[s.reg.labels[n.a]] = true
	case listKind, recordKind, enumKind:
		s.reserve(n.a)
	case funcKind, extendKind:
		s.reserve(n.a)
		s.reserve(n.b)
	}
}

func (s *stringer) variable(ref TypeRef) {
	name, ok := s.names[ref]
	for !ok {
		i := s.next
		s.next++
		name = string(varNames[i%len(varNames)]) + strings.Repeat("'", i/len(varNames))
		ok = !s.taken[name]
	}
	if s.names == nil {
		s.names = make(map[TypeRef]string)
	}
	s.names[ref] = name
	s.WriteString(name)
}

// Nesting levels, at which compound types need parentheses.
const (
	topLevel  = iota // anywhere else
	argLevel         // a function's argument: functions need them
	atomLevel        // a list's element or a tag's payload: lists too
)

func (s *stringer) write(ref TypeRef, nesting int) {
	ref = s.reg.Resolve(ref)
	n := s.reg.at(ref)
	switch n.kind {
	case conKind:
		if ref == NoType {
			panic("cannot write NoType")
		}
		s.WriteString(primitiveNames[ref])
	case varKind, genKind:
		s.variable(ref)
	case rigidKind:
		s.WriteString(s.reg.labels[n.a])
	case listKind:
		if nesting >= atomLevel {
			s.WriteByte('(')
		}
		s.WriteString("list ")
		s.write(n.a, atomLevel)
		if nesting >= atomLevel {
			s.WriteByte(')')
		}
	case funcKind:
		if nesting >= argLevel {
			s.WriteByte('(')
		}
		s.write(n.a, argLevel)
		s.WriteString(" -> ")
		s.write(n.b, topLevel)
		if nesting >= argLevel {
			s.WriteByte(')')
		}
	case recordKind:
		s.row(n.a, '{', '}', func(label string, typ TypeRef) {
			s.WriteString(label)
			s.WriteString(" : ")
			s.write(typ, topLevel)
		})
	case enumKind:
		s.row(n.a, '[', ']', func(label string, typ TypeRef) {
			s.WriteByte('#')
			s.WriteString(label)
			if s.reg.Resolve(typ) != HoleRef {
				s.WriteByte(' ')
				s.write(typ, atomLevel)
			}
		})
	default:
		panic("bad type-ref")
	}
}

// row writes a row's labels in order, followed by its tail if it is open.
func (s *stringer) row(row TypeRef, open, close byte, label func(string, TypeRef)) {
	type entry struct {
		label string
		typ   TypeRef
	}
	var entries []entry
	for {
		row = s.reg.Resolve(row)
		n := s.reg.at(row)
		if n.kind != extendKind {
			break
		}
		entries = append(entries, entry{s.reg.labels[n.aux], n.a})
		row = n.b
	}
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.label, b.label) })

	s.WriteByte(open)
	for i, e := range entries {
		if i > 0 {
			s.WriteByte(',')
		}
		s.WriteByte(' ')
		label(e.label, e.typ)
	}
	isOpen := s.reg.at(row).kind != emptyKind
	if isOpen {
		if len(entries) > 0 {
			s.WriteByte(',')
		}
		s.WriteString(" ..")
		s.write(row, topLevel)
	}
	if len(entries) > 0 || isOpen {
		s.WriteByte(' ')
	}
	s.WriteByte(close)
}
