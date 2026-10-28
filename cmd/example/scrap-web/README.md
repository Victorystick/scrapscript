# scrap-web

A host of the web platform: it runs a scrapscript app in a web page, in the
Elm architecture. Like [scrap-terminal](../scrap-terminal), it is an example
of a host built from bindings that `scrap-gen-go` generates from a platform's
interface (SPEC.md section 13). Unlike it, it is a WebAssembly module, which
a page asks to run the app, as the package `host/wasmhost` describes.

```sh
$ GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o scrap-web.wasm ./cmd/example/scrap-web
```

## Writing an app

An app is a scrap whose value has the type of the interface,
[app.scrap](app.scrap):

```scrap
; app   : type -> type -> type = model => msg => { init : model, update : msg -> model -> model, view : model -> html msg }
; html  : type -> type = msg => [#text text, #el { tag : text, attrs : list (attr msg), children : list (html msg) }]
; attr  : type -> type = msg => [#attr { name : text, value : text }, #on { event : text, msg : msg }, #on-value { event : text, msg : text -> msg }]
```

The app keeps its own model, of a type that it chooses and the host never
inspects, and its own messages. The page shows the `view` of `init`, and
then, for each event that an element's handler handles, the host passes
`update` the handler's message, or for `#on-value`, the message that it makes
of the value of the event's target, as an input's text, with the model, and
the page shows the view of the result.

```scrap
{ init = 0, update, view }
; update = | #inc -> n -> n + 1 | #dec -> n -> n - 1
; view = n -> #el { tag = "p", attrs = [], children =
  [ #el { tag = "button", attrs = [#on { event = "click", msg = #dec }], children = [#text "-"] }
  , #text (int/to-text n)
  , #el { tag = "button", attrs = [#on { event = "click", msg = #inc }], children = [#text "+"] }
  ] }
```

## Running it in a page

The host keeps the app, and its model, as values between requests, and the
view that the page shows, which it answers each request that changes it
with a patch of. Each request and response is JSON. A page:

1. loads the app, with the scraps that it refers to, as `wasmhost.Scraps`,
   optionally a scrap of the model to start from instead of `init`, and
   the messages, as scraps, to update it with, as the steps that it was
   sent gave them:

   ```json
   {"op": "load", "source": "$\"./main.scrap\"", "drafts": {"main.scrap": "…"}, "state": "{ count = 5 }", "messages": ["#inc", "#inc"]}
   ```

   The response is a patch of the view, `{"patch": […], "generation": 1,
   "replayed": 2}`, or `{"missing": […]}` with the hashes of the scraps to
   fetch and ask again with, or `{"error": "…"}`. It replays as many of the
   messages as are messages of the app and update it, which a page that
   keeps them loads a changed app with, as Elm's debugger replays them: so
   a change to `update` changes the model that they led to, and the steps
   go on, which the page can go back to. `"error"` says why it stopped
   before the last, if it did. `"step"` shows the model of a step before
   the last.

   A patch is a list of operations, which `dom.go` describes, on the nodes
   of the view, each of which has an id while the page shows it:

   ```json
   [0, 1, 0, 0, 1, "p", 0, 0, 3, 2, "button", 0, 1, "click", 1, 3, null, "-", …]
   ```

   clears the page's view, and inserts a `p` as node 1 into the body, node
   0, first, with a `button`, node 2, which listens for clicks, of the text
   `-`, and so on. The view is numbered, from the request's `"generation"`,
   1 by default. A new version of the app patches the view that the page
   shows, so that an input keeps its focus and text. An element with a
   `key` attribute is matched with its siblings in the last view by it,
   rather than by order, so that it moves, rather than another element
   changing to look like it.

2. sends the events that the view's elements handle, each with the
   generation of its view, its node, the event, and the value of its
   target, as an input's text, which an `#on-value` handler takes:

   ```json
   {"op": "send", "events": [{"generation": 1, "node": 2, "event": "click"}, {"generation": 1, "node": 4, "event": "input", "input": "milk"}]}
   ```

   The host updates the model with the message of each in turn, and
   answers with a patch to the view of the result, of the next generation,
   and the steps that the messages took, each message as a scrap: `{"patch":
   […], "generation": 2, "steps": [{"step": 1, "message": "#inc"}, …]}`. A
   page that sends the events that happen while the host is busy together
   gets one patch for them, as Elm draws once per frame. Those events are
   of views before the last, whose handlers the host keeps, as Elm uses the
   handlers that were on the page when an event happened. It ignores events
   of views older than the last 16, and answers with `{}` if all are.

3. asks for the model as a scrap, `{"op": "state"}`, as it needs it, or
   that of a step, `{"op": "state", "step": 1}`, to start from with fewer
   messages.

4. goes back to the model after a step, `{"op": "goto", "step": 1}`, for
   the last 1000 steps. The next event updates that model, and drops the
   steps after it.

5. asks for the whole view, `{"op": "view"}`, if it lost it, as when its
   frame reloaded.

A module that is new, as after a trap, answers `{"unloaded": true}` until it
loads an app, and its first patch clears the page's view. A page that loads
apps anew passes a `"generation"` after the last that it showed, so that it
can't mistake events of an earlier app's views for the new one's.

The page runs the module where it likes. Elm runs on the page's thread,
which an app that loops forever freezes; a page that runs apps as they are
being written, such as a playground, runs the module in a Web Worker
instead, which it can stop, and loads the app again with the messages that
it kept, but the last. [play](play) is such a playground.

## The playground

```sh
$ go run ./cmd/example/scrap-web/play
```

serves a playground on http://localhost:8080: a page of drafts of an app,
which it runs as a page runs the module, with just enough of WASI. It builds
`scrap-web.wasm` from the source each time the page loads it, and serves the
page's files, in `play`, as they are, so that reloading the page runs what
was edited, of the host or the page, and shows what `go build` printed if it
failed. It fetches the scraps that drafts refer to by hash from a scrapyard,
https://scraps.oseg.dev/ unless `-server` says otherwise.

- `play.js` is the page: it edits the drafts, runs the module in a worker,
  `worker.js`, which it stops if a request runs too long, shows the view in
  a sandboxed frame, and keeps the messages, which it loads a changed app
  with.
- `index.html` has the drafts that the page starts with, which
  `play_test.go` runs as the page does.

## Hosting another platform

The recipe is the same for any platform:

1. Write its interface, a type scrap such as `app.scrap`.
2. Generate its bindings, with `//go:generate go run ../../scrap-gen-go
   -main=false app.scrap`, which give its types as Go types, and `Load`,
   which checks a scrap against the interface once, and returns its value.
3. Write the effects by hand: here, `host.go` and `dom.go` turn views into
   patches for a page, and call `update` with the messages of the events
   that it sends back.
4. For a page, serve requests with `wasmhost.Serve`, and build with
   `GOOS=wasip1`.
