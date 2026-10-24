# Proposal: type classes as implicit instances

Status: prototype, on branch `claude/hopeful-bohr-n1qwg4-implicits`. Nothing
here is in SPEC.md yet.

Type classes for a language without declarations, global names or nominal
types. A constrained function says what it needs, `ord a =>`, and its
callers don't pass it: the checker finds the instance in the scope of the
call and passes it, as an ordinary argument. Instances are ordinary values
bound in where-clauses, so they are lexical and a scrap stays closed.

```scrap
max-of 0 [3, 5, 1]

; ord : type -> type = a => { cmp : a -> a -> [#lt, #eq, #gt] }

; max : a => ord a => a -> a -> a = o -> x -> y ->
    o.cmp x y |> | #lt -> y | _ -> x

; max-of : a => ord a => a -> list a -> a = o -> z -> list/fold z max

; _ : ord int = { cmp = int/cmp }
```

## Why

- Each library carries its comparison by hand: avl takes `cmp` first in every
  operation (`avl.insert text/cmp`), and lib/parser.scrap, lib/avl.scrap and
  lib/access-log.scrap each write their own `same` or `larger`.
- `+ - *` are overloaded on int and float by an ad hoc rule in the checker,
  and TODO.md section 9 proposes another for `==` and `<`. Users can't write
  such functions themselves.
- A comparison that travels with a value's type, rather than as an
  argument, can't be forgotten or mixed up between calls.

## Constraints from SPEC.md

- **Closed scraps** (section 10, rule 1). No global instance table: that
  would be ambient state. Instances must come from the scrap's own source.
- **An import means its source inlined in parentheses.** Instances in a
  library's where-clause are invisible to its importers, so libraries export
  instances as values and importers bring them into scope.
- **Structural types, no declarations** (section 7.1). A class is not a new
  kind of thing; it is a record type, and an instance is a record.
- **Types are erased.** Resolution happens in the checker, and the core sees
  only the arguments it chose. The core (section 5) is unchanged.
- **Results are scraps** (section 10, rule 6). A function applied to its
  instances must print as code that finds the same instances again.
- **One way to write each thing.** No explicit instance arguments at call
  sites; to choose another instance, bind it in an inner scope.

## The language

### Classes are record types

A class is any type, usually a type constructor of a record, as `ord`
above. Nothing marks it as a class. Two classes with the same shape are the
same class, as two records of the same shape are the same type.

### Constraints

An annotation may list constraints after its type variables, each followed
by `=>`:

```
type     = NAME "=>" type | arrow [ "=>" type ]
```

`c => t` is a function type whose argument callers don't pass. A constraint
is never a lone NAME, which would bind a type variable, so it is a type
application, a record or enum type, or a parenthesised type. Constraints
come first: `(ord a => a) -> int` is an error.

A constrained function takes its instances as its first parameters, so its
value starts with one `NAME ->` per constraint:

```scrap
; cmp : a => ord a => a -> a -> order = o -> o.cmp
```

In its body the parameter is an ordinary name, and also an instance that its
constraints can be solved with: `max-of` passes its own instance to `max`.

### Instances

A binding of `_` is an instance. No name refers to it, only constraints.

```scrap
; _ : ord int = { cmp = int/cmp }                         -- annotated
; _ = { cmp = text/cmp }                                  -- of its value's type
; _ : a => ord a => ord (list a) = o -> { cmp = lex o.cmp }  -- derived
; _ = o.list                                              -- another's, by name
```

- An annotated instance has the type its annotation declares, and may be
  constrained itself: the instance for lists takes the one for elements.
- An instance without an annotation is an instance of its value's type. A
  name or field as its value keeps its constraints, so `_ = o.list` binds a
  derived instance that a library exports.
- `_` cannot be used as a value. Before this change `_ ; _ = 2` evaluated to
  2, against SPEC.md section 2.1.

### Resolution

Each use of a constrained name, an Ident, a field access or an import,
takes one instance per constraint, found in the scope of that use:

1. Scopes are searched from the innermost out: a where-group's instances,
   then a constrained function's parameters, then the enclosing ones.
2. The first scope that has an instance that fits decides. Two that fit in
   that scope are ambiguous, an error. An inner scope may shadow an outer
   instance, which is how a caller chooses another ordering:
   `(sort xs ; _ = reverse int-ord)`.
3. An instance's own constraints are solved the same way, at the same use.
4. A constraint whose type still has variables waits while instances fit it
   only by deciding them, since later code may. At the end of a binding
   whose type is generalized, its variables must be decided: the one
   instance that fits decides them, or it is an error.

So constraints are never inferred. Only an annotation gives a binding
constraints; an unannotated binding that uses `max` at an unknown type gets
the one instance in scope, or fails and asks for an annotation:

```
f ; f = x -> y -> max x y ; _ : ord int = … ; _ : ord text = …
error: max has more than one instance of 'ord a' in scope
```

This is deliberate: it keeps the type of every unannotated binding free of
constraints, which keeps inference, printing and evaluation order simple, and
makes generic code say what it needs.

### Exports

A record literal that is a scrap's value, or an unannotated binding's,
keeps the constraints of fields that are names. A library exports
constrained functions and instances like any other value:

```scrap
{ ord = ord, int = { cmp = int/cmp }, list = list, max = max, sort = sort }
```

and each use of `o.max` takes its instances where it is used:

```scrap
o.sort [[2], [1, 5]] ; _ = o.int ; _ = o.list ; o = $"./ord.scrap"
```

lib/ord.scrap in the branch is such a library.

## Elaboration

The checker records, for each use, the evidence of each constraint: the
instance binding or parameter that solved it, applied to the evidence of
its own constraints (`types.Result.Implicits`). The compiler turns a use into
a call with the instances first, folded into the call's own arguments:
`max 3 4` is `max i 3 4` with `i` the slot of the instance's binding, one
`opCallN`. Instances are frame slots like other bindings.

Evaluation order: a where-group's components are ordered by references by
name and by the instances that uses take, so a binding is evaluated after the
instances it uses. A component that is recursive through instances must be
functions, as SPEC.md section 8 requires: `_ : ord int = { cmp = x -> y ->
cmp x y }` is a recursive value.

## Printing

A function that captures an instance prints with its binding, like any
other captured binding. A constrained function applied to its instances, but
not all its arguments, prints them as instances in scope of the
application:

```
max 3 ; _ : ord int = { cmp = int/cmp } ; …
==> (max 3 ; _ = { cmp = int/cmp }) ; max : … ; ord : … ; cmp : …
```

which evaluates to itself again. This is why instances may lack
annotations: the printer knows an instance's value, not its type.

## Prototype

| Part | Where |
|---|---|
| `c => t` in types; `Implicit`, `Instance` | parser/parser.go, ast/node.go, printer/printer.go |
| Constraint arrows (a flag on function types), trial unification | types/type.go, types/typexpr.go |
| Givens, goals, solving, evidence | types/infer.go |
| Instances in evaluation order | ast/group.go `ComponentsOf` |
| Instances as arguments; printing | eval/compile.go, eval/print.go |
| Tests | eval/implicit_test.go, `TestOrdLibrary` in eval/test_test.go |
| A library | lib/ord.scrap |

Cost on programs without instances, measured against main with interleaved
runs and benchstat: Infer +1.4% (p=0.045), allocations unchanged; Read, Eval
and AccessLog within noise.

## Open

- **Equality and ordering of records.** `eq { a : int, b : text }` can't be
  written as an instance, since rows are open-ended. TODO.md section 9's
  built-in `==` and `<` are complementary: a built-in class that the checker
  solves structurally for every type without functions, as GHC solves
  `Coercible`.
- **Superclasses.** `ord a = { eq : eq a, cmp : … }` nests one class in
  another, but an `ord a` instance doesn't yet provide an `eq a`. Option:
  fields of an instance whose types are classes are instances too.
- **Coherence.** Instances are lexical, so a tree built under one ordering
  can be searched under another, as with avl's explicit `cmp` today. A
  library can avoid it by taking the instance once, as a functor (SPEC.md
  section 12, "members through applications").
- **Parenthesised where-groups merge** with the group around them, so
  `(max 3 ; _ = d) ; _ : ord int = …` has both instances in one scope. That
  is how the parser builds groups today, not specific to instances; it can
  make a printed local instance ambiguous.
- **Hosts.** Constrained functions in a platform's interface aren't
  supported by gogen.
- **Cost.** Dictionary passing makes every method call an indirect call
  through a record. Specialising constrained functions on their instances is
  what lambda sets (proposals/lambda-sets.md) would give for free.
