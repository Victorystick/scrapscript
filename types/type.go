package types

import (
	"errors"
	"iter"
	"maps"
	"slices"
	"strings"

	"github.com/Victorystick/scrapscript/label"
)

// TypeRef references a type node within a Registry.
//
// The zero value, NoType, references no type at all. It is invalid to use as
// a type.
type TypeRef uint32

type kind uint8

const (
	conKind    kind = iota // A primitive type, one of the fixed refs below.
	varKind                // An unbound type variable. aux: its level. a: its lacks.
	linkKind               // A bound type variable. a: what it is bound to.
	genKind                // A generalized type variable of a type scheme. a: its lacks.
	listKind               // a: the element type.
	funcKind               // a: the argument type. b: the result type.
	recordKind             // a: the row of fields.
	enumKind               // a: the row of tags.
	emptyKind              // The empty row, which closes a row.
	extendKind             // aux: the label. a: its field. b: the rest of the row.
	fieldKind              // An entry of a row. a: its payload. b: its flag: presentRef, absentRef or a variable.
	rigidKind              // A rigid type variable, equal only to itself. aux: its level. a: its name's label. b: its lacks.
)

// A node is a type in a Registry. Its kind says how it uses the other fields.
type node struct {
	kind kind
	aux  uint32
	a, b TypeRef
}

// label returns the label of an extend node.
func (n node) label() label.ID {
	return label.ID(n.aux)
}

// name returns the name of a rigid node.
func (n node) name() label.ID {
	return label.ID(n.a)
}

// The fixed refs of the primitive types, the empty row, and the flags of
// fields.
const (
	NoType TypeRef = iota
	HoleRef
	IntRef
	FloatRef
	TextRef
	ByteRef
	BytesRef
	emptyRef
	presentRef // A row has the label.
	absentRef  // A row doesn't have the label, though it lists it.
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
	nodes []node
	// The names of row labels and of rigid variables.
	labels label.Table
	level  uint32
	// While unifying, the nodes overwritten so far and their old contents,
	// so that a failed unification can be undone.
	unifying bool
	trail    []undo

	// The sets of labels that row variables lack, which their nodes index,
	// as spans of lackLabels. Index 0 is the empty set. Sets are never
	// changed, only added.
	lackSets   []span
	lackLabels []label.ID

	// The names of the types that aliases denote, which they print by.
	names map[TypeRef]typeName
	// The enum nodes that a traversal is inside of. Every cycle of a
	// recursive type passes through an enum, so a traversal that meets one
	// it is already inside of has come full circle.
	path []TypeRef
}

type span struct{ start, end uint32 }

// A typeName is the alias that a type was expanded from, and the arguments
// it was applied to.
type typeName struct {
	def  *typeDef
	args []TypeRef
}

// A typeDef is an alias, as types print it: its name, the generalized
// variables of its parameters, and its type for them. An imported one is a
// type scrap, which prints as the reference to it and has no definition.
type typeDef struct {
	name     string
	params   []TypeRef
	body     TypeRef
	imported bool
}

// name names the type ref.
func (r *Registry) name(ref TypeRef, name typeName) {
	if r.names == nil {
		r.names = make(map[TypeRef]typeName)
	}
	r.names[ref] = name
}

// next returns the ref that the next node added will have.
func (r *Registry) next() TypeRef {
	return max(TypeRef(len(r.nodes)), numFixed)
}

// reference names ref, the type of the type scrap that ref refers to,
// applied to args, as that reference. A type the scrap names itself is
// copied, so that it keeps its own name.
func (r *Registry) reference(ref string, typ TypeRef, args []TypeRef) TypeRef {
	typ = r.Resolve(typ)
	switch r.at(typ).kind {
	case listKind, funcKind, recordKind, enumKind:
	default:
		return typ
	}
	if _, named := r.names[typ]; named {
		typ = r.add(r.at(typ))
	}
	r.name(typ, typeName{&typeDef{name: ref, imported: true}, args})
	return typ
}

// enter pushes the enum ref onto the path, returning false if it is already
// on it.
func (r *Registry) enterEnum(ref TypeRef) bool {
	if slices.Contains(r.path, ref) {
		return false
	}
	r.path = append(r.path, ref)
	return true
}

func (r *Registry) exitEnum() {
	r.path = r.path[:len(r.path)-1]
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

// Labels returns the table that the registry interns labels in, which
// values of its types share.
func (r *Registry) Labels() *label.Table {
	return &r.labels
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

// row returns the row tail extended with present labels.
func (r *Registry) row(labels map[string]TypeRef, tail TypeRef) TypeRef {
	for _, l := range slices.Sorted(maps.Keys(labels)) {
		tail = r.extend(r.labels.Of(l), labels[l], tail)
	}
	return tail
}

// extend returns the row rest extended with the label l present, with the
// payload typ.
func (r *Registry) extend(l label.ID, typ, rest TypeRef) TypeRef {
	return r.extendField(l, r.field(typ, presentRef), rest)
}

// extendField returns the row rest extended with the label l and its field,
// which the variable that ends rest then lacks: a row has each label at most
// once.
func (r *Registry) extendField(l label.ID, field, rest TypeRef) TypeRef {
	r.lack(r.rowTail(rest), l)
	return r.add(node{kind: extendKind, aux: uint32(l), a: field, b: rest})
}

// field returns a field with a payload and a flag.
func (r *Registry) field(payload, flag TypeRef) TypeRef {
	return r.add(node{kind: fieldKind, a: payload, b: flag})
}

// lacks returns the labels that the row variable v lacks.
func (r *Registry) lacks(v TypeRef) []label.ID {
	n := r.at(v)
	i := n.a
	switch n.kind {
	case varKind, genKind:
	case rigidKind:
		i = n.b
	default:
		return nil
	}
	if i == 0 {
		return nil
	}
	sp := r.lackSets[i]
	return r.lackLabels[sp.start:sp.end:sp.end]
}

// lack adds labels to those that the row variable v lacks. Other types are
// left as they are.
func (r *Registry) lack(v TypeRef, labels ...label.ID) {
	n := r.at(v)
	if len(labels) == 0 || n.kind != varKind && n.kind != genKind && n.kind != rigidKind {
		return
	}
	// The new set is what v lacks so far, and the labels it doesn't.
	start := uint32(len(r.lackLabels))
	r.lackLabels = append(r.lackLabels, r.lacks(v)...)
	have := len(r.lackLabels)
	for _, l := range labels {
		if !slices.Contains(r.lackLabels[start:], l) {
			r.lackLabels = append(r.lackLabels, l)
		}
	}
	if len(r.lackLabels) == have {
		r.lackLabels = r.lackLabels[:start]
		return
	}
	if len(r.lackSets) == 0 {
		r.lackSets = append(r.lackSets, span{})
	}
	i := TypeRef(len(r.lackSets))
	r.lackSets = append(r.lackSets, span{start, uint32(len(r.lackLabels))})
	if n.kind == rigidKind {
		n.b = i
	} else {
		n.a = i
	}
	r.set(v, n)
}

// A duplicate is the error of a row that would have its label twice.
type duplicate struct{ l label.ID }

func (duplicate) Error() string { return "duplicate label" }

// inherit makes the row t lack labels, as the variable bound to it does.
func (r *Registry) inherit(labels []label.ID, t TypeRef) error {
	for {
		t = r.Resolve(t)
		n := r.at(t)
		switch n.kind {
		case extendKind:
			if slices.Contains(labels, n.label()) {
				return duplicate{n.label()}
			}
			t = n.b
			continue
		case rigidKind:
			// A rigid row may have any label that it doesn't lack.
			for _, l := range labels {
				if !slices.Contains(r.lacks(t), l) {
					return duplicate{l}
				}
			}
		case varKind, genKind:
			r.lack(t, labels...)
		}
		return nil
	}
}

// Var returns a new unbound type variable.
func (r *Registry) Var() TypeRef {
	return r.add(node{kind: varKind, aux: r.level})
}

// rigid returns a new rigid type variable named name, which stands for a type
// that an annotation leaves to its user, so it is equal only to itself.
func (r *Registry) rigid(name string) TypeRef {
	return r.add(node{kind: rigidKind, aux: r.level, a: TypeRef(r.labels.Of(name))})
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
	case enumKind:
		if r.enterEnum(ref) {
			r.generalize(n.a)
			r.exitEnum()
		}
	case listKind, recordKind:
		r.generalize(n.a)
	case funcKind, extendKind, fieldKind:
		r.generalize(n.a)
		r.generalize(n.b)
	}
}

// Instantiate returns a copy of a type scheme with fresh variables in place
// of its generalized ones. Parts without generalized variables are shared.
func (r *Registry) Instantiate(ref TypeRef) TypeRef {
	return r.instantiate(ref, nil)
}

// instantiate copies ref, replacing generalized variables by fresh ones,
// or by what fresh maps them to.
func (r *Registry) instantiate(ref TypeRef, fresh map[TypeRef]TypeRef) TypeRef {
	in := instantiator{r: r, fresh: fresh}
	return in.copy(ref)
}

type instantiator struct {
	r     *Registry
	fresh map[TypeRef]TypeRef
	// The copies of the enums copied so far, or NoType for those being
	// copied, which recursive types refer back to.
	enums map[TypeRef]TypeRef
	// For the enums being copied that something refers back to, the node
	// that their copy will be.
	backs map[TypeRef]TypeRef
}

func (in *instantiator) copy(ref TypeRef) TypeRef {
	r := in.r
	ref = r.Resolve(ref)
	n := r.at(ref)
	switch n.kind {
	case genKind:
		if in.fresh == nil {
			in.fresh = make(map[TypeRef]TypeRef)
		}
		v, ok := in.fresh[ref]
		if !ok {
			v = r.Var()
			in.fresh[ref] = v
		}
		r.lack(r.Resolve(v), r.lacks(ref)...)
		return v
	case enumKind:
		if in.enums == nil {
			in.enums, in.backs = make(map[TypeRef]TypeRef), make(map[TypeRef]TypeRef)
		}
		if c, ok := in.enums[ref]; ok {
			if c != NoType {
				return c
			}
			// A recursive type: refer to the copy being made.
			back, ok := in.backs[ref]
			if !ok {
				back = r.add(node{})
				in.backs[ref] = back
			}
			return back
		}
		in.enums[ref] = NoType
		a := in.copy(n.a)
		c := ref
		if back, ok := in.backs[ref]; ok {
			n.a = a
			r.set(back, n)
			c = back
		} else if a != n.a {
			n.a = a
			c = r.add(n)
		}
		if c != ref {
			in.name(ref, c)
		}
		in.enums[ref] = c
		return c
	case listKind, recordKind:
		a := in.copy(n.a)
		if a == n.a {
			return ref
		}
		n.a = a
		c := r.add(n)
		in.name(ref, c)
		return c
	case funcKind, extendKind, fieldKind:
		a, b := in.copy(n.a), in.copy(n.b)
		if a == n.a && b == n.b {
			return ref
		}
		switch n.kind {
		case extendKind:
			return r.extendField(n.label(), a, b)
		case fieldKind:
			return r.field(a, b)
		}
		n.a, n.b = a, b
		c := r.add(n)
		in.name(ref, c)
		return c
	}
	return ref
}

// name gives the copy c of ref ref's name, if it has one, applied to copies
// of its arguments.
func (in *instantiator) name(ref, c TypeRef) {
	name, ok := in.r.names[ref]
	if !ok {
		return
	}
	args := make([]TypeRef, len(name.args))
	for i, arg := range name.args {
		args[i] = in.copy(arg)
	}
	in.r.name(c, typeName{name.def, args})
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
	if d, ok := err.(duplicate); ok {
		name := r.labels.Name(d.l)
		if r.at(r.Resolve(a)).kind == enumKind {
			name = "#" + name
		}
		return r.errorf("cannot unify '%s' with '%s', which would have "+name+" twice", a, b)
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

	// Link a named one to the other, so that it prints by its name.
	link := func() {
		if _, named := r.names[a]; named {
			r.link(b, a)
		} else {
			r.link(a, b)
		}
	}

	switch na.kind {
	case enumKind:
		// Every cycle passes through an enum, so link enums first, so that
		// unifying recursive types ends when it comes back around.
		link()
		return r.rowError(a, b, r.unifyRows(na.a, nb.a))
	case listKind, funcKind, recordKind:
		// Others are linked after their parts, so that an occurs check on
		// their parts doesn't see one through the other.
		var err error
		switch na.kind {
		case listKind:
			err = r.unify(na.a, nb.a)
		case funcKind:
			if err = r.unify(na.a, nb.a); err == nil {
				err = r.unify(na.b, nb.b)
			}
		case recordKind:
			err = r.rowError(a, b, r.unifyRows(na.a, nb.a))
		}
		if err == nil {
			link()
		}
		return err
	case extendKind:
		return r.unifyRows(a, b)
	case fieldKind:
		if err := r.unifyFlags(na.b, nb.b); err != nil {
			return err
		}
		return r.unify(na.a, nb.a)
	}
	return r.mismatch(a, b)
}

// unifyFlags unifies the flags of two fields. A label present in one row
// and absent from the other is missing from it.
func (r *Registry) unifyFlags(a, b TypeRef) error {
	a, b = r.Resolve(a), r.Resolve(b)
	if a != b && r.at(a).kind == conKind && r.at(b).kind == conKind {
		return errMissingLabel
	}
	return r.unify(a, b)
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
	if labels := r.lacks(v); labels != nil {
		if err := r.inherit(labels, t); err != nil {
			return err
		}
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
	case enumKind:
		if !r.enterEnum(t) {
			return nil
		}
		defer r.exitEnum()
		return r.occurs(v, level, n.a)
	case listKind, recordKind:
		return r.occurs(v, level, n.a)
	case funcKind, extendKind, fieldKind:
		if err := r.occurs(v, level, n.a); err != nil {
			return err
		}
		return r.occurs(v, level, n.b)
	}
	return nil
}

// guarded reports whether every path from t to the variable v passes
// through a tag's payload. It stops at enums, so it terminates on recursive
// types, whose cycles pass through enums.
func (r *Registry) guarded(t, v TypeRef) bool {
	t = r.Resolve(t)
	n := r.at(t)
	switch n.kind {
	case varKind:
		return t != v
	case listKind, recordKind:
		return r.guarded(n.a, v)
	case funcKind, extendKind, fieldKind:
		return r.guarded(n.a, v) && r.guarded(n.b, v)
	}
	return true
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
	field, rest, err := r.rewrite(b, na.label())
	if err != nil {
		return err
	}
	if openTail && r.at(tail).kind != varKind {
		return errInfiniteRow
	}
	if err := r.unify(na.a, field); err != nil {
		return err
	}
	return r.unifyRows(na.b, rest)
}

// rewrite finds the label l in row, returning its field and the rest of the
// row. If the row is open and lacks l, its tail is extended with it. If the
// row is closed without l, l is absent from it.
func (r *Registry) rewrite(row TypeRef, l label.ID) (field, rest TypeRef, err error) {
	row = r.Resolve(row)
	n := r.at(row)
	switch n.kind {
	case extendKind:
		if n.label() == l {
			return n.a, n.b, nil
		}
		field, rest, err = r.rewrite(n.b, l)
		if err != nil {
			return
		}
		return field, r.extendField(n.label(), n.a, rest), nil
	case varKind:
		lacks := r.lacks(row)
		if slices.Contains(lacks, l) {
			return 0, 0, duplicate{l}
		}
		// The new variables live as long as the row's, and the rest lacks
		// what it does.
		level := r.level
		r.level = n.aux
		field, rest = r.field(r.Var(), r.Var()), r.Var()
		r.level = level
		r.lack(rest, lacks...)
		r.link(row, r.extendField(l, field, rest))
		return field, rest, nil
	case emptyKind:
		return r.field(r.Var(), absentRef), row, nil
	}
	return 0, 0, errMissingLabel
}

// isVar returns true if the resolved ref is a type variable.
func (r *Registry) isVar(ref TypeRef) bool {
	k := r.at(ref).kind
	return k == varKind || k == genKind
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

// OpenRow returns a record or enum in ref whose row ends in a variable, and
// true, or false if ref has none. The payloads of absent labels don't count.
func (r *Registry) OpenRow(ref TypeRef) (TypeRef, bool) {
	ref = r.Resolve(ref)
	n := r.at(ref)
	switch n.kind {
	case listKind:
		return r.OpenRow(n.a)
	case funcKind:
		if open, ok := r.OpenRow(n.a); ok {
			return open, true
		}
		return r.OpenRow(n.b)
	case recordKind, enumKind:
		if r.isVar(r.rowTail(n.a)) {
			return ref, true
		}
		if n.kind == enumKind {
			if !r.enterEnum(ref) {
				return NoType, false
			}
			defer r.exitEnum()
		}
		for row := r.Resolve(n.a); r.at(row).kind == extendKind; row = r.Resolve(r.at(row).b) {
			f := r.at(r.Resolve(r.at(row).a))
			if r.Resolve(f.b) == absentRef {
				continue
			}
			if open, ok := r.OpenRow(f.a); ok {
				return open, true
			}
		}
	}
	return NoType, false
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
	// The types are parts of others, with no positions to go by, so they
	// show every tag that may be present.
	s := stringer{reg: r, mayBe: true}
	strs, where := s.strings(e.a, e.b)
	msg := strings.Replace(e.msg, "%s", strs[0], 1)
	msg = strings.Replace(msg, "%s", strs[1], 1)
	return errors.New(msg + where)
}

// Describe returns a string representation of a scrap's result: its type,
// or for a type scrap, its kind and the type it denotes, as `type = T` or
// `type -> type = a => T`.
func (r *Registry) Describe(res Result) string {
	if !res.Sort {
		return r.String(res.Type)
	}
	// The type prints as the alias it names, unfolded, unless it recurs, so
	// that it must be named anyway.
	s := stringer{reg: r}
	typ := r.Resolve(res.Type)
	s.flags(typ, false)
	s.reserveBody(typ)
	name, named := r.names[typ]
	_, recurs := s.defs[name.def]
	unfold := named && !recurs
	if named && recurs {
		// Its definition comes first.
		i := slices.Index(s.order, name.def)
		s.order = slices.Insert(slices.Delete(s.order, i, i+1), 0, name.def)
	}
	var b strings.Builder
	for _, p := range res.Params {
		if p.Row {
			b.WriteString("row -> ")
		} else {
			b.WriteString("type -> ")
		}
	}
	b.WriteString("type = ")
	for _, p := range res.Params {
		b.WriteString(s.string(p.Var))
		b.WriteString(" => ")
	}
	s.Reset()
	if unfold {
		s.writeBody(typ, topLevel)
	} else {
		s.write(typ, topLevel)
	}
	b.WriteString(s.String())
	b.WriteString(s.where())
	return b.String()
}

// apply returns body with each of vars replaced by the matching arg.
func (r *Registry) apply(vars []TypeRef, body TypeRef, args []TypeRef) TypeRef {
	fresh := make(map[TypeRef]TypeRef, len(vars))
	for i, v := range vars {
		fresh[r.Resolve(v)] = args[i]
	}
	return r.instantiate(body, fresh)
}

// String returns a string representation of a type. The types of aliases
// print by name, and their definitions follow in a where-clause.
func (r *Registry) String(ref TypeRef) string {
	strs, where := r.Strings(ref)
	return strs[0] + where
}

// Strings returns string representations of types that are shown together,
// which share the names of their variables and aliases, and the
// where-clause that defines those aliases once for all of them.
//
// A tag that may or may not be present shows as the type that accepts the
// most and returns the least: present if it is in an input position, a
// function's argument, and absent otherwise.
func (r *Registry) Strings(refs ...TypeRef) (strs []string, where string) {
	s := stringer{reg: r}
	for _, ref := range refs {
		s.flags(ref, false)
	}
	return s.strings(refs...)
}

func (s *stringer) strings(refs ...TypeRef) (strs []string, where string) {
	for _, ref := range refs {
		s.reserve(ref)
	}
	strs = make([]string, len(refs))
	for i, ref := range refs {
		strs[i] = s.string(ref)
	}
	return strs, s.where()
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
	// The enums being written, which recursive types refer back to.
	path []TypeRef
	// The printed names of the aliases met, in the order met.
	defs  map[*typeDef]string
	order []*typeDef
	// The flag variables in input positions, whose labels show as present.
	inputs map[TypeRef]bool
	// Whether every label that may be present shows, as in errors.
	mayBe bool
	// The enums that flags has visited, by the positions it visited them in.
	visited map[TypeRef]uint8
}

// flags finds the flag variables of ref that are in input positions, if
// input is false, or that are in output positions otherwise.
func (s *stringer) flags(ref TypeRef, input bool) {
	ref = s.reg.Resolve(ref)
	n := s.reg.at(ref)
	switch n.kind {
	case enumKind:
		bit := uint8(1)
		if input {
			bit = 2
		}
		if s.visited[ref]&bit != 0 {
			return
		}
		if s.visited == nil {
			s.visited = make(map[TypeRef]uint8)
		}
		s.visited[ref] |= bit
		s.flags(n.a, input)
	case listKind, recordKind:
		s.flags(n.a, input)
	case funcKind:
		s.flags(n.a, !input)
		s.flags(n.b, input)
	case extendKind:
		s.flags(n.a, input)
		s.flags(n.b, input)
	case fieldKind:
		if flag := s.reg.Resolve(n.b); input && s.reg.isVar(flag) {
			if s.inputs == nil {
				s.inputs = make(map[TypeRef]bool)
			}
			s.inputs[flag] = true
		}
		s.flags(n.a, input)
	}
}

// entries returns the labels of row that show, and its tail.
func (s *stringer) entries(row TypeRef) ([]rowEntry, TypeRef) {
	entries, tail := s.reg.entries(row)
	if !s.mayBe {
		entries = slices.DeleteFunc(entries, func(e rowEntry) bool {
			return s.reg.isVar(e.flag) && !s.inputs[e.flag]
		})
	}
	return entries, tail
}

// where returns the where-clause that defines the aliases written.
func (s *stringer) where() string {
	s.Reset()
	for i := 0; i < len(s.order); i++ {
		d := s.order[i]
		s.WriteString("\n; ")
		s.WriteString(s.defs[d])
		// As a sort binding: one without parameters needs its kind.
		if len(d.params) == 0 {
			s.WriteString(" : type")
		}
		s.WriteString(" = ")
		// The parameters are local to the binding.
		names, next := s.names, s.next
		s.names, s.next = nil, 0
		for _, p := range d.params {
			s.variable(s.reg.Resolve(p))
			s.WriteString(" => ")
		}
		s.writeBody(s.reg.Resolve(d.body), topLevel)
		s.names, s.next = names, next
	}
	return s.String()
}

// define gives the alias d a name, unless it has one, and reserves the names
// in its definition. An imported one is not defined: it is its name.
func (s *stringer) define(d *typeDef) {
	if _, ok := s.defs[d]; ok || d.imported {
		return
	}
	if s.defs == nil {
		s.defs = make(map[*typeDef]string)
		if s.taken == nil {
			s.taken = make(map[string]bool)
		}
	}
	name := d.name
	for s.taken[name] {
		name += "'"
	}
	s.taken[name] = true
	s.defs[d] = name
	s.order = append(s.order, d)
	s.reserveBody(s.reg.Resolve(d.body))
}

func (s *stringer) string(ref TypeRef) string {
	s.Reset()
	s.write(ref, topLevel)
	return s.String()
}

// reserve reserves the names of the rigid variables and aliases in ref.
func (s *stringer) reserve(ref TypeRef) {
	ref = s.reg.Resolve(ref)
	if name, ok := s.reg.names[ref]; ok {
		s.define(name.def)
		for _, arg := range name.args {
			s.reserve(arg)
		}
		return
	}
	s.reserveBody(ref)
}

// reserveBody reserves the names in the resolved ref, not by its name.
func (s *stringer) reserveBody(ref TypeRef) {
	n := s.reg.at(ref)
	switch n.kind {
	case rigidKind:
		if s.taken == nil {
			s.taken = make(map[string]bool)
		}
		s.taken[s.reg.labels.Name(n.name())] = true
	case enumKind:
		if slices.Contains(s.path, ref) {
			return
		}
		s.path = append(s.path, ref)
		s.reserve(n.a)
		s.path = s.path[:len(s.path)-1]
	case listKind, recordKind:
		s.reserve(n.a)
	case funcKind:
		s.reserve(n.a)
		s.reserve(n.b)
	case extendKind:
		// In the order that they are written, which recursive types are
		// named in.
		entries, tail := s.entries(ref)
		for _, e := range entries {
			s.reserve(e.payload)
		}
		s.reserve(tail)
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
	if name, ok := s.reg.names[ref]; ok {
		s.named(name, nesting)
		return
	}
	s.writeBody(ref, nesting)
}

// writeBody writes the resolved ref, not by its name.
func (s *stringer) writeBody(ref TypeRef, nesting int) {
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
		s.WriteString(s.reg.labels.Name(n.name()))
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
		if slices.Contains(s.path, ref) {
			// Unification merges recursive types into named ones, so
			// this shouldn't happen.
			s.WriteString("...")
			return
		}
		s.path = append(s.path, ref)
		s.enum(n)
		s.path = s.path[:len(s.path)-1]
	default:
		panic("bad type-ref")
	}
}

// enum writes the tags of an enum.
func (s *stringer) enum(n node) {
	s.row(n.a, '[', ']', func(label string, typ TypeRef) {
		s.WriteByte('#')
		s.WriteString(label)
		if s.reg.Resolve(typ) != HoleRef {
			s.WriteByte(' ')
			s.write(typ, atomLevel)
		}
	})
}

// named writes a type as its alias's name, applied to its arguments.
func (s *stringer) named(name typeName, nesting int) {
	s.define(name.def)
	paren := len(name.args) > 0 && nesting >= atomLevel
	if paren {
		s.WriteByte('(')
	}
	if name.def.imported {
		s.WriteString(name.def.name)
	} else {
		s.WriteString(s.defs[name.def])
	}
	for _, arg := range name.args {
		s.WriteByte(' ')
		s.write(arg, atomLevel)
	}
	if paren {
		s.WriteByte(')')
	}
}

// A rowEntry is a label of a row, its payload and its flag.
type rowEntry struct {
	label   string
	payload TypeRef
	flag    TypeRef
}

// entries returns the labels in order that a row may have, and its tail.
// Absent labels are left out.
func (r *Registry) entries(row TypeRef) ([]rowEntry, TypeRef) {
	var entries []rowEntry
	for {
		l, payload, flag, rest, ok := r.entry(row)
		if !ok {
			row = rest
			break
		}
		entries = append(entries, rowEntry{r.labels.Name(l), payload, flag})
		row = rest
	}
	slices.SortFunc(entries, func(a, b rowEntry) int { return strings.Compare(a.label, b.label) })
	return entries, row
}

// entry returns the first label of row that it may have, its payload and
// flag, and the rest of the row after it. If there is none, it returns the
// tail of the row as the rest, and false.
func (r *Registry) entry(row TypeRef) (l label.ID, payload, flag, rest TypeRef, ok bool) {
	for {
		row = r.Resolve(row)
		n := r.at(row)
		if n.kind != extendKind {
			return 0, 0, 0, row, false
		}
		f := r.at(r.Resolve(n.a))
		if flag := r.Resolve(f.b); flag != absentRef {
			return n.label(), f.a, flag, n.b, true
		}
		row = n.b
	}
}

// fields yields the labels that row may have, in row order, and their
// payloads.
func (r *Registry) fields(row TypeRef) iter.Seq2[label.ID, TypeRef] {
	return func(yield func(label.ID, TypeRef) bool) {
		for {
			l, payload, _, rest, ok := r.entry(row)
			if !ok || !yield(l, payload) {
				return
			}
			row = rest
		}
	}
}

// row writes a row's labels in order, followed by its tail if it is open.
func (s *stringer) row(row TypeRef, open, close byte, label func(string, TypeRef)) {
	entries, row := s.entries(row)
	s.WriteByte(open)
	for i, e := range entries {
		if i > 0 {
			s.WriteByte(',')
		}
		s.WriteByte(' ')
		label(e.label, e.payload)
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
