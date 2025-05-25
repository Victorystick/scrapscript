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

	a := reg.Generic(0)
	b := reg.Generic(1)

	listFold := reg.Func(b, reg.Func(reg.Func(b, reg.Func(a, b)), reg.Func(reg.List(a), b)))
	Eq(t, reg.String(listFold), "b -> (b -> a -> b) -> list a -> b")

	listMap := reg.Func(reg.Func(a, b), reg.Func(reg.List(a), reg.List(b)))
	Eq(t, reg.String(listMap), "(a -> b) -> list a -> list b")
}

func TestResolveGeneric(t *testing.T) {
	reg := Registry{}

	a := reg.Generic(0)
	Eq(t, reg.String(a), "a")
	Eq(t, reg.ResolveGeneric(a, a, IntRef), IntRef)

	id := reg.Func(a, a)
	Eq(t, reg.String(id), "a -> a")
	Eq(t, reg.Size(), 1)

	inc := reg.ResolveGeneric(id, a, IntRef)
	Eq(t, reg.String(inc), "int -> int")
	Eq(t, reg.Size(), 2)

	b := reg.Generic(1)
	listMap := reg.Func(reg.Func(a, b), reg.Func(reg.List(a), reg.List(b)))
	Eq(t, reg.String(listMap), "(a -> b) -> list a -> list b")
	Eq(t, reg.Size(), 7)

	// Replace b -> int.
	listMapBInt := reg.ResolveGeneric(listMap, b, IntRef)
	Eq(t, reg.String(listMapBInt), "(a -> int) -> list a -> list int")
	Eq(t, reg.Size(), 11)

	// Now also a -> int.
	listMapABInt := reg.ResolveGeneric(listMapBInt, a, IntRef)
	Eq(t, reg.String(listMapABInt), "(int -> int) -> list int -> list int")
	Eq(t, reg.Size(), 13)

	// Let's go the other way, replace a -> int.
	listMapAInt := reg.ResolveGeneric(listMap, a, IntRef)
	Eq(t, reg.String(listMapAInt), "(int -> b) -> list int -> list b")
	Eq(t, reg.Size(), 16)

	// Returns same type if resolved the other way.
	Eq(t, listMapABInt, reg.ResolveGeneric(listMapAInt, b, IntRef))
	Eq(t, reg.Size(), 16)

	record := reg.Record(MapRef{"kind": IntRef, "a": a, "b": b})
	enum := reg.Enum(MapRef{"a": a, "b": b})
	recordToEnum := reg.Func(record, enum)

	// TODO: Don't sort order of record keys.
	Eq(t, reg.String(recordToEnum), "{ a : a, b : b, kind : int } -> #a a #b b")

	recordToEnumAInt := reg.ResolveGeneric(recordToEnum, a, IntRef)
	Eq(t, reg.String(recordToEnumAInt), "{ a : int, b : b, kind : int } -> #a int #b b")

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
