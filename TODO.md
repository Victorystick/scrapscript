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

- [ ] Carry the checker's types into the IR.
- [ ] Choose `++` (list, text or bytes) statically instead of by the runtime
      value.
- [ ] Use them to specialise functions per record shape (SPEC.md section 12).

## 4. Errors everywhere after type checking

`(Value, error)` dates from before the checker existed. The evaluator now
returns plain values and panics with a `runtimeError`, which `catch` turns
back into an error at its edge; builtins still return errors.

- [ ] Drop the runtime type checks (`runAs`, the operand type switches in
      `run`) that the checker already guarantees.
- [ ] Builtins return plain values and panic like the evaluator.
- [x] Match without panic and recover: return a bool and write into the
      frame.
- [ ] Check exhaustiveness for all patterns, not only enums (SPEC.md
      section 7.1); then a well-typed scrap never reaches "no arm matches".

## 5. Closures keep their whole scope chain

- [ ] Capture only the slots a closure uses, not the `*context` chain, while
      still printing with its captured bindings.
- [ ] Explore lambda set inference (as in Roc): the checker tracks which
      closures can reach each function type, so calls compile to a switch
      over known closures instead of an indirect call. Likely a lot of work.

## 6. Values are maps and strings

- [ ] Intern tags and labels to global ids, shared with the type registry.
- [ ] Records as word buffers in label order, tags as `(id, payload)`
      (SPEC.md section 12, "Compact runtime values").

## 7. Smaller

- [ ] Tail calls, sorely missing: platform run loops recurse once per event
      and grow the stack without bound.
- [ ] Sources have no file name, so errors in drafts don't say which file.

## 8. Printing closures

Known issue: the printer maps each free Ident node to one binding, so
closures made by the same lambda in different frames all refer to the last
frame's binding. A printed value can then mean something else, even call
itself forever: `list/fold (y -> y) (acc -> x -> (y -> acc y + x)) [1, 2, 3]`
prints `acc = y -> acc y + x''`. The sources are in `brokenRoundTrips`.

Printed values should also stay proportional to the value's size; a 300-step
version of that fold prints 189 KB.

- [ ] Name clashing bindings with numbers (`acc`, `acc2`) instead of primes,
      whose length grows with the number of clashes.
- [ ] Merge bindings that print the same, bottom-up: `list/map mk [1, 1, 1]`
      prints `n = 1` three times.
- [ ] Resolve each printed piece of code's free names from its own frame,
      not through one map for all of them.

## Accepted

- The type registry only grows. Most programs don't keep accumulating types,
  so a long-running platform or REPL can live with it.
