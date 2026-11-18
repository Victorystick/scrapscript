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
3. **Optimisation is unobservable.** It never changes what a scrap
   prints, which runtime error it raises (builtins can still fail,
   TODO.md section 9), or whether it terminates. So it may discard or
   duplicate only expressions that are *total*: constants, variables,
   lambdas, records, variants and lists of totals, field accesses, and the
   operators that can't fail (`+ - *` wrap; `++ >+ +<`). A call is never
   total, nor is a match whose arms aren't. Resources (time, depth, ticks)
   may change.
4. **The printing contract.** A closure prints as its source lambda with
   the values of that lambda's free names (SPEC.md section 10, rule 6;
   eval/print.go). Every lambda the optimiser makes keeps a print origin:
   the lambda it was copied from, whose `uses` say which captured value
   prints as which name. Every variable it copies keeps its origin
   binder, so captures map back to names. A closure's runtime value keeps
   its print origin separate from its code (today `scriptFunc.node` is
   both), so that a specialised copy prints as its source.
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

## Beta-reduction and printing

`ex add ; ex = f -> a -> b -> f a b` is equal to
`a -> b -> add a b` (β: `add` is a variable, so a value, and substituting
a value is sound under strict evaluation), and that to `add` (η: in
`x -> g x`, if `g` is a value without `x`). Nothing in the language can
tell them apart: functions can't be compared, and there are no effects.
Only resources differ, and printing: today the first prints as
`ex add ; ex = f -> a -> b -> f a b`, following rule 6's "a function
applied to fewer arguments than it takes prints as that application".

Printing a normal form instead is reasonable, and would answer TODO.md
section 8: `list/fold (y -> y) (acc -> x -> (f ; f = y -> acc y + x))
(list/repeat 300 1)` prints 189 KB because each closure prints the one it
captured; β-reduced, it is `y -> y + 1 + 1 + …`, linear in 300. But it
is a design change to SPEC.md, separate from this plan, and the plan
should neither depend on it nor rule it out:

- **What it would take.** A small set of rewrite rules in SPEC.md, applied
  when printing to a closure's source lambda with its captured values
  substituted. They must terminate and never grow code, as shrinking
  reductions do (Appel and Jim 1997): β when the argument is an atom or
  the parameter is used at most once, η when the head is a variable, and
  dropping unused total bindings. Hash references stay opaque, or printing
  would inline libraries. Rule 6's partial-application form would change.
- **What makes it hard.** Printed output must check (rule 6), and
  recursive types need annotations (section 7.5), so a rule may not
  reduce away an annotated binding that the output's types need. Printed
  output loses the user's names and sugar, which is a cost when debugging.
  And since printed output can be published, the rules are part of the
  language: changing them is an edition change (section 9.1).
- **Why it doesn't complect this plan.** If printing reduced with the
  optimiser's rules, every heuristic (inlining budgets, specialisation)
  would become observable and part of the spec. Kept apart, the optimiser
  stays free, and printing stays a function of the source lambda and its
  captures, which decision 4 keeps available either way. What they share
  is machinery, not policy: one library of substitution, renaming, β and
  η over the IR, which the optimiser uses freely and printing would use
  with SPEC.md's fixed subset. Step 1 makes that library possible, since
  substitution needs variables instead of slots.

So: build the steps below without print normalisation, keep decision 4,
and decide print normalisation as its own SPEC.md change once step 3 has
built the substitution machinery.

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
tests that guard the printing contract and inlining are eval's
compile_test.go (the core's dumps), roundtrip_test.go (printed values read
back the same), and `TestInliningAcrossScraps` and
`TestKnownCallsAcrossScraps` in env_test.go.

0. [ ] **Compile benchmark.** `BenchmarkCompile`: lowering, the passes and
   allocation for the AccessLogLib program and the Eval sources, without
   evaluating; add it to `bench/ir.sh`. The baseline of main before this
   plan is `bench/2026-11-18-0a92ecc.ir`; commit a new one with Compile.
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
7. [ ] **Function arguments.** Specialise a known function on an argument
   that is a known lambda, or a partial application of one, passing its
   captures as arguments, within a code-size budget; a recursive call
   that passes the parameter on calls the copy. And raise arity: a lambda
   whose body is a partial application of a known function with total
   arguments takes the missing ones itself (η-expansion), so that
   `f -> pa |> map f` called with two arguments builds no closure.
   Targets: AccessLog's `upsert path (hit status ms)`, AccessLogLib's
   closures, partial applications and argument slices. TODO.md section
   5's lambda sets are the general form; decide after this.
8. [ ] **Evaluation state per evaluation.** The frame pool, the depth and
   the tick count belong to an environment today, which ties its
   functions to one goroutine (eval/env.go). A platform that serves
   requests concurrently (SPEC.md section 13) needs them per evaluation,
   carried with frames. Independent of the steps above; do it before such
   a platform.

## Outside this plan

- **Lists are arrays,** so `x >+ xs` copies `xs`, and a list built by
  prepending is quadratic (AccessLogLib's parser, `many`). A
  representation change (a persistent vector, or chunks that share a
  tail) rather than an optimisation.
- **Print normalisation** (above), as a SPEC.md change after step 3.
- **The `core` hash scheme** (SPEC.md section 9.1.1), on lowering's output
  (decision 2), after an encoding is chosen.
