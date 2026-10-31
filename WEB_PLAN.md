# Web platform plan

A platform for web apps in the Elm architecture, a playground for them on
the site, and its runtime as an npm package that other pages can use. Built
as a series of small PRs; tick them off here as they land.

## Decisions

- **Thread.** The wasm runs on the main thread, next to the DOM. The
  protocol between Go and JS is byte buffers that could as well be posted
  to a Web Worker, and the DOM side only applies patches and reports
  events, never calling into Go itself, so a worker transport can be added
  later.
- **Rendering.** Go keeps the previous view, diffs the new one against it,
  and writes binary patches, which a small JS interpreter applies.
- **Effects in v1.** HTTP, time, animation frames and random numbers, as
  commands and subscriptions (Elm's `Cmd` and `Sub`). URL routing, storage,
  focus and keyboard subscriptions come later.
- **HTTP bodies.** Text or bytes, chosen per request, or JSON that the host
  parses into a `json` enum the app matches on. Scrapscript has no JSON
  parser, and decoders as data would need much more interface.
- **Flags.** `init` takes a `json` value from the page that mounts the app;
  the playground passes `#null`.
- **Views.** Keyed children and typed events from the start.
- **Runaway scripts.** On the main thread a scrap that loops freezes the
  tab, so the evaluator gets a deadline.
- **Hot reload.** The playground keeps the model across an edit if its type
  is unchanged, and re-inits otherwise.
- **Imports.** The playground has one editor; scraps import from the yard
  by hash, through the "missing, fetch, ask again" loop that scrap-wasm
  already has, with a `fetchScrap` hook that embedders can replace.
  Drafts that import each other by path come later.
- **Layout.** All in this repository: the host in `cmd/scrap-web`, the npm
  package in a new `js/` workspace that `web/` uses, the playground a page
  in `web/`. Working name `@scrapscript/web`, settled when publishing.

## Interface

A draft. The counter after it checks and runs against it.

```scrap
app

; app : type -> type -> type = model => msg =>
    { init : json -> step model msg
    , update : msg -> model -> step model msg
    , view : model -> node msg
    , subscriptions : model -> list (sub msg)
    }
; step : type -> type -> type = model => msg => { model : model, cmd : list (cmd msg) }
; node : type -> type = msg =>
    [ #text text
    , #el { tag : text, attrs : list (attr msg), children : list (node msg) }
    , #keyed { tag : text, attrs : list (attr msg), children : list { key : text, node : node msg } }
    ]
; attr : type -> type = msg =>
    [ #attr { name : text, value : text }
    , #prop { name : text, value : json }
    , #style { name : text, value : text }
    , #on-click msg
    , #on-input (text -> msg)
    , #on-check (bool -> msg)
    , #on-submit msg
    , #on { event : text, msg : msg }
    ]
; cmd : type -> type = msg =>
    [ #http { method : text, url : text, headers : list header, body : text, expect : expect msg }
    , #now (int -> msg)
    , #random-int { min : int, max : int, msg : int -> msg }
    , #random-float (float -> msg)
    ]
; expect : type -> type = msg =>
    [ #text ([#ok (response text), #error text] -> msg)
    , #bytes ([#ok (response bytes), #error text] -> msg)
    , #json ([#ok (response json), #error text] -> msg)
    ]
; response : type -> type = body => { status : int, headers : list header, body : body }
; header : type = { name : text, value : text }
; sub : type -> type = msg =>
    [ #every { ms : int, msg : int -> msg }
    , #animation-frame (float -> msg)
    ]
; bool : type = [#false, #true]
; json : type =
    [ #null
    , #bool bool
    , #number float
    , #text text
    , #list (list json)
    , #object (list { key : text, value : json })
    ]
```

```scrap
app
; app : web int msg =
    { init = _ -> { model = 0, cmd = [] }
    , update = | #inc -> n -> { model = n + 1, cmd = [] }
               | #tick _ -> n -> { model = n + 1, cmd = [] }
    , view = n -> #el { tag = "button", attrs = [#on-click #inc], children = [#text (int/to-text n)] }
    , subscriptions = _ -> [#every { ms = 1000, msg = t -> #tick t }]
    }
; msg : type = [#inc, #tick int]
; web : type -> type -> type = $"./web.scrap"
```

## Efficiency

- Values are two words and immutable, so a subtree of the view that is the
  same value as in the last view is skipped in O(1), as with Elm's `lazy`
  but without asking. Needs an `eval.Same(a, b)`.
- Handlers are closures, which can't be compared, so they never cause
  patches. The DOM side only knows that node 17 listens to `input`; Go
  keeps a table from (node, event) to the current handler, overwritten on
  each render. Only adding or removing a listener is a patch.
- DOM nodes have integer ids, and JS keeps them in a flat array. Patches
  are opcodes over ids (create, set attribute, insert, remove, move, …),
  with a string table per batch.
- JSON only where it's cold: loading, errors and effect requests. Events
  in and patches out are binary.
- The deadline polls the clock every so many calls. Go on wasm can't
  preempt a goroutine, so a `context` deadline would never fire inside a
  loop.
- Labels are interned per environment, so keeping the model across a
  reload means reusing the environment.

## PRs

Useful on their own, so they land first:

1. [x] **Checker:** let a sort binding name a type scrap that takes
   arguments, as `web : type -> type -> type = $"./web.scrap"` (#23).
2. [x] **Checker:** let a binding without an annotation name a type, as
   `web = $"./web.scrap"` (#24).
3. [x] **Hosts without the network:** bindings from `scrap-gen-go
   -main=false` import `host`, which imports `yards` and so `net/http`, into
   every WebAssembly host. Move the command line into its own package, which
   only the generated `main.go` imports, and the HTTP client into
   `yards/httpyard` (#26).
4. [x] **Evaluator deadline:** cancel an evaluation that runs past a
   deadline, polling the clock every so many calls; measure with benchstat.
   Settles the open question in SPEC.md section 13, for `scrap eval`, the
   terminal host and the tutorial's REPL as much as for the web (#27). In
   WebAssembly, hosts must not start Go timers, as `context.WithTimeout`
   does: the runtime would wait on them through WASI's `poll_oneoff`, which
   our shim doesn't implement.
5. [x] **Generic aliases in bindings:** `scrap-gen-go` rejects an alias used
   at more than one type, such as `response text` and `response json`. Name
   each use by its arguments instead: `ResponseText`, `ResponseJson` (#25).
6. [ ] **`js/` workspace:** move the wasm loader and its WASI shim,
   `scrap-wasm.ts`, out of `web/` into an npm workspace that `web/` uses.
   No change in behaviour, besides the REPL passing a timeout. Also:
   scrap-wasm's `sources` can be `yards.Validate(yards.ByMap(…))`.
6b. [ ] **Call depth:** a deadline doesn't stop a non-tail recursion, such as
   `f = n -> 1 + f n`, from overflowing the stack: natively Go's, fatally
   after about 6 s; in WebAssembly the engine's, within milliseconds. Limit
   the depth of non-tail calls, so that it fails as an error: 100,000
   natively, 1,000 in WebAssembly, where a Web Worker's stack holds about
   2,400 and failing takes about as much again (#28).

The web platform:

7. [ ] **Interface and session:** `web.scrap`, its generated bindings, and
   a Go session that runs `init` and `update` and collects commands and
   subscriptions. Tested in Go. Sketch the platform in SPEC.md section 13.
8. [ ] **Diff:** views to binary patches, unkeyed, with the opcode format
   documented and the handler table. Tested against a mock DOM in Go:
   applying `diff(a, b)` to the DOM of `a` gives the DOM of `b`.
9. [ ] **Keyed children** in the diff.
10. [ ] **Runtime:** in the `js/` workspace, the patch interpreter, event
    delegation, and `mount(element, { source, flags, fetchScrap })`.
    Tested in Chromium.
11. [ ] **HTTP**, and `json` both ways.
12. [ ] **Time, animation frames and random**, with subscriptions diffed.
13. [ ] **Playground** page in `web/`: an editor and the running app,
    errors, scraps fetched from the yard, and the deadline.
14. [ ] **Hot reload** that keeps the model if its type is unchanged.
15. [ ] **Publish** to npm, with a README for embedders.

To check on the way: an earlier attempt measured building a list with `+<`
in a fold as growing faster than linearly, which views built that way would
feel. Unconfirmed.
