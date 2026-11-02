# Site

The site at https://victorystick.github.io/scrapscript/: the tutorial, the
playground, and a page about the yard. Pushes to `main` test it and deploy
it to GitHub Pages (`.github/workflows/pages.yml`).

- The yard, https://scraps.oseg.dev, stays a Cloudflare Worker in
  [Victorystick/cloudflare-scrapyard](https://github.com/Victorystick/cloudflare-scrapyard),
  which stores scraps and type checks them on push. It lets this site read
  scraps cross-origin.
- Examples on the pages must reference scraps that are in the yard. Run
  them verbatim against scraps.oseg.dev before shipping. `scrap hash` (or
  `sha256sum`) gives a scrap's key and `scrap push` uploads it.
- The site uses the runtime in `../js`, `@scrapscript/web`, through the
  pnpm workspace: `scrap-wasm.wasm`, the scrap tool as WebAssembly, and
  `scrap-wasm.ts`, which runs it. `pnpm build` and `pnpm test` build the
  wasm first, so building needs Go.
- The pages (`index.html`, `tutorial.html`, `play.html`), `style.css` and
  the browser's copy of `scrap-wasm.wasm` are built by Vite under the base path `/scrapscript/`,
  so links between pages are relative. A new page must be listed in
  `vite.config.ts`.
- `repl.ts` makes the `.example`s of a page editable and runs them in the
  browser: `repl-worker.ts` evaluates them (or runs their tests, with
  `data-run="test"`) with `scrap-wasm.wasm` in a Web Worker, fetching the
  scraps they refer to from the yard. Evaluating stops just before 5
  seconds, and the page stops the worker at 5 seconds. The `.example`s
  are highlighted with the CSS Custom Highlight API: building gives each
  its ranges as `data-highlights` (`highlightExamples` in
  `vite.config.ts`, with the wasm `highlight` command), `repl.ts`
  registers them by tree-sitter capture name and asks the worker again as
  they are edited, and `style.css` colours them with `::highlight()`.
- `tutorial.html` teaches the language as `cmd/scrap` implements it, which
  lags SPEC.md. Each `.example` is a scrap with what `scrap eval` and
  `scrap type` print, and `tutorial.test.ts` runs them all with the CLI
  built from this repository, and as the browser would. A change to what
  the CLI prints updates the tutorial in the same change: write an
  example, then paste in what the CLI prints.
- `pnpm dev` serves locally and `pnpm test` builds and tests.
