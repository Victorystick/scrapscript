# Proposal: explicit monads

Status: prototype, on branch `ccr-a1bb1e05-06ownm`. Nothing here is in
SPEC.md yet.

An alternative to type classes as implicit instances
(`claude/hopeful-bohr-n1qwg4-implicits`). A "class" is a record type and
an "instance" is a record value, as there; but generic code takes the
instance as an ordinary argument, which its callers pass. Nothing is
resolved by type, so the core, evaluation order and printing are unchanged.
What the language lacked to write a monad that way is two things in types:

```scrap
traverse option-monad text/to-int ["1", "2"]          -- #some [1, 2]

; monad : (type -> type) -> type = m =>
    { pure : a => a -> m a                             -- polymorphic fields
    , bind : a => b => m a -> (a -> m b) -> m b        -- m is a constructor
    }

; option-monad : monad option =
    { pure = x -> #some x
    , bind = | #some x -> f -> f x
             | #none   -> _ -> #none
    }

; traverse : m => a => b => monad m -> (a -> m b) -> list a -> m (list b) = d -> f ->
    list/fold (d.pure []) (acc -> x ->
      d.bind acc   <| ys ->
      d.bind (f x) <| y ->
      d.pure (ys +< y))

; option : type -> type = a => [#some a, #none]
```

## Why explicit

- The call site shows every argument. `traverse option-monad` is the
  choice; to choose another, pass another.
- Types stay erased, and the core follows from syntax alone. Nothing in the
  checker decides which value a program passes (SPEC.md section 5, section 9).
- Instances are values, so they compose as values do: `ops d` derives
  operations once, a library exports instances as fields, and `result-monad`
  is generic in its error type with no instance search.
- No new syntax at all for values. `<|` chains read as do-notation: each
  `d.bind m <| x ->` line is one `x <- m`.

The cost is writing the dictionary, once per generic call, or once per
scope with `; o = m.ops m.option-monad`.

## The language

### Polymorphic fields

A field of a record type may bind its own type variables:

```
tfield = NAME ":" [ NAME "=>" { NAME "=>" } ] type
```

`{ id : a => a -> a }` is a record whose field works at every type. Its
variables are types: not rows, not constructors, and not the names of
variables in scope.

- **Uses choose.** Each access `r.id` has the field's type with fresh
  variables, so `{ a = r.id 1, b = r.id "" }` is fine.
- **Values can't.** A record literal checked against such a type checks the
  field's value with the variables rigid, as an annotated binding's are:
  `{ id = x -> x + 1 }` fails, `cannot unify 'a' with 'int'`.
- **Only annotations give them.** A polymorphic field's type comes from an
  annotation and is never inferred, as in OCaml's polymorphic record fields:
  inference stays Hindley–Milner, since it never guesses a polymorphic type.
  A literal is checked against one where an annotation or a function's
  parameter type provides it (`f { id = x -> x }` with `f`'s annotation
  taking one). An unannotated record that meets one is an error that says
  so: `which is polymorphic: give the value an annotation`.
- **Equality.** Two polymorphic fields unify if their bodies do, for fresh
  rigid variables that neither may bind.

### Constructor variables

A type variable may stand for a type constructor. Its kind is found from its
uses, as a sort binding's parameters' are: applied to n types, it takes n.

```scrap
; f : m => (m int -> m text) -> m int -> m text = g -> g
; monad = m => { pure : a => a -> m a }          -- kind (type -> type) -> type
```

Kinds may say so: `monad : (type -> type) -> type`. A constructor's
parameters are types; constructors of constructors are not supported.

The argument for such a parameter is a constructor: a variable of that
arity, `list`, an alias or type scrap of that many parameters, or one
applied to all but its last ones, as `state s` or `result e`:

```scrap
; result-monad : e => monad (result e) = …
; state-monad : s => monad (state s) = …
```

Types print the same way: `monad option`, `monad (state a)`.

### Inferring a constructor

Types are structural, so `[#some int, #none]` has no constructor to read
off: it is `option int`, and also a constructor that ignores its argument,
applied to `text`. So a
constructor variable is inferred in two ways only:

1. **From another application**: `m a = n b` makes `m = n` and `a = b`.
2. **From a polymorphic field** (Miller's pattern fragment). Checking
   `option-monad`'s `pure` against `monad m`'s makes `m a = option a` for a
   rigid `a` that `m` can't depend on, which has exactly one solution:
   `m = a => option a`.

The second is why a dictionary should come first: `traverse option-monad`
fixes `m` from `option-monad`'s annotation before `f` and the list are
checked. Without a dictionary, an application can't be inferred:

```
g [1] ; g : m => m int -> int = _ -> 1
error: cannot infer the type constructor of 'a int' from 'list int': pass a value whose annotation names it, such as a monad, first
```

This is the price of structural types. Haskell reads `m` off a nominal type
constructor; here a constructor exists only where an annotation names one.

## lib/monad.scrap

Exports `monad`, the types `option`, `result` and `state`, the instances
`option-monad`, `result-monad`, `list-monad` and `state-monad`, and generic
code that takes one: `ops` (`map`, `join`, `then`), `traverse`, `sequence`,
and `get`, `put` and `modify` for state.

```scrap
number ["a", "b"] 10       -- { value = [{ n = 10, x = "a" }, { n = 11, x = "b" }], state = 12 }
; number : list text -> m.state int (list { n : int, x : text }) =
    m.traverse s (x ->
      s.bind m.get <| n ->
      (m.ops s).then (m.put (n + 1)) (s.pure { n = n, x = x }))
; s = m.state-monad
; m = $"./monad.scrap"
```

## Prototype

| Part | Where |
|---|---|
| `forall`, `lam`, `app` and bound-variable nodes; reduction in `Resolve` | types/type.go |
| Unifying foralls by skolems; `unifyApp`, the pattern fragment | types/type.go |
| Kinds with arity; constructors as arguments, partially applied | types/typexpr.go |
| Polymorphic fields: opened at use, checked in record literals and arguments | types/infer.go |
| Printing `a => T` fields, `m a`, constructors eta-reduced | types/type.go |
| Tests | types/infer_test.go, `TestMonadLibrary` in eval/test_test.go |
| A library | lib/monad.scrap |

No change to the parser, the core, the evaluator or the printer of values.
Cost on programs without these types, against main with interleaved runs and
benchstat: Infer and every Eval benchmark within noise, allocations
unchanged.

## Compared with implicits

| | Implicits branch | This branch |
|---|---|---|
| Call site | `traverse f xs` | `traverse option-monad f xs` |
| Choosing another instance | shadow it in an inner scope | pass another |
| Core depends on the checker | yes: the arguments it chose | no |
| Instances | anonymous `_` bindings, found by type | named values |
| Derived instances | solved: `ord (list a)` from `ord a` | applied: `ord.list ord.int` |
| Needs polymorphic fields and constructor variables for monads | yes | yes |
| Change, without tests | ~700 lines: parser, checker, compiler, printer | ~550 lines, all in types/ |

## Open

- **Patterns.** Destructuring a polymorphic field, `| { pure = p } -> …`,
  binds `p` at one type, since a match function's patterns are inferred
  before its argument. Field access keeps the polymorphism.
- **Kinds of polymorphic fields' variables.** Rows (`r => { a : int, ..r }
  -> int`) and constructors are rejected; rows would need presence flags
  under a quantifier.
- **Superclasses.** `monad` could have a `functor` field, or `ops` could
  return one; nothing in the type system relates them, as nothing
  should need to.
- **Laws.** Not checked, as in Haskell. Tests can check them per instance.
- **SPEC.md.** Section 7.2's grammar (`tfield`, kinds with constructor
  parameters), section 7.1's "type classes are deferred", and section 7.4's
  open items would change if this is adopted.
- **Performance.** Every `bind` is an indirect call through a record, as with
  any dictionary. The inliner and lambda sets can resolve them where the
  dictionary is a known value.
