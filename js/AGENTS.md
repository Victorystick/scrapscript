# Runtime

The web runtime, `@scrapscript/web` (a working name), to be published to
npm. web/ uses it through the pnpm workspace. See WEB_PLAN.md for what
comes next: the Elm-style platform, its patch interpreter and `mount`.

- `scrap-wasm.wasm` is the scrap tool as WebAssembly, built from
  `cmd/scrap-wasm` by `pnpm build`, so building needs Go.
- `scrap-wasm.ts` runs it, with just enough of WASI for Go, in the browser
  and in Node. `wasmURL` is where the module is, which bundlers such as
  Vite follow. Start a `ScrapWasm` once and reuse it: starting the Go
  runtime takes far longer than a request.
- In WebAssembly, Go must not start timers (`context.WithTimeout`, …): the
  runtime would wait on them through WASI's `poll_oneoff`, which the shim
  doesn't implement, and crash at the next GC.
- The package exports its TypeScript source until it is published, when
  a build to JavaScript and declarations comes in.
