package types

import "testing"

func TestTypeRefDefaultsToNever(t *testing.T) {
	reg := Registry{}
	var ref TypeRef
	Eq(t, reg.String(ref), "never")
}

func TestPrimitives(t *testing.T) {
	reg := Registry{}

	for i, ref := range primitives {
		Eq(t, primitiveNames[i], reg.String(ref))
	}
}

func TestLists(t *testing.T) {
	reg := Registry{}

	ints := reg.List(IntRef)
	bytes := reg.List(ByteRef)

	Eq(t, ints, TypeRef(listTag))

	Eq(t, ints, reg.List(IntRef))
	Neq(t, ints, bytes)

	Eq(t, reg.String(ints), "list int")
	Eq(t, reg.String(bytes), "list byte")

	Eq(t, reg.String(reg.List(reg.Func(IntRef, IntRef))), "list (int -> int)")
	Eq(t, reg.String(reg.List(reg.List(IntRef))), "list (list int)")
}

func TestFunc(t *testing.T) {
	reg := Registry{}

	inc := reg.Func(IntRef, IntRef)
	add := reg.Func(IntRef, inc)

	Neq(t, inc, add)

	Eq(t, reg.String(inc), "int -> int")
	Eq(t, reg.String(add), "int -> int -> int")

	reduce := reg.Func(IntRef, reg.Func(add, reg.Func(reg.List(IntRef), IntRef)))
	Eq(t, reg.String(reduce), "int -> (int -> int -> int) -> list int -> int")

	split := reg.Func(TextRef, reg.Func(TextRef, reg.List(TextRef)))
	Eq(t, reg.String(split), "text -> text -> list text")
}

func TestEnum(t *testing.T) {
	reg := Registry{}

	enum := MapRef{"l": IntRef, "r": IntRef}
	ref := reg.Enum(enum)

	Eq(t, reg.String(ref), "#l int #r int")

	maybe := MapRef{"some": IntRef, "none": NeverRef}
	ref = reg.Enum(maybe)

	Eq(t, reg.String(ref), "#none #some int")

	inc := reg.Func(IntRef, IntRef)
	typ := MapRef{"fun": inc}
	Eq(t, reg.String(reg.Enum(typ)), "#fun (int -> int)")
}

func TestRecord(t *testing.T) {
	reg := Registry{}

	record := MapRef{"x": IntRef, "y": IntRef}
	ref := reg.Record(record)
	Eq(t, reg.String(ref), "{ x : int, y : int }")

	inc := reg.Func(IntRef, IntRef)
	typ := MapRef{"inc": inc}
	Eq(t, reg.String(reg.Record(typ)), "{ inc : (int -> int) }")
}

func TestGeneric(t *testing.T) {
	reg := Registry{}

	a := reg.Unbound()
	b := reg.Unbound()

	listFold := reg.Func(b, reg.Func(reg.Func(b, reg.Func(a, b)), reg.Func(reg.List(a), b)))
	Eq(t, reg.String(listFold), "a -> (a -> b -> a) -> list b -> a")

	listMap := reg.Func(reg.Func(a, b), reg.Func(reg.List(a), reg.List(b)))
	Eq(t, reg.String(listMap), "(a -> b) -> list a -> list b")
}

func TestBind(t *testing.T) {
	reg := Registry{}

	a := reg.Unbound()
	Eq(t, reg.String(a), "a")
	Eq(t, reg.Bind(a, a, IntRef), IntRef)

	id := reg.Func(a, a)
	Eq(t, reg.String(id), "a -> a")
	Eq(t, reg.Size(), 1)

	inc := reg.Bind(id, a, IntRef)
	Eq(t, reg.String(inc), "int -> int")
	Eq(t, reg.Size(), 2)

	b := reg.Unbound()
	listMap := reg.Func(reg.Func(a, b), reg.Func(reg.List(a), reg.List(b)))
	Eq(t, reg.String(listMap), "(a -> b) -> list a -> list b")
	Eq(t, reg.Size(), 7)

	// Replace b -> int.
	listMapBInt := reg.Bind(listMap, b, IntRef)
	Eq(t, reg.String(listMapBInt), "(a -> int) -> list a -> list int")
	Eq(t, reg.Size(), 11)

	// Now also a -> int.
	listMapABInt := reg.Bind(listMapBInt, a, IntRef)
	Eq(t, reg.String(listMapABInt), "(int -> int) -> list int -> list int")
	Eq(t, reg.Size(), 13)

	// Let's go the other way, replace a -> int.
	listMapAInt := reg.Bind(listMap, a, IntRef)
	Eq(t, reg.String(listMapAInt), "(int -> a) -> list int -> list a")
	Eq(t, reg.Size(), 16)

	// Returns same type if resolved the other way.
	Eq(t, listMapABInt, reg.Bind(listMapAInt, b, IntRef))
	Eq(t, reg.Size(), 16)

	record := reg.Record(MapRef{"kind": IntRef, "a": a, "b": b})
	enum := reg.Enum(MapRef{"a": a, "b": b})
	recordToEnum := reg.Func(record, enum)

	// TODO: Don't sort order of record keys.
	Eq(t, reg.String(recordToEnum), "{ a : a, b : b, kind : int } -> #a a #b b")

	recordToEnumAInt := reg.Bind(recordToEnum, a, IntRef)
	Eq(t, reg.String(recordToEnumAInt), "{ a : int, b : a, kind : int } -> #a int #b a")

}

func TestSubstitute(t *testing.T) {
	reg := Registry{}

	x := reg.Var()
	Eq(t, reg.substitute(x, Subst{{x, IntRef}}), IntRef)

	Eq(t, reg.substitute(reg.Func(x, x), Subst{{x, IntRef}}), reg.Func(IntRef, IntRef))

	y := reg.Var()
	Eq(t, reg.substitute(reg.Func(x, y), Subst{{x, IntRef}}), reg.Func(IntRef, y))

	// A Scheme is represented by an unbound variable.
	// These are left untouched. (forall x. x) untouched.
	a := reg.Unbound()
	Eq(t, reg.substitute(a, Subst{{x, IntRef}}), a)

	Eq(t, reg.substitute(reg.Func(x, y), Subst{{a, IntRef}, {y, TextRef}}), reg.Func(x, TextRef))

	// Other...
	b := reg.Unbound()
	f := reg.Func(a, b)
	Eq(t, reg.String(f), "a -> b")

	l := reg.List(a)
	Eq(t, reg.String(l), "list a")

	g := reg.Instantiate(f)
	h := reg.Instantiate(f)
	Eq(t, reg.String(f), "a -> b")
	Eq(t, reg.String(g), "$2 -> $3")
	Eq(t, reg.String(h), "$4 -> $5")

	Eq(t, reg.String(reg.Instantiate(l)), "list $6")

	gFn := reg.GetFunc(g)

	subst := Subst{
		{replace: gFn.Arg, with: IntRef},
	}

	Eq(t, reg.substitute(g, subst), reg.Func(IntRef, gFn.Result))
	Eq(t, reg.substitute(h, subst), h)
}

func TestCompose(t *testing.T) {
	reg := Registry{}

	t1 := reg.Var()
	t2 := reg.Var()

	s1 := Subst{{t2, IntRef}}
	Eq(t, s1.String(&reg), "$1: int")

	s2 := Subst{{t1, reg.Func(IntRef, t2)}}
	Eq(t, s2.String(&reg), "$0: int -> $1")

	Eq(t, reg.Compose(nil, nil).String(&reg), "")
	Eq(t, reg.Compose(s1, nil).String(&reg), "$1: int")
	Eq(t, reg.Compose(nil, s1).String(&reg), "$1: int")

	Eq(t, reg.Compose(s1, s2).String(&reg), "$0: int -> int, $1: int")
}

func TestUnify(t *testing.T) {
	reg := Registry{}

	a := reg.Var()
	Eq(t, reg.Unify(a, IntRef).String(&reg), "$0: int")

	a = reg.Var()
	Eq(t, reg.Unify(reg.Func(a, IntRef), reg.Func(TextRef, IntRef)).String(&reg), "$1: text")

	a = reg.Var()
	b := reg.Var()
	Eq(t, reg.Unify(reg.Func(a, b), reg.Func(b, a)).String(&reg), "$2: $3")

	a = reg.Var()
	b = reg.Var()
	Eq(t, reg.Unify(reg.Func(a, IntRef), reg.Func(HoleRef, b)).String(&reg), "$5: int, $4: ()")
}

func Neq[T comparable](t *testing.T, a, b T) {
	if a == b {
		t.Errorf("Expected %v NOT to be %v", a, b)
	}
}

func Eq[T comparable](t *testing.T, a, b T) {
	if a != b {
		t.Errorf("Expected %v to be %v", a, b)
	}
}
