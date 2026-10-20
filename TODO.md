# TODO

Architectural blemishes, most important first. Items 1–4 are one piece of
work: lower the AST to a core IR after type checking, and evaluate that.
Track the effect on performance in [BENCHMARK.md](BENCHMARK.md).

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

- [ ] Carry the checker's types into the IR. Next up: it is evaluator
      independent, and decides closures against `run` (section 5). The
      compiler reads them to choose ops, so the core keeps no types; next,
      static field indices where a record's type is closed.
- [x] Choose operators (`+ - *` on int or float; `+< >+` on lists or
      bytes; `++` on lists, text or bytes) statically instead of by the
      runtime value.
- [ ] Use them to specialise functions per record shape (SPEC.md section 12).

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

      What costs now is per-node overhead: `run` has a ~1.5 KB Go frame
      and is called for every subexpression. Reading slot operands
      without calling `run` saved 6%.
- [ ] Decide between `run` and the core compiled to Go closures
      (eval/closures.go, built with `-tags closures`). Untyped, closures
      beat `run` by 4% on AccessLog: dispatch was not the cost, decoding
      each lambda on every call was, which a compiled table of arms per
      lambda removes. fib is still 9% slower (`apply`/`enter` decode the
      lambda; a non-tail call takes two hops). Next: types in core
      (section 3), then specialise in closures only (int arithmetic,
      access on a known shape, jump tables on closed enums) and compare.
      Without the tag, `run` is as fast as before the prototype.
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
- [ ] Functions aren't comparable. Remove their `eq` methods (a
      `ScriptFunc` compares by printing), remove `eval.Equals`, and mark
      `Value` with an unexported `value()` method instead of `eq`. Only
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

## Accepted

- The type registry only grows. Most programs don't keep accumulating types,
  so a long-running platform or REPL can live with it.
