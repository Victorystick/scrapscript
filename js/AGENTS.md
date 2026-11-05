# Runtime

The web runtime, `@scrapscript/web` (a working name), to be published to
npm. web/ uses it through the pnpm workspace. See WEB_PLAN.md for what
comes next.

- `mount(element, options)` (`mount.ts`), the package's main export, runs
  an app on the web platform (SPEC.md section 13) in an element. It
  fetches the scraps the app refers to with `fetchScrap`, from the yard by
  default, applies the patches that the app's views make to the page
  (their format is in `cmd/scrap-web/patch.go`), and passes events back:
  it listens to each event once, on the element, in the capture phase.
  Events update the app at once, and it renders once per animation frame.
  It carries out the app's `#http` commands with `fetch` after each step,
  and passes back each response as a step of its own; stopping the app
  aborts its requests.
- `scrap-web.wasm` is the web platform's host as WebAssembly, built from
  `cmd/scrap-web`; `scrap-web.ts` runs it, and `webWasmURL` is where it is.
  Pages share one `ScrapWeb`, which runs any number of apps.
- `scrap-wasm.wasm` is the scrap tool as WebAssembly, built from
  `cmd/scrap-wasm`, which checks and evaluates scraps; `scrap-wasm.ts` runs
  it, and `wasmURL` is where it is. They're `@scrapscript/web/scrap-wasm`,
  so that pages that only mount apps don't carry the module. Start a
  `ScrapWasm` once and reuse it: starting the Go runtime takes far longer
  than a request.
- `wasi.ts` is the WASI shim that both modules run with: just enough for
  Go, in the browser and in Node. Bundlers such as Vite follow the modules'
  URLs, `new URL(…, import.meta.url)`.
- `pnpm build` builds both modules, so building needs Go. `pnpm test`
  builds them and runs `mount.test.ts`, which mounts apps in Chromium with
  Playwright, on `test/index.html` as Vite serves it.
- In WebAssembly, Go must not start timers (`context.WithTimeout`, …): the
  runtime would wait on them through WASI's `poll_oneoff`, which the shim
  doesn't implement, and crash at the next GC. Use `host.Deadline`.
- The package exports its TypeScript source until it is published, when
  a build to JavaScript and declarations comes in.
