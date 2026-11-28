# Compilation plan

Split `compile` (eval/compile.go) into lowering, optimisation and
allocation, so that optimisations become small passes over one IR instead
of special cases in a single pass over the AST. Built as a series of small
PRs, in order; tick them off here as they land, with their numbers. Read
AGENTS.md first: discuss design changes before implementing them, and make
one change at a time.

## Where things stand

The evaluator is the core compiled to Go closures (eval/closures.go). The
core is made in one pass over the checked AST, which at once:

- resolves names, and assigns each binder a slot of its frame as it is
  emitted (`c.next`, `c.alloc`), so the core exists only with its slots
  already chosen;
- computes each lambda's captures (`c.fns`, `capture`);
- inlines calls of small non-recursive where-bound functions by lowering
  the callee's AST again at the call site (`inline`, `match`), its slots
  after the caller's;
- marks calls of known functions (`known`, `opCallKnown`), which needs a
  binder to note its lambda after the fact (`binder.fn`), since a
  recursive call is lowered before its function's lambda exists;
- chooses operators and records' shapes from the checker's types;
- records what printing closures needs (`uses`, `binders`, `roots`,
  `wheres`).

PRs #40 to #43 (November 2026) made variants hold their payloads, closures
one allocation, frames recycled and known calls direct: instructions -23%
(geomean of the evaluation benchmarks), AccessLog bytes -75%. What still
allocates, per op:

| | AccessLog (155k) | AccessLogLib (4.3M) |
|---|---|---|
| record literals | 50% | 25% |
| record spreads | 13% | |
| partial applications and closures | 6% | 39% |
| argument slices of calls that aren't exactly saturated | | 12% |
| `x >+ xs`, which copies the list | | 14% |
| list literals | | 5% |
| `text/split`, and the host's and the test's strings | 26% | |

Each optimisation left on the table ran into the single pass:

- **Frames are as large as their largest path.** Slots come from a bump
  counter during lowering, so `lean`'s frame has 22 slots for the arm that
  inlines `node`, while its other arms use 5. Nothing after lowering can
  shrink it.
- **Constructions that are destructured at once stay.** In
  Eval/fields, destructure-closed and rows every remaining allocation is
  a record that one inlined body builds and the next takes apart, as the
  core of destructure-closed shows:
  `(match (match @0 | >2 -> { a = @2, b = (+ @2 2), c = "ignored" }) | { a = >2, b = >3, c = _ } -> (+ @2 @3))`,
  and rows' `(match { x = @0 } | >2 -> @2.x)`. Removing them needs
  rewriting after inlining, which the core can't take (its slots are
  fixed) and the AST hasn't had (inlining hasn't happened).
- **Copying a body means lowering its AST again.** Inlining does that;
  passing a record's fields instead of the record (worker/wrapper),
  returning a record into the caller's slots, and specialising a function
  on a function argument all need copies of bodies that are then
  rewritten.
- **Known calls are a back-patch** (`binder.fn`, and `lam` registering a
  lambda before compiling its arms) instead of a lookup.

## Decisions

1. **One IR, with variables until allocation.** The core keeps its shape
   (tree-shaped nodes in flat arrays indexed by `cnode`, pools in `extra`,
   `at` pointing back to the AST), but names refer to variables, which
   are binders, not to slots or captures. A pass after optimisation
   assigns slots and computes captures into side tables. Not ANF: it
   multiplies nodes and lowering time, and the tree already shows where a
   value flows.
2. **Lowering is desugaring.** It makes no choices that optimisation
   could make (no slots after step 1, no inlining after step 3), so its
   output is a deterministic function of the source, close to SPEC.md
   section 5's core. That is
   where the `core` hash scheme's canonical encoding (section 9.1.1) can
   be defined later, on output that no optimisation has touched.
3. **Optimisation is unobservable.** It never changes a scrap's value
   (SPEC.md section 10, rule 6), which runtime error it raises (builtins
   can still fail, TODO.md section 9), or whether it terminates, and
   printing doesn't see it (decision 4). So it may discard or
   duplicate only expressions that are *total*: constants, variables,
   lambdas, records, variants and lists of totals, field accesses, and the
   operators that can't fail (`+ - *` wrap; `++ >+ +<`). A call is never
   total, nor is a match whose arms aren't. Resources (time, depth, ticks)
   may change.
4. **Printing reads origins, not optimised code.** Rule 6 lets a value
   print as any scrap that evaluates to it and is a fixpoint, so printing
   optimised code would be allowed. It doesn't, for two reasons. Heuristic
   inlining and specialisation aren't confluent: a specialised `ex add ;
   ex = f -> a -> b -> f a b` printing as `a -> b -> add a b` reprints as
   `add` once η-reduced, which isn't a fixpoint. And decision 8's test
   could no longer compare printed results, since functions can't be
   compared. So a closure prints from its source lambda and the values of
   that lambda's free names (eval/print.go), simplified by printing's own
   rules (below). Every lambda the optimiser makes keeps a print origin:
   the lambda it was copied from, whose `uses` say which captured value
   prints as which name. Every variable it copies keeps its origin
   binder, so captures map back to names. A closure's runtime value keeps
   its print origin separate from its code (today `scriptFunc.node` is
   both), so that a specialised copy prints as its source. Printing
   decides sharing by structure, as print.go's `same` does, never by
   identity, since the optimiser may duplicate totals.
5. **Representation types on variables.** Lowering keeps what the checker
   knows about each variable's representation: int, float, text, bytes,
   list, a record of a closed shape, an open record, a variant, a
   function. Not polymorphic types: types stay erased (AGENTS.md). This is
   TODO.md section 3's first item, and what unboxing needs.
6. **Deterministic passes.** No pass's output may depend on Go's map
   iteration order.
7. **Compile time is a budget.** Nothing measures it today:
   BenchmarkRead stops after checking, and the evaluation benchmarks
   compile during setup. Each step reports it.
8. **Optimisation can be switched off.** The compiler takes a level:
   lower and allocate only, or with the passes. Tests run every program
   both ways and compare printed results and errors, which is how a new
   pass's mistakes show up.

## Print normalisation

`ex add ; ex = f -> a -> b -> f a b` is equal to
`a -> b -> add a b` (β: `add` is a variable, so a value, and substituting
a value is sound under strict evaluation), and that to `add` (η: in
`x -> g x`, if `g` is a value without `x`). Nothing in the language can
tell them apart: functions can't be compared, and there are no effects.
SPEC.md's rule 6 only asks that a value print as a scrap that evaluates
to it, is a fixpoint, and is in proportion to the value, so any of the
three may print. Today the first does, as its source.

Printing normal forms would answer TODO.md section 8:
`list/fold (y -> y) (acc -> x -> (f ; f = y -> acc y + x))
(list/repeat 300 1)` prints 189 KB because each closure prints the one it
captured; β-reduced, it is `y -> y + 1 + 1 + …`, linear in 300 (and `y +
300`, since `+` wraps and so associates). The rules are the
implementation's, not the language's: changing them changes what
`scrap eval` prints, and web/'s expected outputs, but no scrap's meaning,
so no edition.

- **Rules.** Applied to a closure's source lambda with its captured
  values, they must terminate and never grow code, as shrinking
  reductions do (Appel and Jim 1997): β when the argument is a value and
  the parameter is used at most once, or the argument is an atom; η when
  the head is a variable; dropping unused bindings; folding total
  operators only. Never unfold a recursive binding: `f 3` stays. Hash
  references and builtins stay opaque.
- **Fixpoint.** The rules must be idempotent, and names, binding order
  and sharing must come from the value's structure, so that a normal form
  evaluates to a closure whose source normalises to itself. Clashing
  names numbered by position (TODO.md section 8) suit this better than
  primes.
- **Checking and exports.** Rule 6 lets an evaluated scrap replace the
  original as an import, so printing keeps members and the annotations
  of what the result exports, which may narrow its type and name its
  aliases. A rule may not reduce those away, nor an annotation or sort
  binding that the output needs to check, as recursive types do (SPEC.md
  section 7.5). It may drop the annotation of a binding it inlines,
  where the output checks without it.
- **Names.** Printed output loses the user's names and sugar, a cost when
  debugging. A flag that prints sources unsimplified would keep them, and
  satisfies rule 6 as today's printer does.
- **Shared machinery, separate policy.** One library of substitution,
  renaming, β and η over the IR, which the optimiser uses with its
  heuristics and printing with its fixed rules. Step 1 makes it possible,
  since substitution needs variables instead of slots; step 3 builds it.
- **Testing.** Every program's printed result, evaluated and printed
  again, gives the same text and checks, with optimisation on and off.

So: build the steps below as planned, and normalise printing in its own
PR once step 3 has built the library. It needs no SPEC.md change.

## Measuring

- **Instructions:** `bench/ir.sh` counts instructions per op with
  cachegrind, repeatable to 1% (Eval) and 0.1% (AccessLogs) even where
  times vary by 10 to 30%, and writes `bench/<date>-<commit>.ir`, which
  benchstat compares. The collector stops the world for repeatability, so
  write barriers during marking aren't counted; check a change that moves
  barrier costs (such as clearing memory) with times too.
- **Allocations and bytes:** `-benchmem`, exact.
- **Times:** `bench/run.sh`, interleaved against the base, as BENCHMARK.md
  says. On a shared machine, trust only large differences.
- Report each step's instructions, allocations, bytes and compile time
  against the step before, and the frame sizes it changes.

## Steps

Each is one PR, which passes `go test ./...`, web/'s `pnpm test` if output
changes, and leaves performance unchanged unless it says otherwise. The
tests that guard printing and inlining are eval's
compile_test.go (the core's dumps), roundtrip_test.go (printed values read
back the same), and `TestInliningAcrossScraps` and
`TestKnownCallsAcrossScraps` in env_test.go.

0. [x] **Compile benchmark** (#45). `BenchmarkCompile`: lowering, the
   passes and allocation for the AccessLogLib program and the Eval
   sources, without evaluating; in `bench/ir.sh`, repeatable to 0.3%.
   The baseline of main before this plan is `bench/2026-11-18-0a92ecc.ir`,
   and with Compile `bench/2026-11-18-5f3269e.ir`.
1. [ ] **Variables.** Lowering emits binders instead of slots and
   captures; `opCapture` goes, and patterns bind binders. A new
   allocation pass assigns slots and captures exactly as today, into side
   tables that closures.go and print.go read, including each free name's
   capture index (`ref.cap`), which printing reads. Done when the frame
   sizes and captures of every program in the tests are identical, and
   instructions and compile time within 1%. The core's test dumps change
   notation.
2. [ ] **Liveness.** Allocation reuses a slot once its binder is dead,
   instead of only across arms and after groups. Report frame sizes
   (AccessLog's `lean`, `upsert`, `balance`).
3. [ ] **Inlining on the IR.** Copy a lambda's arms with fresh binders,
   keeping origins (decision 4), instead of lowering its AST again. Same
   heuristic, measured in IR nodes instead of tokens, re-tuned with a
   sweep as BENCHMARK.md's `7b430d4` row did. Known calls become a lookup
   from binder to lambda, and `binder.fn` goes. This step builds the
   substitution and renaming library.
4. [ ] **Simplifier.** Case-of-known-constructor (a match on a record,
   variant or list built in view selects its arm and binds its fields),
   substituting atoms for the binders bound to them, and dropping unused
   total bindings (decision 3). Then join points, so that a match on a
   match moves into the inner match's arms without copying the outer
   arms; a jump writes its argument to a slot and runs the join's code in
   the same frame. Targets: every allocation of Eval/fields and
   destructure-closed, most of rows.
5. [ ] **Decision trees.** Compile matches as TODO.md section 6 says,
   sharing types/exhaustive.go's matrix steps, so that each value is
   tested once. Allocation then sizes a frame per arm, as TODO.md section
   5 asks.
6. [ ] **Records across known calls.** A known function whose parameter
   is a record of a closed shape that it only reads gets a worker that
   takes the fields, and known calls pass them, built or not; the
   original lambda stays the closure's value, so printing and unknown
   callers are unchanged. Likewise a record that a known call returns
   into a match is written into the caller's slots. Targets: Eval/records,
   avl's `pop-min`.
7. [ ] **Reuse of unique values.** A value is unique where the compiler
   proves it *fresh*, made in this function and not escaped (step 4's
   facts), and at its *last use* (step 2's liveness). Then a spread
   updates it in place, and a match that takes it apart gives its memory
   to a construction of the same size in the arm (Koka's drop-reuse,
   Lean's reset/reuse). Across known calls, a parameter is *owned* if
   every call passes a fresh value at its last use, so that `loop (n - 1)
   { ..r, a = r.a + 1 }` updates `r` in place. No counts at runtime, and
   unobservable (decision 3): a unique value isn't captured or printed
   anywhere else. Persistent structures threaded through a platform's
   state are unique only if the host gives the state back rather than
   keeping it, so decide first, in SPEC.md section 13, that passing the
   state to `step` moves it: a program's state is already opaque to the
   host, and the generated bindings must then drop their handle on it.
   Targets: Eval/records, AccessLog's state spread and `upsert`'s path of
   `#node` rebuilds, most of its 77k record literals per run.
8. [ ] **Function arguments.** Specialise a known function on an argument
   that is a known lambda, or a partial application of one, passing its
   captures as arguments, within a code-size budget; a recursive call
   that passes the parameter on calls the copy. And raise arity: a lambda
   whose body is a partial application of a known function with total
   arguments takes the missing ones itself (η-expansion), so that
   `f -> pa |> map f` called with two arguments builds no closure.
   Targets: AccessLog's `upsert path (hit status ms)`, AccessLogLib's
   closures, partial applications and argument slices. TODO.md section
   5's lambda sets are the general form; decide after this.
9. [ ] **Evaluation state per evaluation.** The frame pool, the depth and
   the tick count belong to an environment today, which ties its
   functions to one goroutine (eval/env.go). A platform that serves
   requests concurrently (SPEC.md section 13) needs them per evaluation,
   carried with frames. Independent of the steps above; do it before such
   a platform.

## Outside this plan

- **Lists, text and bytes copy to grow.** A list's capacity is its length,
  so `l +< x` copies `l` every time, and a list built by appending is
  quadratic, as text built by `acc ++ s` is; `x >+ xs` copies `xs`
  (AccessLogLib's parser, `many`). Without any ownership: keep, with a
  backing array, the length of the longest value that uses it, and write
  `l +< x` in place when `l` is that long, since every other value that
  shares the array is shorter and can't see the new element; otherwise
  copy into double the capacity. Appends become amortized O(1), safe
  whoever else holds the array. The same for `++` on text (in a buffer of
  our own) and bytes, and for `>+` with room at the front and the lowest
  index used. A representation change, independent of the steps above.
- **Dynamic uniqueness,** only if step 7 leaves measured cases: a sticky
  one-bit "shared" flag per object (not a count), set where the compiler
  can't rule out duplication. It needs a header per object, which values
  don't have, and care with list tails, which share arrays, with
  builtins, and with values that hosts hold. Reference counts proper fit
  this implementation poorly: every copy of a two-word value would pay,
  and a bug mutates an immutable value silently.
- **Print normalisation** (above), after step 3.
- **The `core` hash scheme** (SPEC.md section 9.1.1), on lowering's output
  (decision 2), after an encoding is chosen.
