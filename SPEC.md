# Scrapscript specification (draft)

Status: design draft. It specifies the language this implementation is moving
towards, and does not describe it yet. It covers the surface grammar and its
desugaring into a small core, types, scrap identity, drafts and publishing, and
platforms, and replaces the precedence table of the reference interpreter with an
explicit grammar. Section 14 relates it to the guide at scrapscript.org. Types
(section 7) and platforms (section 13) are unfinished.

Principles:

1. The **core language** is small (section 5). The surface syntax is a pure
   desugaring into it. For now a scrap's identity is a hash of its source bytes
   (section 9); a canonical-core scheme can be added later without changing the
   surface language.
2. One way to write each thing. No user-defined operators.
3. Every parse failure is a spanned syntax error. The parser is total.
4. Whitespace-insensitive layout, except the operator-spacing rule (2.3).
5. **Reliability first.** Meaning never depends on ambient state: a closed
   scrap evaluates the same everywhere. No implicit network, filesystem or
   clock access. Failures are reported at parse, load or check time, not at
   runtime, wherever the type system can rule them out (section 10).

## 1. Notation

EBNF: `{ x }` zero or more, `[ x ]` optional, `a | b` alternative,
`"tok"` literal token, UPPERCASE a lexical token class.

## 2. Lexical grammar

### 2.1 Tokens

```
INT      = digit { digit }                       # arbitrary precision
FLOAT    = digit { digit } "." digit { digit }   # digits on both sides
TEXT     = '"' { char | escape } '"'             # escapes: \" \\ \n \t \u{HEX}
BYTE     = "~" hex hex                           # one byte: ~FF
BYTES    = "~~" { base64 }                       # padded base64: ~~aGVsbG8=
HASH     = "$" scheme "~~" { hex }               # scheme names how the target is hashed, e.g. $src-sha256~~e4ca…
PATH     = "$" TEXT                              # a draft's reference to a file (11.2): $"./terminal/program.scrap"
NAME     = ident-start { ident-char }            # kebab-case and slashes allowed, see 2.3
TAG      = "#" ( letter | digit ) { ident-char }  # #true  #2d
COMMENT  = "--" ( " " | "-" | EOL ) { any } EOL  # line comment
DOC      = "---" ( " " | EOL ) { any } EOL       # doc comment
```

Comments:

- Line comments only. No block comments: they nest badly, interact with TEXT
  literals, and make "comment out this code" unreliable.
- `--` must be followed by a space, `-` or end of line. `--x` is a lexical
  error (almost always a typo for `- -x` or a missing space), never a silent
  comment.
- `---` followed by a space or end of line is a doc comment. The CST attaches
  it to the next binding or match arm for tooling. A run of four or more
  dashes (`----`) is a plain comment, so separator lines work.
- Comments are trivia: they exist in the CST only and never in the core. Under
  source hashing (section 9) they are part of the source bytes, so they do
  affect a scrap's hash.

- A BYTES literal consumes the maximal run of the base64 alphabet
  (`A-Z a-z 0-9 + / =`), so `(~~aGk=)` ends the literal at `)`. Its length must
  be a multiple of four, padded with `=`. `~~` alone is the empty BYTES.
- A BYTE is exactly two hex digits. A BYTE has type `byte`, and BYTES is a
  sequence of them: `bytes +< ~21` and `~00 >+ bytes` append and prepend one.
- `~~` means "encoded bytes follow", and what precedes it picks the encoding:
  base64 after a bare `~~` (BYTES), hex after `$scheme~~` (HASH). Digests are
  hex because that is how `sha256sum`, git and most tools print them, so a hash
  literal can be compared with their output directly.
- **Identifiers are ASCII.** `letter` is `A-Z` or `a-z` and `digit` is `0-9`.
  `ident-start` = letter | `_`; `ident-char` = letter | digit | `_` | `'` | `-` | `/`
  (`-` and `/` only when both neighbours are ident-chars, so `list/map` is one
  NAME). By convention `/` separates a namespace from a name. This covers NAMEs, record
  field labels and TAGs. Any other non-ASCII character outside a TEXT literal or a
  comment is a lexical error. TEXT and comments may contain any Unicode. The aim is
  to rule out confusable and normalisation-dependent names in code that travels;
  revisit later if needed.
- `_` alone is the wildcard, not a NAME. It matches anything and binds nothing,
  and cannot be referenced. It is valid in patterns (including the extra patterns
  of a multi-pattern arm, `| #false -> _ -> #false`) and as the binder of a plain
  function (`_ -> 1`).
- Keywords: none. Everything is punctuation or a NAME.

### 2.2 Punctuation

```
( ) [ ] { }  ,  ;  :  =  =>  ->  |  ..  .
```

### 2.3 Operators and the spacing rule

Binary operators are the following tokens:

```
|>  >>  ==  /=  <  <=  >  >=  ++  >+  +<  +  -  *  /  %  ^
```

**A binary operator must have whitespace (or a delimiter) on both sides.**
Consequences, all intended:

| Input | Meaning |
|---|---|
| `a - b` | subtraction |
| `a -b` | lexical error (`-b` is neither a literal nor a binary use) |
| `a-b` | one NAME (kebab-case) |
| `a / b` | division |
| `a/b` | one NAME (`list/map`) |
| `-1` | negative INT literal (sign is part of the token when adjacent to digits) |
| `f -1` | application `f (-1)` |
| `f - 1` | subtraction |
| `1--2` | lexical error |
| `x --y` | comment is NOT started (needs space after `--`); lexical error |

`-` followed immediately by a digit, with whitespace or an opener before it,
lexes as part of a numeric literal. `-x` (minus then a name) is not valid;
write `0 - x` or call `neg x`. There is no unary minus operator.

## 3. Expressions

```
program  = expr EOF

expr     = body { ";" binding }                  # where-clause: loosest form
binding  = NAME [ ":" type ] "=" rhs             # one binding form
rhs      = type                                  # sort binding (section 7.2)
         | body                                  # value binding

body     = binder "->" body                      # function
         | arm { arm }                           # match function
         | pipe

arm      = "|" pattern { "->" pattern } "->" arm-body    # one pattern per argument
arm-body = cmp                                   # NOT a bare match or a bare lambda: parenthesise

binder   = NAME | "_"                            # "_" ignores the argument
pipe     = comp { "|>" comp } [ "|>" body ]      # left-assoc; the last operand may be a function
comp     = cmp { ">>" cmp }                      # left-assoc: f >> g is x -> g (f x)
cmp      = listop [ cmp-op listop ]              # non-associative
cmp-op   = "==" | "/=" | "<" | "<=" | ">" | ">="
listop   = cat ( ">+" listop | { "+<" cat } )    # cons right-assoc; snoc left-assoc
cat      = add { "++" add }                      # left-assoc, text only
add      = mul { ( "+" | "-" ) mul }             # left-assoc
mul      = pow { ( "*" | "/" | "%" ) pow }       # left-assoc
pow      = app [ "^" pow ]                       # right-assoc
app      = variant                               # tag head: takes at most one payload
         | postfix { postfix }                   # juxtaposition, left-assoc
variant  = TAG [ postfix ]                       # payload only if next token starts an atom
postfix  = atom { "." NAME }                     # field access, no whitespace before "."

atom     = INT | FLOAT | TEXT | BYTE | BYTES | HASH | PATH
         | NAME
         | TAG                                   # nullary tag (argument position, list items)
         | "(" [ expr ] ")"                      # "()" is the hole
         | "[" [ expr { "," expr } ] "]"
         | "{" [ field { "," field } ] "}"
field    = NAME [ "=" expr ] | ".." atom         # `{ x }` is `{ x = x }`; at most one spread, first; the other keys update it
```

Notes:

- `;` binds loosest. A `;`-group scope covers the whole `body` to its left.
  Inside a match arm or function body use parentheses to scope a where-clause.
- A `;` group is **mutually recursive** (section 8).
- `postfix` requires `.` to be immediately preceded by the atom and followed
  by a NAME, so it never conflicts with FLOAT (digit before and after).
- **Tag payloads.** A tag at the head of an application takes the next
  `postfix` as its payload (`#some 1`, `#some r.a`, `#some (g x)`); a tag anywhere
  else is nullary. So `cond #true (g x)` passes a nullary tag and `#some (#some 1)`
  needs its parentheses. A variant cannot be applied: `#a 1 2` is a syntax error.
  Patterns have no application, so `TAG [ pat-atom ]` there is unambiguous.
- Commas are separators, not operators. Trailing commas are an error
  (revisit).
- Comparisons are non-associative: `a < b < c` is a syntax error.
- **Arms end at pipes.** An arm's body stops before `|>` and `>>`, so a pipe
  after a match function applies the whole function, and the function can be
  piped into: `x |> | 0 -> "zero" | _ -> "other"`. The last operand of a
  pipe may be a function, which extends to the right: `3 |> a -> a + 1`.
- `listop` mixing: `a >+ b +< c` parses as `a >+ (b +< c)`.
- **Multi-pattern arms.** Each pattern after the first `->` matches the next
  argument, so a match function takes as many arguments as its arms have patterns:
  ```
  both = | #true  -> #true -> #true
         | _      -> _     -> #false
  ```
  After each `->` the parser tries a pattern; if a pattern parses and the next
  token is `->`, it is another argument pattern; otherwise the parser rewinds
  and parses the arm body. All arms of one match must have the same number of
  patterns (spanned syntax error otherwise). Since an extra `NAME ->` is now a
  pattern, an arm that returns a lambda writes it in parentheses:
  `| 0 -> (x -> x + 1)`.
- **Bindings.** An annotation, when present, sits directly before the `=`; there
  are no separate annotation lines. The right-hand side is parsed with the type
  grammar when the annotation's final result is `type`, or when it starts with
  `NAME "=>"`; otherwise as a `body`. Both decisions look at tokens of the
  binding itself, never at other bindings.

## 4. Patterns

Patterns are their own nonterminal, parsed directly (never an expression that is
validated afterwards).

```
pattern  = pat-atom

pat-atom = INT | TEXT | BYTE | BYTES             # no FLOAT patterns
         | NAME                                  # binds
         | "_"                                   # wildcard
         | TAG [ pat-atom ]                      # #true   #some x   #pair (a)
         | "(" [ pattern ] ")"                   # "()" matches the hole
         | "[" [ pattern { "," pattern } [ "," ".." [ NAME ] ] ] "]"
         | "{" [ prest | [ prest "," ] pfield { "," pfield } ] "}"
prest    = ".." [ NAME ]                         # the other fields; first, as in a record literal
pfield   = NAME [ "=" pattern ]                  # `{ x }` is `{ x = x }`, binding x
```

Open: text-prefix patterns (`"he" ++ rest`). Either add
`TEXT "++" NAME` to `pat-atom` or reject. Do not parse it and fail later.

## 5. Core language

The surface desugars to these nodes. The canonical encoding (section 9) is
defined on them.

```
Int Float Text Bytes Hole
Var(index)                       -- de Bruijn, no names
List(items)       Record(sorted fields)       Variant(tag, payload)
Fn(body)          Match(arms)    Apply(f, x)
Let(group, body)                 Field(record, name)
Hash(scheme, digest)             -- reference to another scrap; scheme says how it is hashed
```

Desugaring:

| Surface | Core |
|---|---|
| `a \|> f` | `Apply(f, a)` |
| `f >> g` | `Fn(Apply(g, Apply(f, Var0)))` (no generated names) |
| `a + b` etc. | `Apply(Apply(Var(add), a), b)` against the platform prelude, or dedicated primitive nodes (decide once) |
| `a >+ xs` / `xs +< a` | cons / snoc primitives |
| `#tag` | `Variant(tag, Hole)` |
| `{ ..r, k = v }` | record update primitive |
| `e ; x = v` | `Let` group |
| `\| p1 -> p2 -> e` (n patterns) | n-ary `Fn` over a unary `Match` on an internal record `{ 0 = a1, 1 = a2 }`, so first-match-wins and exhaustiveness work across all columns, and the core is unchanged |

## 6. Removed from the reference design

| Removed | Reason |
|---|---|
| `<\|`, `<<` | second spelling of `\|>`, `>>` |
| `//`, `>*` | `//` was unevaluated; `>*` had no semantics. Use named functions. |
| `!`, `?` | no specified meaning. If wanted, name them (`assert`). |
| `@` | replaced by `.field` |
| `::` | no use; `=>` is kept, as the type-level lambda (section 7.2) |
| unary minus operator | negative literals only |
| `&&`, `\|\|` | short-circuiting is branching, and matching is the only control flow. Combine booleans with a match on `#true` / `#false`. |
| `,` / `..` as binary operators | separators / pattern-and-record syntax only |

## 7. Variants and types

### 7.1 Decided

- Variants and records are **structural**. There are no type declarations.
- Tag expressions are open: `#a : [#a, ..r]`, and `#a e` with `e : T` has type
  `[#a T, ..r]`.
- A match **without** a wildcard arm closes its argument type to exactly the tags
  it lists, so it is exhaustive by construction. A wildcard arm leaves the
  argument type open. Exhaustiveness is a typing rule: a scrap that type-checks
  cannot raise a match error. INT, BYTE, TEXT and BYTES patterns always need a
  catch-all arm.
- Records and variants share one row mechanism (7.3).
- Int and Float are separate types with no overloading. Type classes are
  deferred; until then arithmetic uses separate Int and Float operators.
- Type aliases are naming only and are erased from the core. (Under source
  hashing they are still part of a scrap's source, so they affect its hash.)
- Printed types keep the alias names they were written with, and bind them in a
  trailing where-clause, instead of inlining each alias wherever it occurs:
  ```
  [ #first io, #second io ] -> io
  ; io = [ #err text, #ok ]
  ```
  This is display only: aliases never affect unification. Open: which name to
  print when types written with different aliases unify, and whether the
  where-clause should read back as a type scrap (section 10, rule 6).
- Tag payload arity is positional (section 3, "Tag payloads").
- Recursive aliases are allowed when every cycle passes through a tag payload,
  and are equi-recursive (7.5).

### 7.2 Type grammar

Type names are lowercase: `int float text byte bytes list`, plus `()` for the hole
type. Types and values share one namespace. A name bound by a sort binding
(below) may appear only in type positions, and sort bindings are erased from the
core. The one exception is a scrap's body, which may name a type to export it
(7.6).

Type variables are introduced explicitly with `=>`; there is no implicit
quantification. A name in a type is a variable bound by an enclosing `=>`, a
builtin, or a sort binding in scope; anything else is an error.

```
type     = NAME "=>" type                        # prenex quantification / type lambda
         | arrow
arrow    = tapp [ "->" arrow ]                   # right-assoc
tapp     = tatom { tatom }                       # type application: list int
tatom    = NAME | "()" | "(" type ")"
         | "{" [ tfield { "," tfield } ] [ "," ".." NAME ] "}"   # record row
         | "[" [ ttag   { "," ttag   } ] [ "," ".." NAME ] "]"   # tag-union row
tfield   = NAME ":" type
ttag     = TAG [ tatom ]
```

A row tail may stand alone: `{ ..r }`, `[ ..r ]`. Examples:

```
id        : a => a -> a
get-a     : t => r => { a : t, ..r } -> t
area      : [#circle float, #rect { w : float, h : float }] -> float
is-some   : a => r => [#some a, ..r] -> [#true, #false]
```

### Sorts, kinds and sort bindings

`type` is a **sort**, not a type of values. It appears only in binding
annotations, and `type : type` is not a thing. `type` is the kind of types and
`type -> type` the kind of a type constructor; builtins are `int : type` and
`list : type -> type`. In a kind, `->` is an ordinary arrow in the type grammar.
`row` is the kind of a row (7.3): a parameter used as a row tail has kind `row`,
and its argument must be a row variable.

```
has-a : type -> row -> type = a => r => { a : a, ..r }
```

A binding whose annotation's final result is `type` is a **sort binding**. Its
right-hand side is parsed with the type grammar. A type constructor is a
type-level lambda, `s => T`, one parameter per `=>`:

```
event  : type                  = [#line text, #eof]
turn   : type -> type          = s => { state : s, print : list text, next : [#read-line, #exit int] }
either : type -> type -> type  = a => b => [#left a, #right b]
```

The annotation is optional when the right-hand side starts with `NAME "=>"`,
because `=>` occurs only in type syntax:

```
option = a => [#some a, #none]          # kind inferred: type -> type
```

Kind inference is first-order unification over `type`, `row` and `k -> k`; an
unconstrained kind defaults to `type`. Applications are kind-checked: `turn int`
is fine, `turn int int` and `list list` are errors.

Right-hand sides that share syntax with value expressions (`[#line text, #eof]`,
`list int`, `()`, `{}`) need the annotation. Without it,
`event = [#line text, #eof]` is a value: a list of two variants.

### 7.3 Rows

A row is a finite map from labels to types, plus a tail that is either empty
(closed) or a row variable `..r` (open: "and possibly more"). A record type is
`{ row }` and a tag-union type is `[ row ]`. Labels are record fields in one and
tags in the other; the unifier is the same. Unifying two rows unifies the types
of shared labels and pushes labels present on only one side into the other's
tail (fresh row variable). Duplicate labels in a row are an error.

- `r.a` requires `{ a : t, ..r' }`.
- A match requires `[ #a T, …, ..r' ]` (open if it has a wildcard arm, closed
  otherwise).
- **Record spread is update-only.** In `{ ..r, a = v, … }` every explicitly
  written key must already be in `r`'s row, with the same type as `v`. The result
  has the type of `r`. It cannot add a field or change a field's type, so
  `r : { a : t, ..ρ }` and `v : t` give `{ a : t, ..ρ }`. A literal has at most
  one spread, and it comes first. Extending a record is not expressible yet and
  needs a separate construct later (lacks constraints or scoped labels).

### 7.4 Open

- **Canonical form for recursive types**, only needed if types are hashed or
  compared across the wire (7.5).

### 7.5 Recursive types

Surface: an alias may refer to itself, provided every cycle passes through a tag
payload.

```
tree = a => [#leaf, #node { value : a, left : tree a, right : tree a }]
```

Records, lists and function arrows do not count as guards, so `t = list t` and
`t = t` are rejected. Mutually recursive aliases are checked per strongly
connected component.

Core: aliases are erased, so a self-reference becomes a back-reference to an
enclosing fixpoint binder `mu`. `mu` is internal; the surface language never
spells it. `mu . T` denotes the type equal to `T` with itself substituted for the
back-reference, and the unifier treats it as equal to its one-step unfolding
(equi-recursive, with a visited-pairs set to terminate). Different `mu` terms can
denote the same infinite tree, so hashing or comparing types needs a minimised
canonical form (partition refinement).

### 7.6 Type scraps

A scrap whose body denotes a type, rather than a value, is a **type scrap**. It
exports that type: other scraps use a hash reference to it in type position, as
they would a builtin or sort binding of the same kind.

```
t
; t : type = [#a a, #b int, #c byte]
; a : type = [#x, #y, #z]
```

```
f : $sha256~~… -> int = | #a _ -> 1 | #b n -> n | #c _ -> 0
```

- The body denotes a type when it is a name bound by a sort binding, or a hash
  reference to a type scrap. Where-clauses are looked through, so the body may be
  `t ; t : type = …`. Anywhere else, a type in value position is an error.
- A type scrap's type is its kind together with the type it denotes: tooling
  prints the first example as `type = [ #a [ #x, #y, #z ], #b int, #c byte ]`.
- A type scrap has no value, since sort bindings are erased. Evaluating one is an
  error, reported before anything runs. A hash reference to it in value position,
  or to a value scrap in type position, is a type error.
- Exports are still structural: the hash names a type, it does not make it
  nominal. Two type scraps that denote the same type are interchangeable.

This exports one type per scrap. Bundling a library's types and values into one
scrap needs types as record members, which is planned work (section 12).

## 8. Binding groups

A `;`-group's bindings are mutually recursive. Under source hashing (section 9)
the order of bindings is part of the source and so of the hash. A future
canonical-core scheme would compute strongly connected components and order them
by dependency, so source order would not affect identity.

## 9. Scrap identity

### 9.1 Scheme `src-sha256` (current)

For now a scrap is hashed from its source, which is heavier than hashing a
canonical core but needs no canonicalisation pass.

- A published **scrap** is `(edition, source)`. `source` is the program text,
  which refers to other scraps by hash only (section 11).
- **Source bytes** are valid UTF-8, with no BOM and LF line endings only. They are
  stored and hashed exactly as written; tools never normalise them. Anything else
  is rejected at publish time, so no two accidental encodings of one text exist.
- `digest = sha256( preimage )`, where the preimage is the list of length-prefixed
  byte strings: the scheme tag `src-sha256`, the edition, then the source bytes.
- The **edition** (spec version covering both grammar and typing rules) is part of
  the digest, because the same bytes can parse or type-check differently in a
  different edition. An edition upgrade therefore produces new identities, which
  cascade to dependents.
- A **hash literal** is scheme-tagged: `$src-sha256~~…`. A later scheme (for
  example `core-sha256`, canonical core) can coexist, and each scheme defines how
  a target is re-hashed when verified.

### 9.2 What this costs and what it keeps

Costs, accepted: two programs that mean the same thing but are spelled
differently have different identities. That covers whitespace, comments, binder
names, binding order, record field order, and `x |> f` versus `f x`. There is no
deduplication across spellings, and a comment-only edit changes the hash.

Keeps: integrity, immutability, closure verification (section 10), and the Merkle
cascade (a changed dependency changes every dependent's hash). A scrap cannot
contain its own hash, and scraps cannot depend on each other cyclically.

Identity depends on the edition and the source, not on the parser's output, so a
scrap can be verified without parsing it. It must still be parsed before it
is run.

## 10. References and loading

Reliability is a top-level requirement. The rules:

1. Evaluation and type checking are defined only on **closed** terms. A free
   variable is an error ("unbound name"), reported before anything runs. There
   are no free names to resolve: a scrap refers to another only by a hash or, in
   a draft, a path (section 11). There is no ambient lookup, no
   auto-resolution, and no network access during parsing, checking or
   evaluation.
2. A hash reference is valid only if its target is present in the local store and
   re-hashing it under the reference's scheme gives the digest. Verification
   happens on every read, so a corrupted or substituted scrap is rejected.
3. A scrap's **closure** is itself plus every scrap reachable through its hash and
   path references. Loading is a separate, explicit step that completes the
   closure ahead of time, fetching hashes from stores and, for drafts, reading
   paths (11.2). Checking and evaluating require the whole closure.
4. A reference's type, or for a type scrap (7.6) the type it denotes, is inferred
   from the target's source (present by rule 3), parsed and checked under the
   target's edition. The digest includes the edition (section 9), so any type
   cache for published scraps is keyed by the digest alone. The cache is an
   optimisation only; deleting it never changes a result.
5. Name maps (versions, "latest", signing, scrapyard policy) are tooling. They
   produce hash or path references and are never consulted by the language
   itself.
6. **Results are scraps.** A value prints as a closed scrap that evaluates to
   the same value and prints identically. So `scrap eval` of the output of an
   earlier `scrap eval` always gives that same output again. This requires:
   - Functions print with the bindings they capture as a where-clause,
     `b -> a ; a = 3`, recursive ones included. Builtins print by name, and
     captured hash references stay hash references.
   - A function that a binding defines prints as that binding's name, so
     recursive and mutually recursive groups print as groups:
     `f ; f = | 0 -> 1 | n -> n * f (n - 1)`.
   - The bindings that a value reaches, from whatever scopes, print as one
     where-group after it. Two with the same name are told apart by primes,
     `a'`, chosen so that no binder in the printed code shadows them.
   - Bindings keep their annotations, and the sort bindings that those refer
     to print too. Recursive functions over recursive types (7.5) need them
     to check.
   - Printing adds the parentheses the grammar needs: `#fun (x -> x * 2)`,
     `list/fold 0 (a -> b -> a + b)`.
   - TEXT, BYTE and BYTES print with escapes that read back to the same bytes, and
     FLOAT prints exactly, in the shortest form that reads back to the same float.

## 11. Drafts and published scraps

### 11.1 Two flavors

Scraps come in two flavors:

- A **draft** is a scrap being developed, typically a file. It may refer to
  other drafts by path (11.2) and to published scraps by hash. A draft has no
  identity: it cannot be hashed, pushed, or referred to by hash.
- A **published** scrap is immutable and identified by its hash (section 9). It
  refers to other scraps by hash only; a path reference in one is an error.
  Nothing that handles published scraps reads the filesystem.

Only drafts ever touch the filesystem, so production use, which runs published
scraps only, never does.

### 11.2 Path references

A path reference is `$` followed by a TEXT literal: `$"./terminal/program.scrap"`.

- **A path is relative to the draft that has it.** A draft read from standard
  input is an unnamed draft in the current directory, so its paths are relative
  to that directory.
- **The root is the current directory.** Every path must resolve to a file
  within it, also after `..` steps; an absolute path is an error.
- **Paths may not form cycles.** A draft that refers back to itself, directly or
  through others, is an error. (Hashes cannot form cycles at all.)
- Paths use `/` on every system.

For example, with the current directory holding these drafts:

```
a.scrap            $"./subdir/b.scrap"
subdir/b.scrap     $"../other/c.scrap"
other/c.scrap      42
```

`a.scrap` refers to `subdir/b.scrap`, whose `../other/c.scrap` is relative to
`subdir/`, so it refers to `other/c.scrap`, and all three evaluate to `42`. Piped
in on standard input, `$"./a.scrap"` does too, while `$"../a.scrap"` is an error,
as it leaves the current directory.

For simplicity, the CLI reads a draft from standard input only; running one by
its path, as in `scrap eval example.scrap`, could be added later.

### 11.3 Publishing

Publishing turns a draft and its closure of drafts into published scraps,
bottom-up: each draft it refers to is published first, then each path token is
replaced by `$src-sha256~~…` of its target's published form. Nothing else in the
source changes, comments and layout included, so publishing the same drafts
always gives the same hashes.

## 12. Planned work

- **Specify the core evaluation semantics in Coq (now Rocq).** Not started. Scope,
  following the claims this document already makes:
  - The core nodes of section 5 and a strict evaluation relation, including
    evaluation order, recursive bindings, and first-match-wins with multi-pattern
    arms desugared to a unary match on a record.
  - Determinism of evaluation on closed terms.
  - Type soundness (progress and preservation) with row-polymorphic records and
    variants, and the exhaustiveness rule of 7.1: a closed, well-typed term never
    raises a match error.
- **Canonical encoding of the core** (section 9), deferred by decision.
- **Types as record members**, the end goal for exporting types: one scrap
  bundles a library's types and values, as in
  `{ option = a => [#some a, #none], map = … }`, used as `lib.option int` in type
  position. Record types then carry manifest type members, which row unification,
  printing and type caching must handle. Open questions: whether members can be
  made abstract, and when two such records' types are equal. Type scraps (7.6)
  are the interim mechanism.

## 13. Platforms (sketch)

A platform is what lets a scrap affect the world. Platforms are defined by their
implementations, so the language only fixes how a script meets one. A platform
has three parts:

1. **An interface**: a type scrap (7.6) for the contract between script and
   host, such as a record of state transitions and the events and actions they
   exchange. Its hash identifies the platform, so a script names a platform by
   naming its interface.
2. **A host**: native code that implements the interface for one host language,
   generated from the interface type, with the effects filled in by hand.
3. **A script**: an ordinary scrap whose value has the interface's type. Running
   it on a platform first checks it against the interface, so a script that
   type-checks cannot violate the contract.

Open: how generic interfaces such as `program s` are instantiated per script;
whether a platform can provide a pure reference semantics (like
`platforms/terminal.scrap`'s `simulate`) that hosts must agree with; and how
hosts are found from an interface's hash without breaking rule 1 of section 10.

## 14. Relation to the guide

[scrapscript.org/guide](https://scrapscript.org/guide) describes the reference
design. This grammar follows it where listed below, departs from it on purpose
elsewhere, and leaves some of it open.

**Aligned:**

- Where-clauses after the body, mutually recursive.
- Match functions as the only control flow, with multi-pattern arms
  (`| 1 -> 2 -> 3`); no FLOAT patterns.
- `()` as the hole; INT, FLOAT, TEXT, BYTE and BYTES literals: `~FF` is one
  byte, `~~aGk=` is base64 bytes, and hashes are `$scheme~~…`.
- Names with `-` and `/`: `bytes/to-utf8-text`.
- Records with `..r` spread first, which can update fields but not change their
  types; record patterns with the rest first, `{ ..x, a = 1 }`.
- Lists of one element type; `+<`, `>+` and `++`.
- Exhaustive matches, checked by the type checker.
- A scrap may be a type (7.6).
- Any expression can be replaced by a hash reference to a scrap.
- `scrap eval` and `scrap eval apply` pipelines, which rule 6 of section 10
  makes reliable.

**Departs, on purpose:**

| Guide | Here | Why |
|---|---|---|
| Free names resolved through scrap maps, latest version by default | **Intentionally divergent.** Free names are always an error; scraps refer to each other by hash, or by path in drafts (section 11) | A closed scrap means the same thing everywhere |
| Rocks: `$$add`, `$$int` | None | Builtins belong to the prelude, not to syntax |
| `scoop::chocolate`, `(#radius int)::radius 4` | `#chocolate`; `::` removed | Tags are structural, so they need no type to build them |
| `t : #a a #b int`, a type as tags side by side | `t : type = [#a a, #b int]` | The guide's form is clean, but ambiguous with tag values and payloads |
| Variants printed with their type, `(result text e)::ok "hi"` | `#ok "hi"` | Values carry no types at runtime |
| `<\|`, `<<`, `//`, `&&`, `\|\|`, `@` | Removed (section 6) | One spelling per thing; matching is the only branching |
| Hash of the flat encoding, `$sha1~~…` | Hash of edition and source, `$src-sha256~~…` | No canonical encoding yet; others can be added as schemes (section 9) |

**Open:**

- Float arithmetic: the guide writes `1.0 + 1.0`, while 7.1 asks for separate
  Int and Float operators that section 2.3 does not list.
- TEXT: the guide allows raw newlines and has interpolation, `"a` x `b"`.
- List patterns: the guide matches with `>+` and `++` (`head >+ tail`,
  `[x, y] ++ tail`), and text and bytes by prefix (`"hi " ++ name`); this
  grammar has `[a, b, ..rest]` only and lists prefix patterns as open (section 4).
- The `,` where-operator, binding tighter than `;` (in the guide's source, not
  shown on the page).
- Flat scraps (a compact binary encoding), scrap maps, yards, passes and books:
  tooling, to be specified separately.
- The prelude.
