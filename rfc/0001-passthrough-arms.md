# RFC 0001: Passthrough match arms (`?`)

Status: draft

## Summary

Add a match arm written with `?` instead of `|`, which handles the values its
pattern matches and returns every other value unchanged:

```scrap
x |> ? 1 -> 2
```

is sugar for

```scrap
x |> | 1 -> 2 | other -> other
```

## Motivation

A match that rewrites some values and passes the rest through is a common
shape, and today it ends with a catch-all arm that only names its argument
to return it. The repository already has four:

```scrap
-- eval/testdata/cata.scrap
; desugar = | #neg a -> #mul { l = #lit -1, r = a } | other -> other

-- lib/parser.scrap
p input |>
  | #err _ -> one-of ps input
  | ok     -> ok

-- lib/access-log.scrap (and its copy in cmd/example/scrap-terminal)
; busier = a -> b -> int/cmp b.value.requests a.value.requests |>
    | #eq -> text/cmp a.key b.key | o -> o
```

With `?` they read as what they do:

```scrap
; desugar = ? #neg a -> #mul { l = #lit -1, r = a }

p input |> ? #err _ -> one-of ps input

; busier = a -> b -> int/cmp b.value.requests a.value.requests |>
    ? #eq -> text/cmp a.key b.key
```

### Where it helps most

- **Breaking comparator ties.** `int/cmp x y |> ? #eq -> text/cmp a b` says
  "if equal, compare by the next key". Sorting by several keys chains these,
  and the explicit form needs a `| o -> o` per key.
- **Rewriting one case of a variant.** Desugaring and simplification passes
  over a syntax tree (`? #neg a -> …`, `? #add { l = #lit 0, r } -> r`),
  especially as the `ext` step of the extensible-interpreter style in
  `cata.scrap`, where each pass handles a few tags and leaves the rest.
- **Recovering from a failure.** `? #err _ -> fallback` (`one-of` above),
  `? #none -> #some default`: handle the failure case of a result or option
  and keep the success as is, without a dedicated `or-else` per type.
- **Replacing sentinel values.** `? 0 -> 1`, `? "" -> "untitled"`,
  `? [] -> [default]`: clamps and defaults on literals, which always need a
  catch-all arm today (SPEC.md 7.1).
- **Steps in a pipeline.** It ends at pipes like any arm, so a normalising
  step fits between others: `name |> text/trim |> ? "" -> "anonymous" |> greet`.

It does not help where the default isn't the argument itself: a match that
maps the other values to a constant, or wraps them, still writes its
catch-all arm.

## Design

### Grammar

```
body     = binder "->" body
         | arm { arm }
         | parm { parm }                         # passthrough match
         | pipe

parm     = "?" pattern "->" arm-body             # exactly one pattern
```

`?` becomes a punctuation token (section 2.2). A `?` arm follows the rules of
`|` arms: it ends at pipes, its body is an `arm-body` (so a bare match or
lambda there needs parentheses), and a doc comment before it attaches to it.

### Meaning

`? p1 -> e1 ? p2 -> e2` desugars to `| p1 -> e1 | p2 -> e2 | x -> x` with a
fresh `x`. That is the whole definition: there is no typing rule of its own,
and the core (section 5) is unchanged. Consequences:

- **Typing comes from the desugared match.** The arm bodies must have the
  argument's type, so `? 1 -> "one"` fails to unify `text` with `int`.
- **The fallback excludes covered tags** (7.1), as the explicit name arm
  does, so a rewrite removes the tags it fully covers from the result type.
  Today's explicit `desugar` already gets this type, which `?` would keep:
  ```
  [ #mul { l : [ #lit int, ..a ], r : b }, #neg b, ..c ]
    -> [ #mul { l : [ #lit int, ..a ], r : b }, ..c ]
  ```
- **Hashing.** Under the `core` scheme (9.1.1) a `?` match hashes the same as
  its explicit form; under source hashing it differs, like any sugar.

### Restrictions

1. **One pattern per arm.** Passing through is only defined for one
   argument. Since the parser reads `pattern ->` after an arm's first `->` as
   another argument's pattern, `? #a -> x -> x + 1` is a spanned syntax error
   saying `?` arms take one argument; `? #a -> (x -> x + 1)` returns a lambda.
2. **No mixing with `|`.** A match function's arms all start with `|` or all
   with `?`. In a mix, it is unclear where the fallback goes, and a `|`
   catch-all would make it unreachable.
3. **A pattern that can fail.** `? x -> e` and `? _ -> e` leave the fallback
   unreachable and are syntax errors. Patterns that can only fail depending
   on the type, `? { a } -> e` or `? #only x -> e` on a closed type, need
   the checker to find the fallback arm useless after inference. The checker
   does not report useless arms today, so that is a separate change (see
   below); until then these are accepted and behave as their desugaring.

## Arguments against

- **It saves little.** The explicit fallback is `| o -> o`, eight
  characters, and already works after `|>`. There are four uses in the
  repository. A language with few constructs pays for each new one with
  something every reader must learn.
- **It hides a wildcard, and so loses exhaustiveness.** A match without a
  wildcard closes its argument's type, which is how a scrap that type-checks
  cannot raise a match error (7.1). A passthrough always has a wildcard, so
  its argument stays open and a misspelt tag passes everything through
  without a complaint. Checked today with the explicit form:
  ```
  $ echo 'int/div 6 3 |> | #nome -> #some 0 | o -> o' | scrap type
  [ #none, #some int ]
  ```
  Without the fallback, the same typo is a type error. `| o -> o` has the
  same weakness, but you can see it; `?` makes the unchecked match the
  shortest to write, which nudges code away from closed matches.
- **It is control flow you see less of.** Section 6 removed `&&` and `||`
  because short-circuiting is branching and matching is the only control
  flow. `?` stays a match, but its second branch is not written anywhere.
- **`?` meant something else.** In the reference implementation
  (tekknolagi/scrapscript), `value ? cond` was an inline assertion. Readers
  coming from there may misread it. Section 6 already says to write
  assertions as a named function, so the symbol is free, but the history is
  there.
- **Two ways to write the same match.** Formatting, linting and review must
  decide which to prefer, and converting between the forms when an arm is
  added (a second argument, a non-identity default) is churn.

## Alternatives

- **Do nothing.** Keep writing `| o -> o`. Revisit if yard code shows the
  shape is much more common than four uses suggest.
- **Bare `pattern -> body`.** `x |> 1 -> 2` is free syntax today, since a
  binder is only a NAME or `_`. Rejected: it claims the syntax that
  destructuring lambdas (`{ x, y } -> x + y`) would want, and its meaning
  would depend on whether the pattern can fail.
- **A passthrough arm `| ..`.** `x |> | 1 -> 2 | ..`, echoing
  `{ ..r, k = v }`'s "the rest unchanged". It keeps `|` for every arm, so
  arms mix freely and the default is visible at the end. It is longer than
  `?`, and `..` already means a rest inside patterns, so `| ..` and
  `| { .. }` would sit close together.
- **Library functions.** A function cannot take a pattern as an argument,
  so a library can cover the shape only in pieces:
  - *One combinator per type*, for the cases that recur:
    ```scrap
    ; then    = next    -> | #eq -> next () | o -> o        -- orderings
    ; or-else = recover -> | #err e -> recover e | ok -> ok  -- results
    ```
    `int/cmp a b |> then (_ -> text/cmp x y)` and
    `p input |> or-else (_ -> one-of ps input)` read well and keep their
    precise types. This covers the comparator and recovery uses, and is
    worth doing whether or not `?` lands. The cost is a function per type,
    and a thunk where the next value should not be computed early.
  - *A generic passthrough* over a partial function returning an option:
    ```scrap
    ; passthrough = f -> x -> f x |> | #some y -> y | #none -> x
    desugar = passthrough (| #neg a -> #some (#mul { l = #lit -1, r = a }) | _ -> #none)
    ```
    It still writes a catch-all arm, now `| _ -> #none`, so it saves
    nothing. It is also less precise: the result keeps `#neg`, because
    `x` is no longer a name pattern that excludes the covered tags.
  - *Replacing a value*, `replace 0 1`, would be
    `old -> new -> x -> x == old |> | #true -> new | #false -> x`, but `==`
    is not implemented yet (TODO.md, section 9). Once it is, this covers
    sentinels on types with equality, though not patterns such as
    `#some 0` or `[]` bound to a name.

  Taken together, the per-type combinators cover the common uses, and what
  is left for `?` is rewriting a few tags in a tree pass.

## Spec changes

- 2.2: add `?` to the punctuation.
- 3: add `parm` to `body`, and `?` arms to the note that arms end at pipes.
- 5: add the desugaring of `? p -> e` to the table.
- 6: remove `?` from the row of removed operators, leaving `!`.

## Implementation

In two changes, one at a time:

1. Scanner, parser, CST and printer for `?` matches with restrictions 1, 2
   and the syntactic half of 3; lowering adds the fallback arm.
2. A usefulness check for match arms after inference, which rejects a `?`
   whose fallback is useless (the type-dependent half of 3). The same check
   could report redundant `|` arms, and an arm whose tag is absent from a
   closed argument type, which catches misspelt tags where the type is
   known.
