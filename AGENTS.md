# Scrapscript

Work happens on `main`, which holds the data-oriented rewrite of the
implementation. Keep data in flat arrays indexed by node (the AST,
types, the core IR), not pointer trees. Prefer side tables and batch
allocations to per-node structs and maps, and don't build data no
consumer needs.
Measure, don't assume: compare BenchmarkInfer/BenchmarkEval (eval package)
against the base commit with interleaved runs and benchstat.

- SPEC.md is the target language spec and decides design questions; README.md
  is for users; TODO.md tracks architectural blemishes; BENCHMARK.md and
  bench/run.sh track performance.
- Design principles:
  - An import means the same as its source inlined in parentheses, so
    whatever works across imports must work locally too.
  - Types and type members are compile-time only: erased, never part of a
    value's runtime shape.
- Core packages stay free of os/net (enforced by eval/deps_test.go).
- Try programs with `go run ./cmd/scrap type < file.scrap` (or `eval`). The
  CLI reads stdin, and paths resolve from the current directory by
  default; override with `--root <dir>`.
- web/ holds the tutorial and playground, deployed to GitHub Pages; see
  web/AGENTS.md. Its tests run the tutorial's examples with cmd/scrap, so
  run `npm test` there when changing what the CLI prints.
- Never put links to agentic coding sessions (such as claude.ai/code
  session URLs) in PR descriptions or commit messages.
- Discuss design changes before implementing them; make one change at a time.
