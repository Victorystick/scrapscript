# Proposal: lambda sets, specialisation and fusion

Status: design, with measurements; no implementation yet. TODO.md section
5 lists lambda set inference as an open item.

Infer, as part of each function type, the set of lambdas whose closures can
reach it, as Roc does (Brandon et al., "Better Defunctionalization through
Lambda Set Specialization", PLDI 2023). Then use the sets to call known code
directly, to specialise higher-order functions per set of function arguments,
and to fuse builtins over lists. No syntax changes: lambda sets are inferred,
erased and never printed.

## Where the time goes

Measured with a throwaway patch that recorded, per call site of the core,
which functions it called at runtime, on the two access-log programs over
2000 lines. The numbers per site are an upper bound on what static sets can
resolve.

| | AccessLog (testdata) | AccessLogLib (combinators) |
|---|---|---|
| Calls through a value | 30,546 at 58 sites | 831,629 at 55 sites |
| at sites that call one lambda | 100% | 45% (50 sites) |
| at sites that call several | 0% | 55% (5 sites) |
| that build a partial application | 6.5% | 24% |
| that pass more arguments than the callee takes | 0% | 12.5% |
| Closures made | 59 | 135,838, at 7 sites |

The five polymorphic sites are all in lib/parser.scrap's combinators: `p
input` in `map`, `and-then` and `one-of` (3 to 5 callees), `f r.value` (11)
and `f r.value r.rest` (16, every call over-applied). The closures come from
`many p`, which builds its parser again on every call.

A memory profile of AccessLogLib attributes 36% of allocated bytes to
frames, 20% to partial applications (`withArgs`) and 14% to closures
(`opLambda`); allocation and GC are about a third of CPU time.

So first-order code already calls one function per site, which the inliner
(eval/compile.go `inline`) exploits for where-bound names. The cost is in
combinators, where a function parameter is called with many functions. One
set per function type, a monovariant analysis, leaves those sites as they
are: `p` in `and-then` really does see five parsers. Lambda sets pay off
with **specialisation per set**: a copy of `and-then` per parser that it is
applied to, in which `p input` calls one known lambda. Roc gets this from
monomorphising every polymorphic function.

## Design

### 1. Sets in types

A function type gets a third part, its lambda set: a row (SPEC.md section
7.3) whose labels are lambda identities, one per lambda node of a source,
and builtins. All labels are present, so rows need no presence flags here.
Unifying two function types unions their sets with the row unifier, so the
machinery exists. A row variable as the tail keeps a set open.

- Generalisation generalises the set variables with the type variables, so
  each instantiation of `and-then` has its own sets. At a call site, the set
  of `and-then`'s parameter `p` is closed: the parsers passed there.
- A host's functions, and values a host passes in (`Environment.Apply`),
  are an unknown member, which forces the generic call path.
- Sets are erased: types print without them, as presence flags are.
- Storage: `node` has two children, so the set lives in a side array by
  function node, which `instantiate` copies. Cost on Infer to be measured.

### 2. Phase A: measure statically

Compute the sets and report, per call site, the static set size next to the
dynamic one from the patch above. This decides whether the rest is worth
it, before the type system changes for good. A 0-CFA over the core gives the
same monovariant answer without touching types, and is the cheaper way to
run this phase. Only phase C needs sets per instantiation, which is what
types give for free.

### 3. Phase B: known calls

A call site whose callee's set is one lambda compiles to a direct call of
its code (`opCallKnown`), skipping `enter`'s dispatch on the value's kind
and the arity lookup. With the arity known statically, an under-applied call
builds its partial application directly, and an over-applied one splits
into two calls at compile time instead of in `enterN`. This covers values
that the inliner can't see: functions in records, in arguments and from
imports.

### 4. Phase C: specialisation

A where-bound function called with closed sets for its function parameters
compiles to one copy per distinct tuple of sets, memoised so that recursion
with the same sets reuses its copy (`many p` calls `many p`). Within a copy,
calls to those parameters are known calls (phase B), and small lambdas
inline into it as they do into where-bound callers now. A budget, as
`inlineBudget` bounds inlining, caps the copies per function, with the
generic version as the fallback.

Dictionary passing (proposals/implicits.md) is the same case: `max` called
with the instance for `int` is `max` with `o.cmp`'s set closed to
`int/cmp`, so a specialised `max` calls the builtin directly. This is GHC's
`SPECIALISE`, without pragmas.

### 5. Phase D: smaller closures

With a set of one lambda, a closure needs no `*scriptFunc` with a capture
slice: its code is known wherever it goes, so a closure with one capture can
be that capture alone, in the two words of a `value`. A set of several
lambdas needs a tag besides. That would remove much of the 14% that closure
creation allocates. It needs every site that the closure can reach to know
its set, so it comes last.

### 6. Fusion of builtins

With a known function argument, `list/map f xs` compiles to a loop that
calls known code, and chains fuse into one pass:

| Core | Fused |
|---|---|
| `list/map g (list/map f xs)` | `list/map (f >> g) xs` |
| `list/fold z h (list/map f xs)` | `list/fold z (a -> x -> h a (f x)) xs` |

Fusion makes the same calls in another order: `f` and `g` alternate per
element instead of `f` on every element first. Evaluation is pure, so only a
failure could tell, by which call fails first. Once TODO.md section 9's
builtins are total, a well-typed function can't fail and fusion is
unobservable; until then it would change which error a failing pipeline
reports, so it waits for them. Rewrites that drop calls, such as
`list/length (list/map f xs)` to `list/length xs`, would also drop a call
that never returns, and are left out.

## Printing

Specialisation is compile-time only. A closure made in a specialised copy is
still its lambda node with its captures, so it prints from its source as
now (SPEC.md section 10, rule 6). `lam` looks code up by lambda and
specialisation, while `scriptFunc.node` stays the lambda.

## Risks

- **Infer cost**: a set per function type, and row unification on every
  function unification. Measure with BenchmarkInfer first; phase A as a CFA
  avoids it until it is proven worth it.
- **Code size**: copies multiply with the sets of nested combinators. The
  budget bounds it; BENCHMARK.md's inlining sweep is the model for tuning
  it.
- **Programs that build code at runtime**, like `many p` rebuilding its
  parser, still allocate per call even when every call is known. Hoisting
  `many p` out of the recursion is the program's job, or later an
  optimisation for loop-invariant closures.

## Recommendation

Phase A as a 0-CFA over the core, reporting on AccessLogLib and the Eval
benchmarks. If the five combinator sites resolve to one lambda per copy
under specialisation, do B and C with type-based sets. Phase B alone
helps code that passes functions through records and imports, but the
measured hot paths need C. Fusion is independent and can come any time after
B.
