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

	maybe := func(ref TypeRef) TypeRef {
		return reg.Enum(MapRef{"some": ref, "none": NeverRef})
	}

	ref = maybe(IntRef)

	Eq(t, reg.String(ref), "#none #some int")

	Eq(t, reg.String(maybe(ref)), "#none #some (#none #some int)")

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

func TestInstantiate(t *testing.T) {
	reg := Registry{}

	// Only unbound types _introduced_ by arrows should be replaced.
	a := reg.Unbound()
	b := reg.Unbound()
	f := reg.Func(a, b)
	Eq(t, reg.String(f), "a -> b")

	l := reg.List(a)
	Eq(t, reg.String(l), "list a")

	g := reg.Instantiate(f)
	h := reg.Instantiate(f)
	Eq(t, reg.String(g), "$0 -> a")
	Eq(t, reg.String(h), "$1 -> a")

	Eq(t, reg.String(reg.Instantiate(l)), "list a")
	Eq(t, reg.String(reg.Instantiate(reg.Func(a, l))), "$2 -> list $2")
}

func TestGeneralize(t *testing.T) {
	reg := Registry{}

	// Only type variables _introduced_ by arrows should be replaced.
	a := reg.Var()
	b := reg.Var()
	f := reg.Func(a, b)
	Eq(t, reg.String(f), "$0 -> $1")

	l := reg.List(a)
	Eq(t, reg.String(l), "list $0")

	g := reg.generalize(f)
	h := reg.generalize(f)
	Eq(t, reg.String(g), "a -> $1")
	Eq(t, reg.String(h), "a -> $1")

	Eq(t, reg.String(reg.generalize(l)), "list $0")
	Eq(t, reg.String(reg.generalize(reg.Func(a, l))), "a -> list a")

}

func TestGetVar(t *testing.T) {
	reg := Registry{}

	a := reg.Var()
	b := reg.Var()
	c := reg.Var()
	reg.bind(a, b)
	reg.bind(b, c)
	reg.bind(c, IntRef)

	Eq(t, reg.GetVar(a), IntRef)
}

func TestResolve(t *testing.T) {
	reg := Registry{}

	a := reg.Var()
	b := reg.Var()

	Eq(t, reg.Resolve(a), a)
	Eq(t, reg.Resolve(b), b)
	Eq(t, reg.IsFree(a), true)
	Eq(t, reg.IsFree(b), true)

	reg.bind(a, b)

	Eq(t, reg.IsFree(a), true)
	Eq(t, reg.IsFree(b), true)
	Eq(t, reg.Resolve(a), b)
	Eq(t, reg.Resolve(b), b)

	reg.bind(b, IntRef)

	Eq(t, reg.IsFree(a), false)
	Eq(t, reg.IsFree(b), false)
	Eq(t, reg.Resolve(a), IntRef)
	Eq(t, reg.Resolve(b), IntRef)
}

func TestUnify_J(t *testing.T) {
	reg := Registry{}

	res := reg.Var()
	a := reg.Var()
	fn := reg.Func(a, reg.List(a))
	reg.unify(fn, reg.Func(IntRef, res))

	Eq(t, reg.String(res), "list int")
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
