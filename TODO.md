# TODO

Architectural blemishes, most important first, then gaps in the language
itself (9). Items 1–4 are one piece of work: lower the AST to a core IR
after type checking, and evaluate that. Track the effect on performance in
[BENCHMARK.md](BENCHMARK.md).

## 1. No core IR

SPEC.md section 5 describes a desugared core, but the checker, the evaluator
and the printer each walk the surface AST, handling pipes, composition,
multi-pattern arms, where-groups and pattern operators separately.

- [x] Define the core IR, with each node pointing back to its AST node for
      errors and for printing closures.
- [x] Lower the AST to it after type checking.
- [x] Evaluate the core instead of the AST.
- [ ] Later: define the canonical encoding and the `core` hash scheme on it
      (SPEC.md section 9.1.1).

## 2. Eval re-reads source text on the hot path

- [x] Resolve variables to frame slots (depth, index) instead of looking up
      names, which re-scans each token (`TokenText`) and allocates a string.
- [x] Frames as `[]Value` instead of a `Variables` map per call and per match
      arm.
- [x] Decode literals once, not on every evaluation.

## 3. Types are dropped before evaluation

- [ ] Carry the checker's types into the IR. The
      compiler reads them to choose ops and static field indices (access,
      spreads and rest patterns on records of a closed type), so the core
      keeps no types. Open rows still look labels up.
- [x] Choose operators (`+ - *` on int or float; `+< >+` on lists or
      bytes; `++` on lists, text or bytes) statically instead of by the
      runtime value.
- [ ] Use them to specialise functions per record shape (SPEC.md section 12).
- [ ] Know the shapes of rests statically, so that the shape table stops
      changing once a program is compiled and loses its locks. A rest
      pattern on an open record makes its rest's shape while values run
      (`shapeTable.without`), and access to a field of an open record
      searches its shape. Either collect the closed rows that the checker
      instantiates each open row with, following generic callers to a
      fixpoint, and make those shapes when compiling; or pass rests' shapes
      and fields' indices as hidden arguments (evidence passing, SPEC.md
      section 12), which also spares access its search.

## 4. Errors everywhere after type checking

`(Value, error)` dates from before the checker existed. The evaluator and
builtins now return plain values and panic with a `runtimeError`, which
`catch` turns back into an error at the edge.

- [x] Drop the runtime type checks that the checker already guarantees.
- [x] Builtins return plain values and panic like the evaluator.
- [x] Match without panic and recover: return a bool and write into the
      frame.
- [x] Check exhaustiveness for all patterns, not only enums (SPEC.md
      section 7.1); then a well-typed scrap never reaches "no arm matches".

## 5. Closures keep their whole scope chain

- [x] Capture only the slots a closure uses, not the `*context` chain, while
      still printing with its captured bindings.
- [x] ~~Frames as windows on a value stack instead of a heap slice per
      call~~: tried two ways in October 2026 and dropped, since neither was
      faster than a heap slice per call. Both halved allocations
      (AccessLog 1.07M -> ~0.5M per run, Eval fib 5210 -> 43), but
      allocation was only about a quarter of AccessLog's CPU time, and
      the savings went to new costs:
      - A chunked stack per environment (clox style): +3% on AccessLog.
        The chunks are long-lived heap memory, so while the GC marks,
        every slot write takes a write barrier that also records the old
        value; clearing freed windows doubles those (`clear` takes a bulk
        barrier with an object lookup: +7%). Push/pop bookkeeping on top.
      - Frames in a `[8]Value` buffer in the Go frame of the call that
        owns them (87% of AccessLog's calls need <= 4 slots, all <= 6):
        no barriers on the stack and no clearing, but Go zeroes the
        buffer on every entry to `run`, which is once per node (0.9 s of
        11 s), and copies 6-word frame structs. Splitting `run` so only
        calling code has a buffer added a Go call per node instead. Even
        with the operand fast path, AccessLog was level with heap frames.
        `enter` must not return slots of its caller's buffer: it and
        `run` call each other, so Go moves the buffer to the heap.

- [x] Decide between `run` and the core compiled to Go closures: the
      closures, eval/closures.go, are the only evaluator. With calls out
      of non-tail code made directly, they beat `run` by 14% on AccessLog
      and 10% on Eval, even on fib and the higher-order lib/access-log,
      and recurse 2-2.6x deeper on the same Go stack (~0.6-0.8 KB per
      level against `run`'s ~1.6 KB frame). Allocation is the same.
- [ ] Explore lambda set inference (as in Roc): the checker tracks which
      closures can reach each function type, so calls compile to a switch
      over known closures instead of an indirect call. Likely a lot of work.

## 6. Values are maps and strings

- [x] Intern tags and labels to ids, in a `label.Table` per environment that
      the type registry owns and the runtime shares.
- [x] Records as word buffers in label order, tags as `(id, payload)`
      (SPEC.md section 12, "Compact runtime values").
- [ ] Compile matches to decision trees (Maranget 2008) with the same
      matrix steps as the exhaustiveness check (types/exhaustive.go), so
      each value is tested once instead of per arm. Pays off once tags are
      ids.

## 7. Smaller

- [x] Tail calls, sorely missing: platform run loops recurse once per event
      and grow the stack without bound.
- [x] Functions aren't comparable. Remove their `eq` methods (a
      `ScriptFunc` compares by printing), remove `eval.Equals`, and mark
      `Value` with an unexported `isValue()` method instead of `eq`. Only
      literal patterns compare values, and only primitives.
- [ ] Sources have no file name, so errors in drafts don't say which file.
- [ ] Structure parse and type errors for an LSP as well as the CLI: a
      start and end position (line and column, convertible to LSP's UTF-16
      columns), a severity, related locations (such as "the first arm is
      here"), several diagnostics per scrap instead of bailing at the first,
      and no ANSI colours in the error itself, only in the CLI's rendering
      (token.Error bakes them into Error()).
- [ ] Say which tag a call can't pass. `(| #a -> 1 | #b -> 2) #c` reports
      `cannot unify '[ #a?, #b? ]' with '[ #c, ..a ]'`, which names
      neither the call nor `#c`, and with presence the `?` flags leak an
      inference detail. Better: `cannot pass #c to a function that accepts
      [ #a, #b ]`, pointing at the argument. Needs (1) the missing-label
      error (`errMissingLabel`, from `rewrite` and from a present/absent
      clash) to carry the label and the side lacking it, and (2)
      `context.call` to unify the argument with the domain on its own, so
      it knows the mismatch is the argument's. Absent entries print as
      omitted, so without the label `[ #b? ]` vs `[ #a int ]` is no
      clearer.
- [x] Multi-pattern arms (SPEC.md section 3): an arm has one pattern per
      argument, and a match function collects all its arguments before
      matching any arm, so a later pattern falls through to the next arm.
- [x] Functions with an arity: merge lambda chains `a -> b -> body` into
      one function and frame, compile `f a b` to one call of two arguments
      (eval/apply, Marlow and Peyton Jones 2004), with partial and extra
      arguments handled at runtime, so that a call with all its arguments
      makes no intermediate closures and calls builtins and host functions
      once.
- [ ] Printed types are hard to read. `scrap type < lib/avl.scrap` prints
      the library's record type on one 500-character line, with type
      variables named a to r across fields although each field is
      polymorphic on its own. Options: one field per line once a record
      or enum doesn't fit the width, variables named from `a` per field,
      and syntax highlighting on a terminal (as errors already colour
      only when stdout is one).

## 8. Printing closures

Printed values should stay proportional to the value's size;
`list/fold (y -> y) (acc -> x -> (f ; f = y -> acc y + x)) (list/repeat 300 1)`
prints 189 KB.

- [ ] Name clashing bindings with numbers (`acc`, `acc2`) instead of primes,
      whose length grows with the number of clashes.
- [ ] Merge bindings that print the same, bottom-up: `list/map mk [1, 1, 1]`
      prints `n = 1` three times.
- [x] Resolve each printed piece of code's free names from its own frame,
      not through one map for all of them.
- [ ] Bindings from nested where-groups print as one group, which can make
      a binding recursive that wasn't (`brokenRoundTrips`).

## 9. The language falls short of its spec

Gaps in the language itself, found by writing the libraries; most important
first. 1 and 2 change what the language promises, 5 is quick.

- [ ] Comparisons and equality. SPEC.md lists `== /= < <= > >=`, but `==`
      is a parse error and `1 < 2` fails with `can't infer binary expression
      LT`. Without them each library writes its own `same`, `larger` and
      `not` (lib/parser.scrap, lib/avl.scrap, lib/access-log.scrap), and
      `status > 499` is `int/cmp status 499 |> | #gt -> 1 | _ -> 0`. Choose
      them once inference is done, as `+ - *` are: `==` and `/=` on any
      ground type without functions, the orderings on `int`, `float`,
      `text`, `byte` and `bytes`, all returning `[#false, #true]`. An
      operand that stays a type variable, or holds a function, is a type
      error. No type classes, and types stay erased; generic code still
      takes a `cmp`, as avl does.
- [ ] A well-typed scrap never fails at runtime. Only three builtins still
      can: `list/repeat` and `text/repeat` on a negative count and
      `bytes/to-utf8-text` on invalid UTF-8 (eval/builtins.go). Return an
      option where there is no answer, as `int/div` and `text/to-int` do,
      and an empty result for a negative count; define `round`, `ceil` and
      `floor` on NaN and infinities. Then SPEC.md can promise that a closed,
      well-typed scrap fails only on resources or a host's values, which is
      also the soundness theorem planned in section 12.
- [ ] Finish arithmetic, and settle SPEC.md's contradiction: section 7.1
      asks for separate Int and Float operators, while `+ - *` are chosen
      by type (`1.5 + 2.0` works) and section 14 calls it open. Keep the
      overloading and say so in 7.1.
  - `/`, `%` and `^` are in the grammar but don't scan, so floats can't be
    divided at all. Make `/` float only (IEEE, total), keep `int/div` and
    add a Euclidean `int/mod`, so division by zero can't hide in an
    operator. Drop `^`, or make it float only: `2 ^ -1` has no int.
  - Ints wrap silently: `9223372036854775807 + 1` is
    `-9223372036854775808`. Arbitrary precision with a small-int fast
    path neither wraps nor fails; decide before published scraps rely on
    wrapping.
  - Until they're implemented, drop `/ % ^` from section 2.3.
- [ ] Text has two units: `text/length "héllo"` is 6 (bytes), while
      `text/split "" "héllo"` gives 5 code points, so `pad` in
      lib/access-log.scrap misaligns non-ASCII paths. Count code points
      throughout; bytes are a `bytes/from-utf8-text` away. And settle
      section 4's open text-prefix patterns (`"he" ++ rest`) in favour:
      lib/parser.scrap works on `list text` and hand-writes `strip` for
      lack of them.
- [ ] Field punning. SPEC.md has `{ x }` for `{ x = x }` in literals and
      patterns, but `{ x } ; x = 1` is a parse error. Every library's export
      record is `{ parser = parser, outcome = outcome, … }`.
- [ ] Specify the prelude (section 14 lists it as open). `[#some v, #none]`,
      `[#lt, #eq, #gt]` and `[#true, #false]` are spelled out everywhere:
      add builtin aliases `bool`, `option a`, `result e a` and `order`,
      and `not`, `int/min`, `int/max`, `list/filter`, `list/reverse` and
      `list/range`. Since scraps are closed, the prelude is part of a
      scrap's meaning, which makes it the concrete case for editions
      (section 9.1): changing a builtin changes published scraps.
- [ ] Later: record extension. Spreads only update, so `{ ..{ a = 1 }, b =
      2 }` is a type error; section 7.3 notes that lacks constraints make
      a separate extension construct possible.

## Accepted

- The type registry only grows. Most programs don't keep accumulating types,
  so a long-running platform or REPL can live with it.
