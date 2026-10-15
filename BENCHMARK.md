# Benchmarks

Performance is tracked by saving the raw output of the benchmarks for the
commits that matter, and comparing them with
[benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat).

## Running

```sh
./bench/run.sh                      # all benchmarks, 10 runs each, about 2 minutes
BENCH=Eval COUNT=5 ./bench/run.sh   # a subset
```

The script writes `bench/<date>-<commit>.txt`, with `-dirty` added when the
working tree has changes. Commit the file along with the change it measures.

## Comparing

```sh
go install golang.org/x/perf/cmd/benchstat@latest
benchstat bench/2026-10-12-149eeed.txt bench/<new>.txt
```

benchstat reports the change in time, bytes and allocations per operation,
and marks changes that aren't significant with `~`. Compare only runs from
the same machine; each milestone below names it.

## The benchmarks

| Benchmark | Measures |
|---|---|
| `Scan` | The scanner, on about 4 MB of source. |
| `Parse` | The parser, on a 1 MB where-chain. |
| `Infer` | Type inference on 1000 bindings. |
| `Read` | The front end for a program and its drafts: loading, parsing and checking `Eval/echo`. |
| `Eval/fib` | Calls and integer arithmetic. |
| `Eval/loop` | Tail calls with an accumulator, 10000 deep. |
| `Eval/sum` | List patterns, recursing over 1000 elements. |
| `Eval/fold` | A builtin calling back into a closure, 10000 times. |
| `Eval/records` | Record spreads and field access. |
| `Eval/fields` | Wide record literals, destructured by closed patterns. |
| `Eval/rows` | One field access over records of three shapes, and a rest pattern. |
| `Eval/destructure` | A rest pattern whose rest is built but never used. |
| `Eval/destructure-closed` | The same with a closed pattern; the difference from `destructure` is the rest's cost. |
| `Eval/tree` | Tags of a recursive type, built and matched. |
| `Eval/text` | Text concatenation. |
| `Eval/echo` | A terminal-platform program, run on 1000 lines by its reference semantics. |

`Eval` evaluates only the root scrap each time; the values of the drafts it
refers to are cached.

## Milestones

Medians of 10 runs, Apple M3 Pro, `-cpu 1`.

| Commit | Change | Eval/fib | Eval/fold | Eval/echo | Eval allocs (geomean) | Infer | Read |
|---|---|---|---|---|---|---|---|
| `149eeed` | Baseline: tree-walking eval over the AST, map frames | 1.18 ms | 3.09 ms | 4.59 ms | 27.3k | 1.04 ms | 49.6 µs |
| `7a4d0e0` | Eval runs the compiled core: slot frames, match writes slots, failures panic | 0.31 ms | 1.38 ms | 2.12 ms | 13.9k | 1.02 ms | 49.1 µs |
| `283734e` | No runtime type checks; tail calls run in constant stack | 0.31 ms | 1.31 ms | 1.51 ms | 13.9k | 1.00 ms | 48.8 µs |
| `f23f0bc` | Static exhaustiveness checks; `Infer` source gains two catch-all arms | 0.28 ms | 1.25 ms | 1.47 ms | 13.9k | 1.02 ms | 48.2 µs |
| `03895d5` | Tags and labels interned per environment; records −7%, tree −8% vs. an interleaved baseline, others unchanged (this run's absolute times drifted ~3% up) | 0.31 ms | 1.31 ms | 1.48 ms | 13.2k | 1.06 ms | 50.2 µs |
