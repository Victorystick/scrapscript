# Design notes

These are decisions from design discussions that aren't in SPEC.md yet.
Once one is written into the spec, the spec decides it and the note here can
go. "Deferred" items are open questions to come back to.

## Goals

This project explores the ideas behind Scrapscript. It doesn't implement the
guide at scrapscript.org, and it departs from the guide wherever doing so
serves the ideas better.

The core ideas, each depending on the one before:

1. **Closed scraps.** There are no free names and no ambient state, so a
   scrap means the same thing everywhere and at any time. This is the main
   reason to depart from scrap maps and "latest version". With those, the
   meaning of a name depends on when you look it up. A hash fixes the
   meaning when you write it.
2. **Content addressing.** A hash identifies a scrap and pins its meaning.
   Closure is what lets it do so: a hash of code with free names would pin
   the text but not the meaning.
3. **Values travel.** A result prints as a scrap that can be evaluated
   again, published or sent elsewhere.

Consequences:

- **Platforms follow from closure.** Effects can't be ambient, so a host
  supplies them through an interface.
- **Erased types follow from travel.** A value that travels is plain data,
  so types can't affect evaluation. This also keeps code generation
  simple.

There are no compatibility promises before the first edition. Breaking
changes are fine for now.

## Editions

- A scrap declares its edition in its source with a year-based keyword,
  e.g. `edition 2027`. It comes first in the file, before anything except
  comments. The declaration's own syntax never changes, since it has to be
  read before the edition is known.
- An edition pins the grammar, the primitives and the prelude, so a hash
  always means one thing.
- Drafts may leave the declaration out. Drafts have no identity, so this
  can't change what any hash means.
- Publishing rejects a draft that has no edition. It doesn't insert one, so
  publishing still changes only path tokens.
- Printed output includes its edition line, so that it can be published as
  is.
- The status line of SPEC.md should say that there are no compatibility
  promises before the first edition.

## Scope

- The spec covers the language only. Stores and transport belong to
  tooling. A store protocol could become an optional substandard later.
- By default, values travel small, as scraps whose dependencies are hash
  references, and stores supply those dependencies.

## The core (SPEC principle 1, section 5)

- The core is a semantic device. A small core is easier to optimise and to
  verify. If it can later serve other purposes too, such as an interchange
  format, that's a bonus.
- "Small" counts semantic forms, not node kinds. Implementations may
  represent and compile the core however they like, as long as no scrap can
  tell the difference.
- Section 5 should be reworded along these lines. Drop "the canonical
  encoding is defined on them" and "or dedicated primitive nodes (decide
  once)". Present de Bruijn indices and sorted fields as saying that binder
  names and field order don't affect meaning, not as requirements.
- Identity moves out of principle 1. "Content-addressed, and the hash pins
  the meaning" goes with the core ideas, and the choice of source hashing
  stays in section 9.

## Syntax (SPEC principles 2 and 4)

- "One way to write each thing" becomes "every feature earns its place":
  sugar exists only where it reads clearly better than the core form.
- No user-defined operators. The edition alone fixes the grammar, so a scrap
  parses without loading its dependencies.
- Keywords are allowed, but only for constructs that can't be functions,
  such as short-circuiting or edition declarations. They are reserved only
  in expressions, so a record label like `{ and = 1 }` is still allowed.
- `and` and `or` are short-circuiting keywords. `not` stays a function.
- `if c then a else b`, with `else` required. Multi-way conditionals use
  `else if` chains.
- Guards use `if` at the end of an arm, after all its patterns:

  ```
  ; strip : list text -> list text -> [#some (list text), #none] =
      | []      -> rest               -> #some rest
      | c >+ cs -> d >+ ds if same c d -> strip cs ds
      | _       -> _                  -> #none
  ```

  A pattern-less arm, `| if c -> e`, is allowed in a match that has
  columns, and stands for `_` in every column. There is no zero-column
  conditional. A guarded arm doesn't count towards exhaustiveness. A guard
  or an `if` condition must parenthesise any lambda it contains.
- Matching stays the only branching in the core. `and`, `or` and `if`
  desugar to `Match` on `#true` / `#false`. Guards need the core's `Match`
  to fall through to the next arm when a guard fails. Matches already
  compile to an in-frame `opMatch`. Decision trees for many arms are a later
  optimisation.
- Tags stay structural. That serves travel, because a variant needs no type
  declaration in scope to mean the same thing.
- Both directions of pipe and composition stay: `|>` / `<|` and `>>` /
  `<<`.
- Layout carries no meaning. Indentation and newlines don't matter (unlike
  Python), so one-liners, pipes and pasted code always work.
- Kebab-case and slash names stay, and with them the operator-spacing rule.
  When an unbound NAME such as `a-b` splits into bound parts, the error
  should suggest `a - b`.
- Unary minus is added. A `-` directly attached to an atom or field access
  negates it: `-x`, `-r.a`, `-(a + b)`, and `f -x` means `f (-x)`, as `f -1`
  does. A negated operand directly before `^` is a syntax error: write
  `(-x) ^ 2` or `-(x ^ 2)`, as in JavaScript. Negation is int-only until
  floats are decided.

## Errors (SPEC principle 3)

- The language-level principle: how a scrap parses depends only on its own
  text and its edition. "Total, with spanned errors" is a requirement on
  implementations.
- Parsing reports at least the first error, with a span. Error recovery
  isn't required. Revisit it if an editor or language server needs a tree
  for broken code.
- Type checking may report several errors, since errors in independent
  bindings don't cascade.
- Errors from parsing, loading and checking all carry spans. A span names
  its scrap (a hash or path) and a byte range.
- Spans on runtime errors are encouraged but not required.

## Deferred

- **Reliability (SPEC principle 5):**
  - Resource limits. Proposal: where evaluation finishes, the result is the
    same everywhere, and running out of time, stack or memory is reported as
    exactly that.
  - Integer overflow: wrap (as now), fail at runtime, or use arbitrary
    precision. Measure before choosing.
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
- Precedence of `and` and `or`, against comparisons and pipes.
- How exactly guards change the core's `Match`.
- Hashing: whether published source must be canonically formatted. Leave it
  until identity moves to a core format, if it ever does.
