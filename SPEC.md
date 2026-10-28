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
INT      = digit { digit }                       # 64-bit for now, wrapping on overflow
FLOAT    = digit { digit } "." digit { digit }   # digits on both sides
TEXT     = '"' { char | escape } '"'             # escapes: \" \\ \n \t \u{HEX}
BYTE     = "~" hex hex                           # one byte: ~FF
BYTES    = "~~" { base64 }                       # padded base64: ~~aGVsbG8=
HASH     = "$" scheme ":" hex { hex }            # a published scrap: $source:5c43…, lowercase hex (9)
PATH     = "$" TEXT                              # a draft's reference to a file, development only (11.2): $"./a.scrap"
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
- `~~` always means base64 bytes follow. A HASH is one token, with no
  whitespace inside: `$`, a scheme NAME, `:` and lowercase hex digits, whose
  number the scheme fixes. A PATH is one token too: `$` directly followed by a
  TEXT. A `$` followed by anything else is a lexical error. Digests are hex because that is how `sha256sum`, git
  and most tools print them, so a hash literal can be compared with their output
  directly.
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
|>  <|  >>  <<  ==  /=  <  <=  >  >=  ++  >+  +<  +  -  *  /  %  ^
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
         | "{" bfield { "," bfield } "}" "=" body  # destructuring binding
bfield   = NAME [ "=" NAME ]                     # `{ x }` is `{ x = x }`, binding x
rhs      = type                                  # sort binding (section 7.2)
         | body                                  # value binding

body     = binder "->" body                      # function
         | arm { arm }                           # match function
         | pipe

arm      = "|" pattern { "->" pattern } "->" arm-body    # one pattern per argument
arm-body = cmp                                   # NOT a bare match or a bare lambda: parenthesise

binder   = NAME | "_"                            # "_" ignores the argument
pipe     = comp { "|>" comp }                    # |> left-assoc, <| right-assoc
           [ ( "|>" | "<|" ) body ]              # the last operand may be a function
comp     = cmp { ( ">>" | "<<" ) cmp }           # left-assoc: f >> g and g << f are x -> g (f x)
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
         | "{" [ [ ".." atom "," ] field { "," field } ] "}"   # the fields update the spread record
field    = NAME [ "=" expr ]                     # `{ x }` is `{ x = x }`
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
- **Arms end at pipes.** An arm's body stops before `|>`, `<|`, `>>` and `<<`, so a pipe
  after a match function applies the whole function, and the function can be
  piped into: `x |> | 0 -> "zero" | _ -> "other"`. The last operand of a
  pipe may be a function, which extends to the right: `3 |> a -> a + 1`.
- `listop` mixing: `a >+ b +< c` parses as `a >+ (b +< c)`.
- **Multi-pattern arms.** Each pattern after the first `->` matches the next
  argument, so a match function takes as many arguments as its arms have patterns:
  ```scrap
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
- **Destructuring bindings.** `{ a, b = c } = r` binds `a` to `r.a` and `c` to
  `r.b` in the group, as if written `a = r.a ; c = r.b` with `r` evaluated
  once: `max 3 4 ; { max, sort } = ord.on int/cmp`. The record may have more
  fields. It is a value binding, so it has no annotation and can't bind a
  type member, and it can't be recursive, since a record isn't a function
  (section 8). Fields are names only; nested patterns are not supported yet.

## 4. Patterns

Patterns are their own nonterminal, parsed directly (never an expression that is
validated afterwards).

```
pattern  = pat-atom
         | pattern ">+" pattern                  # head >+ tail
         | pattern "+<" pattern                  # init +< last
         | pat-list "++" pattern                 # [a, b] ++ rest
         | pattern "++" pat-list                 # rest ++ [a, b]

pat-atom = INT | TEXT | BYTE | BYTES             # no FLOAT patterns
         | NAME                                  # binds
         | "_"                                   # wildcard
         | TAG [ pat-atom ]                      # #true   #some x   #pair (a)
         | "(" [ pattern ] ")"                   # "()" matches the hole
         | pat-list
         | "{" [ [ prest "," ] pfield { "," pfield } ] "}"
pat-list = "[" [ pattern { "," pattern } ] "]"   # exactly these elements
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
| `a \|> f`, `f <\| a` | `Apply(f, a)` |
| `f >> g`, `g << f` | `Fn(Apply(g, Apply(f, Var0)))` (no generated names) |
| `a + b` etc. | `Apply(Apply(Var(add), a), b)` against the platform prelude, or dedicated primitive nodes (decide once) |
| `a >+ xs` / `xs +< a` | cons / snoc primitives |
| `#tag` | `Variant(tag, Hole)` |
| `{ ..r, k = v }` | record update primitive |
| `e ; x = v` | `Let` group |
| `\| p1 -> p2 -> e` (n patterns) | n-ary `Fn` over a unary `Match` on an internal record `{ 0 = a1, 1 = a2 }`, so first-match-wins and exhaustiveness work across all columns, and the core is unchanged |

## 6. Removed from the reference design

| Removed | Reason |
|---|---|
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
- A match **without** a wildcard arm closes its argument type to at most the
  tags it lists, so it is exhaustive by construction: it also accepts values
  that never have some of them. A wildcard arm leaves the argument type open.
- A name pattern excludes the tags that earlier arms **fully cover** at its
  position: an arm covers `#t` there if its only refutable tests are `#t` at
  that position, with an irrefutable payload pattern, and the tags on the way
  there. Each use of the name may have the excluded tags or not, so
  `| #lit n -> n | other -> ext other` passes `other` to an `ext` that doesn't
  handle `#lit`, and `| #none -> d | o -> option-map f o` to one that does. Exhaustiveness is a typing rule: a scrap that type-checks
  cannot raise a match error. INT, BYTE, TEXT and BYTES patterns always need a
  catch-all arm. The checker closes enums, then, once the whole scrap is
  inferred, checks every match function against its argument types (Maranget's
  usefulness, one column per argument of a multi-pattern arm, with list
  patterns split by length) and names arguments that no arm matches. A host's call into a scrap doesn't check its arguments, so a value
  from a host that no arm accepts still stops evaluation with an error.
- Records and variants share one row mechanism (7.3).
- Int and Float are separate types with no overloading. Type classes are
  deferred; until then arithmetic uses separate Int and Float operators.
- Type aliases are naming only and are erased from the core. (Under source
  hashing they are still part of a scrap's source, so they affect its hash.)
- Printed types keep the alias names they were written with, and bind them in a
  trailing where-clause, instead of inlining each alias wherever it occurs:
  ```scrap
  [ #first io, #second io ] -> io
  ; io : type = [ #err text, #ok ]
  ```
  This is display only: aliases never affect unification. Each binding is a
  sort binding, `; io : type = …` or `; tree = a => …`, with primes for
  clashing names. An alias of a primitive type prints as the primitive. A type
  scrap prints its own alias unfolded, `type = [ #err text, #ok ]`, unless it is
  recursive (7.5). A type from an imported type scrap prints as the reference,
  `$"./io.scrap"`, with no binding. When types written with different aliases,
  or an alias and none, unify, the first named one is kept. Open: whether the
  where-clause should read back as a type scrap (section 10, rule 6).
- Tag payload arity is positional (section 3, "Tag payloads").
- Recursive aliases are allowed when every cycle passes through a tag payload,
  and are equi-recursive (7.5).

### 7.2 Type grammar

Type names are lowercase: `int float text byte bytes list`, plus `()` for the hole
type. Types and values share one namespace. A name bound by a sort binding
(below) may appear only in type positions, and sort bindings are erased from the
core. The exceptions are a scrap's body, which may name a type to export it
(7.6), and a record literal's field, which makes the type a member (7.7).

Type variables are introduced explicitly with `=>`; there is no implicit
quantification. A name in a type is a variable bound by an enclosing `=>`, a
builtin, or a sort binding in scope; anything else is an error.

```
type     = NAME "=>" type                        # prenex quantification / type lambda
         | arrow
arrow    = tapp [ "->" arrow ]                   # right-assoc
tapp     = tatom { tatom }                       # type application: list int
tatom    = tpath | "()" | "(" type ")"
         | "{" [ tfield { "," tfield } ] [ "," ".." NAME ] "}"   # record row
         | "[" [ ttag   { "," ttag   } ] [ "," ".." NAME ] "]"   # tag-union row
tfield   = NAME ":" type
ttag     = TAG [ tatom ]
tpath    = ( NAME | HASH | PATH ) { "." NAME }   # a type, a type scrap, or a type member
```

A path with dots, `avl.tree` or `$"./avl.scrap".tree`, names a type member and
resolves through static parts (7.7).

A row tail may stand alone: `{ ..r }`, `[ ..r ]`. Examples:

```scrap
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

```scrap
has-a : type -> row -> type = a => r => { a : a, ..r }
```

A binding whose annotation's final result is `type` is a **sort binding**. Its
right-hand side is parsed with the type grammar. A type constructor is a
type-level lambda, `s => T`, one parameter per `=>`:

```scrap
event  : type                  = [#line text, #eof]
turn   : type -> type          = s => { state : s, print : list text, next : [#read-line, #exit int] }
either : type -> type -> type  = a => b => [#left a, #right b]
```

The annotation is optional when the right-hand side starts with `NAME "=>"`,
because `=>` occurs only in type syntax:

```scrap
option = a => [#some a, #none]          -- kind inferred: type -> type
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

Each label of a row is **present**, **absent**, or not yet known: a presence
variable, which unification binds like a type variable. A closed row has every
label it doesn't list absent, and a label present in one row can't be absent
from the other. Presence has no syntax: a type prints the instance that
accepts the most and returns the least, with a label present where its
presence is a variable in an input position (a function's argument), and
absent otherwise. Errors print every label that may be present. Open: reading
a closed enum in an input position of an annotation as "at most those tags",
so that printed types read back as general as they were inferred.

Rows compare by label, not position: `[#a int, #b text]` and
`[#b text, #a int]` are the same type. To keep each label unique, a row variable
lacks the labels written before it: in `{ a : t, ..r }`, `r` can never be bound
to a row with `a`. The checker reports where a duplicate would arise, as when
reading `.a` from the rest of a record that a pattern took `a` from. Lacks
constraints are internal; they have no syntax, since a variable's position
gives them.

- `r.a` requires `{ a : t, ..r' }`.
- A match requires `[ #a T, …, ..r' ]` (open if it has a wildcard arm, closed
  otherwise), where each tag it lists may be present or not.
- **Record spread is update-only.** In `{ ..r, a = v, … }` every explicitly
  written key must already be in `r`'s row, with the same type as `v`. The result
  has the type of `r`. It cannot add a field or change a field's type, so
  `r : { a : t, ..ρ }` and `v : t` give `{ a : t, ..ρ }`. A literal has at most
  one spread, and it comes first. Extending a record is not expressible yet and
  needs a separate construct later, which lacks constraints make possible.

### 7.4 Open

- **Canonical form for recursive types**, only needed if types are hashed or
  compared across the wire (7.5).

### 7.5 Recursive types

Surface: an alias may refer to itself, provided every cycle passes through a tag
payload.

```scrap
tree = a => [#leaf, #node { value : a, left : tree a, right : tree a }]
```

Records, lists and function arrows do not count as guards, so `t = list t` and
`t = t` are rejected. Mutually recursive aliases are checked per strongly
connected component. The guard is checked on the expanded type, so an alias
may recur as an argument to another that puts it in a payload:

```scrap
expr-f = a => [#lit int, #neg a]
; expr : type = expr-f expr
```

A recursive use applies the alias to its own parameters, so that the type is a
finite graph: `t = a => [#a (t (list a))]` is rejected.

A type need not have finite values. `ex = [#next ex]` is allowed, and is
uninhabited, since recursive values are not (section 8).

Core: aliases are erased, so a self-reference becomes a back-reference to an
enclosing fixpoint binder `mu`. `mu` is internal; the surface language never
spells it. `mu . T` denotes the type equal to `T` with itself substituted for the
back-reference, and the unifier treats it as equal to its one-step unfolding
(equi-recursive). Different `mu` terms can denote the same infinite tree, so
hashing or comparing types needs a minimised canonical form (partition
refinement).

The checker ties the knot instead: `mu . T` is a cycle in its type graph. Every
cycle passes through an enum, so the unifier merges two enums before unifying
their tags, and comes back to them merged, which is what terminates it.

A recursive type prints as the name of the alias it came from, which a
trailing where-clause defines (7.1):

```scrap
tree -> int
; tree : type = [ #leaf, #node { left : tree, right : tree } ]
```

Recursion is not inferred: an unannotated function that recurses on a
recursive value still fails the occurs check. Annotate it with the alias.

### 7.6 Type scraps

A scrap whose body denotes a type, rather than a value, is a **type scrap**. It
exports that type: other scraps use a hash reference to it in type position, as
they would a builtin or sort binding of the same kind.

```scrap
t
; t : type = [#a a, #b int, #c byte]
; a : type = [#x, #y, #z]
```

```scrap
f : $source:… -> int = | #a _ -> 1 | #b n -> n | #c _ -> 0
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

A type scrap exports one type. A library bundles its types with its values as
the members of the record it denotes (7.7). Both follow one rule: a reference in
type position resolves from the body of the scrap it names.

### 7.7 Static parts and type members

A scrap runs in two phases. The **static** phase builds the program: it
resolves types, and the members that paths in types reach, without inferring or
running any value. The runtime phase evaluates values only. Types and members
belong to the static phase, and are erased from the core.

An expression's **static part** is the type it denotes, or the type members of
the record it denotes:

- A name bound by a sort binding: that type. A name bound by a value binding:
  the static part of the binding's value.
- `e ; …`: the static part of `e`, with the where-clause's bindings in scope.
- A reference: the static part of the referenced scrap's body. An import means
  the same as its source inlined in parentheses, so a path resolves the same way
  locally and across imports.
- A record literal without a spread: its fields whose values have a static part,
  by name. These are its **members**.
- `e.m`: the member `m` of the static part of `e`.
- Anything else, such as a lambda, a call or a spread, has none. Members don't
  survive value flow: for `f = x -> …`, a path `x.t` is an error, and so are
  `(id lib).t` and `{ ..lib, v = 1 }.t`.

```scrap
f
; f : avl.tree text int -> int = avl.size
; avl = $"./avl.scrap"
```

```scrap
{ tree = tree, empty = #leaf, size = size, … }
; tree : type -> type -> type = k => v => [#leaf, #node (node k v)]
; …
```

- A field whose value denotes only a type is a member and not a field of the
  value: `{ t = t, v = 1 }` has the type `{ v : int }`, and its runtime shape is
  that of `{ v = 1 }`. A field whose value is a record with members, such as
  `{ s = { t = t, v = 1 } }`, is both a value field and a member.
- A member names a type: a sort binding, a path, or a reference to a type scrap.
  The kind comes from the sort binding's annotation.
- Members are transparent: `avl.tree text int` is the structural type it expands
  to, with the same arity and row-argument checks as a type scrap (7.6).
- In type position, a name bound by a value binding without a static part
  doesn't hide a type of the same name in an outer scope, so a parser named
  `int` doesn't hide the type `int`.
- Static parts don't depend on the order of a where-group: `state : type = {
  paths : avl.tree text int }` and `avl = …` may share a group in either order.
- A scrap's result exports the members of the static part of its body, which is
  all that importers see of it.
- Printing keeps members where their record literal is printed (section 10,
  rule 6).

Open: abstract members, which would need nominal identity; members through
applications (section 12); the printed type of a value, as `scrap type` shows
it, names a path `avl.tree` without a binding for `avl`.

## 8. Binding groups

A `;`-group's bindings are mutually recursive. Under source hashing (section 9)
the order of bindings is part of the source and so of the hash. A future
canonical-core scheme would compute strongly connected components and order them
by dependency, so source order would not affect identity.

**Recursive values are forbidden.** Only functions may be recursive: if a
binding's strongly connected component refers to itself, every binding in it
must be a function or match function, possibly under where-clauses. Evaluation
is strict, so any other value would be used before it exists:

```scrap
x ; x : ex = #next x                    -- error: x is recursive
r ; r = { f = _ -> r }                  -- error, though r is only used in a function
ones
; ones : () -> stream = _ -> #more { head = 1, tail = ones }   -- fine: a function
; stream : type = [#more { head : int, tail : () -> stream }]
```

A reference is a free name of a binding's value; names that the value binds
itself, as parameters, patterns or where-clauses, shadow the group's.

## 9. Scrap identity

### 9.1 Scheme `source` (current)

For now a scrap is hashed from its source, which is heavier than hashing a
canonical core but needs no canonicalisation pass.

- A published **scrap** is its source: the program text, which refers to other
  scraps by hash only (section 11).
- **Source bytes** are valid UTF-8, with no BOM and LF line endings only. They are
  stored and hashed exactly as written; tools never normalise them. Anything else
  is rejected at publish time, so no two accidental encodings of one text exist.
- `digest = sha256( source bytes )`, written as 64 lowercase hex digits:
  `$source:5c43…`. It is what `sha256sum` prints for the file.
- A **hash literal** is scheme-tagged, and the scheme names what is hashed. The
  hash function is sha256 for every scheme; a different one would be a new
  scheme. Each scheme defines how a target is re-hashed when verified. An
  unknown scheme is an error.
- Open: editions. The same bytes can parse or type-check differently in a later
  edition (spec version), so the edition should take part in identity, either
  in the `core` scheme or as a scheme of its own.

### 9.1.1 Scheme `core` (reserved)

`$core:…` will be the sha256 of a scrap's canonical compact representation,
which is not yet defined. It is reserved: a `core` hash is an error for now.

### 9.2 What this costs and what it keeps

Costs, accepted: two programs that mean the same thing but are spelled
differently have different identities. That covers whitespace, comments, binder
names, binding order, record field order, and `x |> f` versus `f x`. There is no
deduplication across spellings, and a comment-only edit changes the hash.

Keeps: integrity, immutability, closure verification (section 10), and the Merkle
cascade (a changed dependency changes every dependent's hash). A scrap cannot
contain its own hash, and scraps cannot depend on each other cyclically.

Identity depends on the source, not on the parser's output, so a
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
   from the target's source (present by rule 3), parsed and checked. While
   the digest leaves out the edition (9.1), a type cache for published scraps
   must be keyed by the digest and the checker's edition. The cache is an
   optimisation only; deleting it never changes a result.
5. Name maps (versions, "latest", signing, scrapyard policy) are tooling. They
   produce hash or path references and are never consulted by the language
   itself.
6. **Results are scraps.** A value prints as a closed scrap that evaluates to
   the same value and prints identically. So `scrap eval` of the output of an
   earlier `scrap eval` always gives that same output again. This requires:
   - Functions print with the bindings they capture as a where-clause,
     `f ; f = b -> a ; a = 3`, recursive ones included. Builtins print by
     name, and captured hash references stay hash references.
   - A function applied to fewer arguments than it takes prints as that
     application: `(a -> b -> a) 3`, or `f 3` for a function bound to `f`.
     A chain of lambdas `a -> b -> …` takes all its arguments at once.
   - A function that a binding defines prints as that binding's name, so
     recursive and mutually recursive groups print as groups:
     `f ; f = | 0 -> 1 | n -> n * f (n - 1)`.
   - The bindings that a value reaches, from whatever scopes, print as one
     where-group after it. Two with the same name are told apart by primes,
     `a'`, chosen so that no binder in the printed code shadows them.
   - Bindings keep their annotations, and the sort bindings that those refer
     to print too. Recursive functions over recursive types (7.5) need them
     to check.
   - Type members (7.7) print with the record literals they belong to: a
     binding, or the scrap's result, whose value is a record literal prints
     its fields and its members. A binding whose value is a reference prints
     as that reference. A binding that only paths in types reach, such as
     `avl` in `avl.tree`, prints as its static part alone: a reference as
     itself, a record literal as its members, `{ option = option }`. A record
     that reaches the result any other way, as through a call, prints without
     members, since members don't survive value flow.
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
replaced by `$source:…` of its target's published form. Nothing else in the
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
- **Type members** (7.7) are resolved statically and erased, so record types
  carry none and row unification is unchanged. Open: abstract members.
  - Later, members through applications, as ML functors: in
    `ordered = cmp -> { tree = tree, insert = … }` the members don't depend
    on `cmp`, so `(avl.ordered text/cmp).tree` could resolve at compile time
    (the static part of an application is that of the function's body). A
    library could then close over its comparison instead of taking it in
    every call.
- **Compact runtime values.** Names exist only in source, types and printing;
  at runtime, values are words. Started: variables are frame slots, tags
  and labels are interned IDs, and records are buffers under a shape. The
  design:
  - Variables resolve to frame slots (section 5's `Var(index)`), so a frame is
    a buffer of words and binding a pattern writes into it.
  - Tags and labels are interned to integers by a table that the runtime and
    the type checker share, one per environment (package `label`). `#foo 5` is two words, `(tagOf("foo"), 5)`; a tag
    without a payload needs only its id. A match switches on the id. Ids are
    global rather than dense per enum, because rows let one value flow
    through enums with different tag sets.
  - A record is its field values in label order, by name: `{ a = 2, b = 4 }`
    is `(2, 4)`, and a field's index is its label's rank in the record's
    label set. When the checker knows that set, indices are static.
  - Row polymorphism is the hard part: `{ a : int, ..r } -> int` doesn't know
    where `a` is. Options: a shape word per record (its interned sorted label
    set, which a field access searches, or caches per site), indices passed as
    hidden arguments (evidence passing, as in Leijen's row calculi), or
    specialising functions per shape. A shape word costs one word per record
    and keeps types erased, which suits closures that print their source.
  - Spreads `{ ..r, a = 1 }` build a new buffer under `r`'s shape: a spread
    only updates fields that `r` has (section 7), so the shape never changes.
  - Printing, equality and the flat encoding map ids back to names through the
    table. Ids are per environment and never part of a scrap's identity
    (section 9); a host gets them by name, as generated bindings do once
    when they load a script.
  - Uniform words need boxing for TEXT, BYTES, lists and closures: tagged
    immediates and a custom heap, which suits a
    WebAssembly backend better than Go interfaces. So this is likely a
    separate backend over the core, after the tree-walker has slots.

## 13. Platforms (sketch)

A platform is what lets a scrap affect the world. Scraps stay pure: a platform
gives them no effects to call, only types to return effects as data in. A
platform has three parts:

1. **An interface**: a type scrap (7.6) for the contract between script and
   host. A script names a platform by naming its interface.
2. **A host**: a Go program that runs scripts on the platform, using Go
   bindings generated from the interface, with the effects written by hand.
   The `scrap` tool may build in hosts for a few platforms; there is no
   general way to run a script on a platform by name (section 14).
3. **A script**: an ordinary scrap whose value has the interface's type. The
   host checks it against the interface once, when loading it; after that the
   host calls it without checking, so a script that type-checks cannot violate
   the contract.

   The types that a script chooses for the interface's parameters must be
   closed: once it is checked against the interface, no record or enum in
   its type may still end in a row variable. The host passes back what the
   script returns, so nothing else can add to these types, and a row left
   open only means that a catch-all arm takes tags that no arm names, such
   as `#runing` for `#running`. A script closes them by annotating the type,
   or by matching it without a catch-all arm (7.1).

**Effects as data.** A script returns what it wants done, and a function to
continue with once it is done, which the host calls with the result:

```scrap
; reply : type = [#respond response, #fetch { request : request, then : response -> reply }]
```

**State.** A type constructor's parameters are the script's to choose, and
opaque to the host: the terminal platform's `program s` threads a state `s`
from one event to the next. A platform that serves requests concurrently
instead takes an immutable context, made once at startup and passed to each
request; state that outlives a request belongs to the host's effects, such as
a database.

**Bindings.** Generated from the interface's type:

| Type | Go |
|---|---|
| `()`, `int`, `float`, `text`, `byte`, `bytes` | `struct{}`, `int64`, `float64`, `string`, `byte`, `[]byte` (copied) |
| `list a` | `host.ListRef[A]`, or `[]A` |
| record | a Ref with an accessor per label, in CamelCase, or a struct |
| enum | a Ref with a tag, constants dense per enum, and a typed accessor and constructor per tag, or a struct of the same |
| function | Go func, uncurried; it returns an error if evaluation fails. A function in a record's Ref is a method |
| type parameter | an opaque wrapper of the value, which the host can only pass back |

A Ref holds the value, converts only the parts that the host reads, and
passes back to a script as it is; its `Load` converts all of it to the
struct, for hosts that want plain Go values, and the struct's `Ref` converts
it back, which is how a host makes values. Aliases name the Go types.

Open: cancelling a running evaluation (a `context.Context`, checked on each
call, without counting steps); whether host resources such as connections can
be values, and how they print; open rows and aliases used at more than one type
in interfaces, rejected at first; whether a platform provides a pure reference
semantics (like `platforms/terminal/simulate.scrap`'s `simulate`) that hosts
must agree with.

## 14. Relation to the guide

[scrapscript.org/guide](https://scrapscript.org/guide) describes the reference
design. This grammar follows it where listed below, departs from it on purpose
elsewhere, and leaves some of it open.

**Aligned:**

- Where-clauses after the body, mutually recursive.
- Match functions as the only control flow, with multi-pattern arms
  (`| 1 -> 2 -> 3`); no FLOAT patterns.
- `()` as the hole; INT, FLOAT, TEXT, BYTE and BYTES literals: `~FF` is one
  byte and `~~aGk=` is base64 bytes.
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
| `//`, `&&`, `\|\|`, `@` | Removed (section 6) | `//` was unevaluated; `@` is spelled `.field`; matching is the only branching |
| sha1 of the flat encoding, written `$sha1~~…` | sha256 of the source, written `$source:` and hex digits (section 9) | No canonical encoding yet; hex is what `sha256sum` prints; other encodings can be added as schemes |
| `scrap platform connie2036/server`, a platform found by name | A host is a Go program, with bindings generated from the platform's interface (section 13) | Effects need native code, which a scrap name cannot supply |

**Open:**

- Float arithmetic: the guide writes `1.0 + 1.0`, while 7.1 asks for separate
  Int and Float operators that section 2.3 does not list.
- TEXT: the guide allows raw newlines and has interpolation, `"a` x `b"`.
- Text and bytes prefix patterns: the guide matches `"hi " ++ name`; this
  grammar lists them as open (section 4). List patterns follow the guide
  (`head >+ tail`, `[x, y] ++ tail`).
- The `,` where-operator, binding tighter than `;` (in the guide's source, not
  shown on the page).
- Flat scraps (a compact binary encoding), scrap maps, yards, passes and books:
  tooling, to be specified separately.
- The prelude.
