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

The host keeps the app, and its model, as values between requests. Each
request and response is JSON. A page:

1. loads the app, with the scraps that it refers to, as `wasmhost.Scraps`,
   and optionally a scrap of the model to start from instead of `init`:

   ```json
   {"op": "load", "source": "$\"./main.scrap\"", "drafts": {"main.scrap": "…"}, "state": "{ count = 5 }"}
   ```

   The response is the first view, or `{"missing": […]}` with the hashes of
   the scraps to fetch and ask again with, or `{"error": "…"}`. The view's
   text is a string, and each element is

   ```json
   {"tag": "button", "attrs": {"class": "…"}, "on": {"click": {"handler": 1}}, "children": […]}
   ```

   Each of its handlers has an index, and `"value": true` if it takes the
   value of the event's target. The response numbers the view, from the
   request's `"generation"`, 1 by default: `{"view": …, "generation": 1}`.

2. sends the events that the views' handlers handle, each with the
   generation of its view, and the value of its target if its handler
   takes one:

   ```json
   {"op": "send", "events": [{"generation": 1, "handler": 1}, {"generation": 1, "handler": 0, "input": "milk"}]}
   ```

   The host updates the model with the message of each in turn, and
   answers with the view of the result, of the next generation, and the
   steps that the messages took, each message as a scrap: `{"view": …,
   "generation": 2, "steps": [{"step": 1, "message": "#inc"}, …]}`. A page
   that sends the events that happen while the host is busy together gets
   one view for them, as Elm draws once per frame. Those events are of
   views before the last, whose handlers the host keeps, as Elm uses the
   handlers that were on the page when an event happened. It ignores events
   of views older than the last 16, and answers with `{}` if all are.

3. asks for the model as a scrap, `{"op": "state"}`, as it needs it: to
   keep it, or to load a new version of the app with it, which the host
   checks the model's type against.

4. goes back to the model after a step, `{"op": "goto", "step": 1}`, for
   the last 1000 steps.

A module that is new, as after a trap, answers `{"unloaded": true}` until it
loads an app. A page that loads apps anew passes a `"generation"` after the
last that it showed, so that it can't mistake events of an earlier app's
views for the new one's.

The page runs the module where it likes. Elm runs on the page's thread,
which an app that loops forever freezes; a page that runs apps as they are
being written, such as a playground, runs the module in a Web Worker
instead, which it can stop, and loads the app again from the last model that
it asked for. https://scraps.oseg.dev/web is such a playground; its source,
in [cloudflare-scrapyard](https://github.com/Victorystick/cloudflare-scrapyard),
has the page's side, with just enough of WASI to run the module.

## Hosting another platform

The recipe is the same for any platform:

1. Write its interface, a type scrap such as `app.scrap`.
2. Generate its bindings, with `//go:generate go run ../../scrap-gen-go
   -main=false app.scrap`, which give its types as Go types, and `Load`,
   which checks a scrap against the interface once, and returns its value.
3. Write the effects by hand: here, `host.go` turns views into JSON for a
   page, and calls `update` with the messages of the events that it sends
   back.
4. For a page, serve requests with `wasmhost.Serve`, and build with
   `GOOS=wasip1`.
