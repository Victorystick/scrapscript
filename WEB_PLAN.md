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

1. [x] **Checker:** let a sort binding name a type scrap that takes
   arguments, as `web : type -> type -> type = $"./web.scrap"` (#23).
   Follow-up: without the annotation, `web = $"./web.scrap"` (#24).
2. [ ] **Evaluator deadline:** cancel an evaluation that runs past a
   deadline, polling the clock every so many calls; measure with benchstat.
   Settles the open question in SPEC.md section 13.
3. [ ] **Interface and session:** `web.scrap`, its generated bindings, and
   a Go session that runs `init` and `update` and collects commands and
   subscriptions. Tested in Go. Sketch the platform in SPEC.md section 13.
4. [ ] **Diff:** views to binary patches, unkeyed, with the opcode format
   documented and the handler table. Tested against a mock DOM in Go:
   applying `diff(a, b)` to the DOM of `a` gives the DOM of `b`.
5. [ ] **Keyed children** in the diff.
6. [ ] **JS package:** the `js/` workspace with the wasm loader (the WASI
   shim moves out of `web/`), the patch interpreter, event delegation, and
   `mount(element, { source, flags, fetchScrap })`. Tested in Chromium.
7. [ ] **HTTP**, and `json` both ways.
8. [ ] **Time, animation frames and random**, with subscriptions diffed.
9. [ ] **Playground** page in `web/`: an editor and the running app,
   errors, scraps fetched from the yard, and the deadline.
10. [ ] **Hot reload** that keeps the model if its type is unchanged.
11. [ ] **Publish** to npm, with a README for embedders.
