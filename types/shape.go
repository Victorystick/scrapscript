package types

// A Shape is what a type is, for code outside this package that walks
// types, such as generators of bindings. Its Kind says which fields it
// uses. The refs it holds are those of the type's parts, unresolved.
type Shape struct {
	Kind Kind
	// The type, resolved, which identifies a variable.
	Ref TypeRef

	Name   string  // ShapePrimitive: its name, such as "int" or "()".
	Rigid  bool    // ShapeVariable: whether it is rigid.
	Elem   TypeRef // ShapeList: the element type.
	Arg    TypeRef // ShapeFunc: the argument type.
	Result TypeRef // ShapeFunc: the result type.
	// ShapeRecord and ShapeEnum: the fields or tags, sorted by name. A tag
	// without a payload has the payload HoleRef.
	Labels []Label
	// ShapeRecord and ShapeEnum: whether the row ends in a variable, so
	// that it may have other labels.
	Open bool

	// The alias that the type was expanded from, if any, and the arguments
	// it was applied to. An imported alias is named by its reference.
	Alias    string
	Imported bool
	Args     []TypeRef
}

// A Kind is the sort of type that a Shape is.
type Kind uint8

const (
	ShapePrimitive Kind = iota
	ShapeVariable
	ShapeList
	ShapeFunc
	ShapeRecord
	ShapeEnum
)

// A Label is a record field or enum tag, and its type.
type Label struct {
	Name string
	Type TypeRef
}

// Shape returns the shape of the type ref.
func (r *Registry) Shape(ref TypeRef) Shape {
	ref = r.Resolve(ref)
	s := Shape{Ref: ref}
	if name, ok := r.names[ref]; ok {
		s.Alias, s.Imported, s.Args = name.def.name, name.def.imported, name.args
	}
	n := r.at(ref)
	switch n.kind {
	case conKind:
		s.Kind, s.Name = ShapePrimitive, primitiveNames[ref]
	case varKind, genKind:
		s.Kind = ShapeVariable
	case rigidKind:
		s.Kind, s.Rigid = ShapeVariable, true
	case boundKind, forallKind, lamKind, appKind:
		// Types that only annotations give, which no value's shape follows.
		s.Kind = ShapeVariable
	case listKind:
		s.Kind, s.Elem = ShapeList, n.a
	case funcKind:
		s.Kind, s.Arg, s.Result = ShapeFunc, n.a, n.b
	case recordKind, enumKind:
		s.Kind = ShapeRecord
		if n.kind == enumKind {
			s.Kind = ShapeEnum
		}
		entries, tail := r.entries(n.a)
		for _, e := range entries {
			s.Labels = append(s.Labels, Label{e.label, e.payload})
		}
		s.Open = r.at(tail).kind != emptyKind
	default:
		panic("bad type-ref")
	}
	return s
}
