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
