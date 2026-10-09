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

	ref := reg.Enum(map[string]TypeRef{"l": IntRef, "r": IntRef})
	Eq(t, reg.String(ref), "[ #l int, #r int ]")

	maybe := func(ref TypeRef) TypeRef {
		return reg.Enum(map[string]TypeRef{"some": ref, "none": NeverRef})
	}

	ref = maybe(IntRef)
	Eq(t, reg.String(ref), "[ #none, #some int ]")
	Eq(t, reg.String(maybe(ref)), "[ #none, #some [ #none, #some int ] ]")
	Eq(t, reg.String(maybe(reg.List(IntRef))), "[ #none, #some (list int) ]")

	inc := reg.Func(IntRef, IntRef)
	Eq(t, reg.String(reg.Enum(map[string]TypeRef{"fun": inc})), "[ #fun (int -> int) ]")
}

func TestRecord(t *testing.T) {
	reg := Registry{}

	ref := reg.Record(map[string]TypeRef{"x": IntRef, "y": IntRef})
	Eq(t, reg.String(ref), "{ x : int, y : int }")

	inc := reg.Func(IntRef, IntRef)
	Eq(t, reg.String(reg.Record(map[string]TypeRef{"inc": inc})), "{ inc : int -> int }")
	Eq(t, reg.String(reg.Record(nil)), "{}")
}

func TestGeneric(t *testing.T) {
	reg := Registry{}

	a := reg.Gen()
	b := reg.Gen()

	listFold := reg.Func(b, reg.Func(reg.Func(b, reg.Func(a, b)), reg.Func(reg.List(a), b)))
	Eq(t, reg.String(listFold), "a -> (a -> b -> a) -> list b -> a")

	listMap := reg.Func(reg.Func(a, b), reg.Func(reg.List(a), reg.List(b)))
	Eq(t, reg.String(listMap), "(a -> b) -> list a -> list b")
}

func TestInstantiate(t *testing.T) {
	reg := Registry{}

	a := reg.Gen()
	b := reg.Gen()
	f := reg.Func(a, reg.Func(b, a))

	g := reg.Instantiate(f)
	h := reg.Instantiate(f)
	Neq(t, g, f)
	Neq(t, g, h)

	// Instantiating replaces each generalized variable consistently.
	Eq(t, reg.Unify(g, reg.Func(IntRef, reg.Func(TextRef, IntRef))), nil)
	Eq(t, reg.String(g), "int -> text -> int")
	Eq(t, reg.String(h), "a -> b -> a")
	Eq(t, reg.String(f), "a -> b -> a")

	// Types without generalized variables are shared.
	l := reg.List(IntRef)
	Eq(t, reg.Instantiate(l), l)
}

func TestGeneralize(t *testing.T) {
	reg := Registry{}

	outer := reg.Var()
	reg.enter()
	inner := reg.Var()
	f := reg.Func(inner, outer)
	reg.exit()
	reg.generalize(f)

	// Only the variable local to the binding is generalized.
	g := reg.Instantiate(f)
	Eq(t, reg.Unify(g, reg.Func(IntRef, TextRef)), nil)
	Eq(t, reg.String(f), "a -> text")
	Eq(t, reg.String(outer), "text")
}

func TestUnifyLowersLevels(t *testing.T) {
	reg := Registry{}

	outer := reg.Var()
	reg.enter()
	inner := reg.Var()
	Eq(t, reg.Unify(outer, reg.List(inner)), nil)
	reg.exit()
	reg.generalize(inner)

	// inner escaped into outer's type, so it isn't generalized.
	Eq(t, reg.at(reg.Resolve(inner)).kind, varKind)
}

func TestResolve(t *testing.T) {
	reg := Registry{}

	a := reg.Var()
	b := reg.Var()

	Eq(t, reg.Resolve(a), a)
	Eq(t, reg.Resolve(b), b)

	Eq(t, reg.Unify(a, b), nil)
	Eq(t, reg.Resolve(a), b)

	Eq(t, reg.Unify(b, IntRef), nil)
	Eq(t, reg.Resolve(a), IntRef)
	Eq(t, reg.Resolve(b), IntRef)
}

func TestUnify(t *testing.T) {
	reg := Registry{}

	res := reg.Var()
	a := reg.Var()
	fn := reg.Func(a, reg.List(a))
	Eq(t, reg.Unify(fn, reg.Func(IntRef, res)), nil)

	Eq(t, reg.String(res), "list int")
}

func TestUnifyOccurs(t *testing.T) {
	reg := Registry{}

	a := reg.Var()
	b := reg.Var()
	Eq(t, reg.Unify(b, reg.List(a)), nil)
	// a occurs in b only through b's binding.
	Eq(t, reg.Unify(a, reg.List(b)).Error(), "cannot construct infinite type 'a' = 'list (list a)'")
}

func TestUnifyRows(t *testing.T) {
	reg := Registry{}

	open := func(labels map[string]TypeRef) TypeRef {
		return reg.add(node{kind: recordKind, a: reg.row(labels, reg.Var())})
	}

	// Open rows take each other's labels, in any order.
	x := open(map[string]TypeRef{"a": IntRef})
	y := open(map[string]TypeRef{"b": TextRef})
	Eq(t, reg.Unify(x, y), nil)
	Eq(t, reg.String(x), "{ a : int, b : text, ...a }")

	// A closed row can't take labels.
	closed := reg.Record(map[string]TypeRef{"a": IntRef, "b": TextRef})
	Eq(t, reg.Unify(x, closed), nil)
	Eq(t, reg.String(x), "{ a : int, b : text }")
	Eq(t, reg.Unify(open(map[string]TypeRef{"c": IntRef}), closed).Error(),
		"cannot unify '{ c : int, ...a }' with '{ a : int, b : text }'")

	// Shared labels must have the same types.
	Eq(t, reg.Unify(open(map[string]TypeRef{"a": TextRef}), closed).Error(),
		"cannot unify 'text' with 'int'")

	// A row can't contain itself.
	r := reg.Var()
	z := reg.add(node{kind: recordKind, a: reg.extend(reg.intern("a"), IntRef, r)})
	w := reg.add(node{kind: recordKind, a: reg.extend(reg.intern("b"), IntRef, r)})
	Neq(t, reg.Unify(z, w), nil)
}

func Neq[T comparable](t *testing.T, a, b T) {
	t.Helper()
	if a == b {
		t.Errorf("Expected %v NOT to be %v", a, b)
	}
}

func Eq[T comparable](t *testing.T, a, b T) {
	t.Helper()
	if a != b {
		t.Errorf("Expected %v to be %v", a, b)
	}
}
