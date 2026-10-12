# TODO

Architectural blemishes, most important first. Items 1–4 are one piece of
work: lower the AST to a core IR after type checking, and evaluate that.
Track the effect on performance in [BENCHMARK.md](BENCHMARK.md).

## 1. No core IR

SPEC.md section 5 describes a desugared core, but the checker, the evaluator
and the printer each walk the surface AST, handling pipes, composition,
multi-pattern arms, where-groups and pattern operators separately.

- [ ] Define the core IR, with each node pointing back to its AST node for
      errors and for printing closures.
- [ ] Lower the AST to it after type checking.
- [ ] Evaluate the core instead of the AST.
- [ ] Later: define the canonical encoding and the `core` hash scheme on it
      (SPEC.md section 9.1.1).

## 2. Eval re-reads source text on the hot path

- [ ] Resolve variables to frame slots (depth, index) instead of looking up
      names, which re-scans each token (`TokenText`) and allocates a string.
- [ ] Frames as `[]Value` instead of a `Variables` map per call and per match
      arm.
- [ ] Decode literals once, not on every evaluation.

## 3. Types are dropped before evaluation

- [ ] Carry the checker's types into the IR.
- [ ] Choose `++` (list, text or bytes) statically instead of by the runtime
      value.
- [ ] Use them to specialise functions per record shape (SPEC.md section 12).

## 4. Errors everywhere after type checking

- [ ] Drop the runtime type checks (`c.int`, `c.list`, …) that the checker
      already guarantees.
- [ ] Keep errors only for real runtime failures: unmatched TEXT, INT or
      BYTES patterns, and builtins.
- [ ] Match without panic and recover: return a bool and write into the
      frame.

## 5. Closures keep their whole scope chain

- [ ] Capture only the slots a closure uses, not the `*context` chain, while
      still printing with its captured bindings.

## 6. Values are maps and strings

- [ ] Intern tags and labels to global ids, shared with the type registry.
- [ ] Records as word buffers in label order, tags as `(id, payload)`
      (SPEC.md section 12, "Compact runtime values").

## 7. Smaller

- [ ] Tail calls: platform run loops recurse once per event and grow the
      stack without bound.
- [ ] The type registry only grows; a long-running platform or REPL never
      frees type nodes.
- [ ] Sources have no file name, so errors in drafts don't say which file.
