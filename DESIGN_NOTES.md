# Design notes

These are decisions from design discussions that aren't in SPEC.md yet, and
the questions deferred. Once a decision is written into the spec, the spec
decides it and the note here goes. The language's goals and principles are at
the top of SPEC.md.

## Surface additions

Each is to be specified in SPEC.md and implemented in a change of its own,
with the tutorial.

- **Keywords.** A construct gets a keyword only if it can't be a function.
  Keywords are reserved only in expressions: labels stay free (record fields,
  `.then`, type records such as `{ then : text -> reply }`). Punning makes a
  label a variable, so `{ then }` in a literal, pattern or destructuring
  binding must be written `{ then = t }`, and pattern binders can't be
  keywords. `eval/testdata/expr.scrap` defines a function `then`, to rename.
- **`and` and `or`** short-circuit, and desugar to `Match` on `#true` /
  `#false`. `not` stays a function.
- **`if c then a else b`**, with `else` required. Multi-way conditionals use
  `else if` chains. It desugars to `Match`.
- **Guards** use `if` at the end of an arm, after all its patterns:

  ```
  ; strip : list text -> list text -> [#some (list text), #none] =
      | []      -> rest               -> #some rest
      | c >+ cs -> d >+ ds if same c d -> strip cs ds
      | _       -> _                  -> #none
  ```

  A pattern-less arm, `| if c -> e`, is allowed in a match that has columns,
  and stands for `_` in every column. There is no zero-column conditional.
  A guarded arm doesn't count towards exhaustiveness. A guard or an `if`
  condition must parenthesise any lambda in it. Guards need the core's
  `Match` to fall through to the next arm when a guard fails, which touches
  `opMatch`, so benchmark it. Decision trees for many arms are a later
  optimisation.
- **Unary minus.** A `-` directly attached to an atom or field access negates
  it: `-x`, `-r.a`, `-(a + b)`, and `f -x` means `f (-x)`, as `f -1` does. A
  negated operand directly before `^` is a syntax error: write `(-x) ^ 2` or
  `-(x ^ 2)`, as in JavaScript. Negation is int-only until floats are
  decided.

## Experiments

- **A policy host.** A server holds many policies, `map[PolicyName]*Scrap`,
  each a function from an open record of inputs, `{ plan : …, ..r }`, to a
  well-known closed output type. Unlike a platform (SPEC section 13), which
  checks a script against a closed interface and is mostly a binary per
  interface, a policy host is a library in an existing server, and reads
  what each policy demands from its inferred input type.
  - **A universe type.** The host declares a closed type of every input it
    can fetch, and checks each policy's input type against it when
    loading. An open row accepts any field, so without it a typo such as
    `trail-ended` would be a field that the policy demands and nothing
    fetches.
  - **Type-directed fetching.** The inputs that a set of policies needs is
    the union of their input rows, merged through nested records. A field
    that two policies use at different types is a conflict, reported when
    loading. The type gives an upper bound: a field read in only one
    branch is still fetched. The checker infers these rows already; the
    API would have to expose the fields that an inferred type uses.
  - **A/B testing by hash.** A rollout chooses between policies by hash,
    and each decision can be logged with the hash that made it and
    replayed exactly.
  - Concurrency: TODO.md section 7.
- **Compiling scraps to Go.** If policies ship as server code, compiling a
  scrap to Go source would remove the interpreter, which a company may push
  back on embedding, while the generated code still records the scrap's
  hash. Today `gogen` generates only bindings.

## Diagnostics

- When an unbound NAME such as `a-b` splits into bound parts, suggest
  `a - b`. Suggest close names for typos too, like `list/mpa`.
- Error recovery in the parser isn't required. Revisit it if an editor or a
  language server needs a tree for broken code.

## Deferred

- **Reliability (SPEC principle 5):**
  - Resource limits. Proposal: where evaluation finishes, the result is the
    same everywhere, and running out of time, stack or memory is reported as
    exactly that.
  - Integer overflow: wrap (as now), fail at runtime, or use arbitrary
    precision. Measure before choosing. COMPILE_PLAN.md's printer argument
    relies on `+` wrapping, and so associating.
  - Whether the spec should list every runtime failure (holes, division by
    zero, perhaps overflow, resource limits, platform failures).
- **"Libraries are never inlined"** (section 10, rule 6). This relates to
  self-contained values, which inlining dependencies would give.
- **Floats:**
  - separate operators or named functions, given that erased types rule out
    overloading `+`;
  - float negation;
  - requiring each operation to be rounded on its own. Go may fuse
    `a * b + c` on arm64, ppc64 and s390x, so the implementation must
    prevent that with explicit `float64(…)` conversions.
- Precedence of `and` and `or`, against comparisons and pipes, and how far
  `if … else e` extends: whether `if c then a else b |> f` pipes only `b`,
  as a lambda's body extends to the right.
- How exactly guards change the core's `Match`.
- Hashing: whether published source must be canonically formatted. Leave it
  until identity moves to a core format, if it ever does.
- A store protocol, as an optional substandard outside the language spec.
