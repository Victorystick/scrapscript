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
| `AccessLog` | A realistic program, in `cmd/example/scrap-terminal`: per-path stats of 10k access-log lines in an AVL tree, loaded through the generated bindings and run by the example's `Run` loop. |

`Eval` evaluates the root scrap and the drafts it refers to each time; until
`1570ae6` it evaluated only the root, with the drafts' values cached.

## Milestones

Medians of 10 runs, Apple M3 Pro, `-cpu 1`.

| Commit | Change | Eval/fib | Eval/fold | Eval/echo | Eval allocs (geomean) | Infer | Read | AccessLog |
|---|---|---|---|---|---|---|---|---|
| `149eeed` | Baseline: tree-walking eval over the AST, map frames | 1.18 ms | 3.09 ms | 4.59 ms | 27.3k | 1.04 ms | 49.6 µs | — |
| `7a4d0e0` | Eval runs the compiled core: slot frames, match writes slots, failures panic | 0.31 ms | 1.38 ms | 2.12 ms | 13.9k | 1.02 ms | 49.1 µs | — |
| `283734e` | No runtime type checks; tail calls run in constant stack | 0.31 ms | 1.31 ms | 1.51 ms | 13.9k | 1.00 ms | 48.8 µs | — |
| `f23f0bc` | Static exhaustiveness checks; `Infer` source gains two catch-all arms | 0.28 ms | 1.25 ms | 1.47 ms | 13.9k | 1.02 ms | 48.2 µs | — |
| `03895d5` | Tags and labels interned per environment; records −7%, tree −8% vs. an interleaved baseline, others unchanged (this run's absolute times drifted ~3% up) | 0.31 ms | 1.31 ms | 1.48 ms | 13.2k | 1.06 ms | 50.2 µs | — |
| `6990e04` | Interleaved baseline for the next two rows; adds the record benchmarks, so the allocation geomean is over a different set than the rows above | 0.31 ms | 1.32 ms | 1.45 ms | 14.6k | 1.04 ms | 50.1 µs | — |
| `3ea375b` | Records as value buffers under interned shapes; records −27%, rows −28%, destructure −31%, tree −13% | 0.31 ms | 1.34 ms | 1.43 ms | 14.6k | 1.03 ms | 49.6 µs | — |
| `bf7ceaf` | Multi-pattern arms, and functions called with all their arguments at once; loop −49%, echo −38%, fold −32%, text −17%; fib +20%, sum +17%, tree back to the baseline | 0.37 ms | 0.90 ms | 0.89 ms | 11.6k | 1.09 ms | 50.5 µs | — |
| `73332ec` | One-argument functions match their argument directly, without argument slots; vs. an interleaved `6990e04`: loop −54%, records −43%, echo −42%, fold −34%; fib +4%, sum +5%; front end from multi-pattern arms: Infer +8%, Parse +7%, Read +5% | 0.33 ms | 0.89 ms | 0.88 ms | 11.5k | 1.14 ms | 53.0 µs | — |
| `0d03cba` | Directly applied matches run in the current frame; builtins, and fold's and sort's callbacks, get their arguments without a slice; vs. `73332ec`: records −26%, fold −26%, echo −10%, text −9%, fib −5%; Read +5% (allocs +6%) from the builtins added in `7ee294b`. AccessLog interleaved at default `-cpu`: 61.6 → 54.4 ms, allocs −22% | 0.31 ms | 0.66 ms | 0.79 ms | 10.7k | 1.12 ms | 55.9 µs | 64.8 ms |
| `a692c47` | Since `0d03cba`: closures capture values flat (`6edcf1a`) and slot operands are read inline (`d4c03d6`): fold −24%, fib −19%, loop −14%, destructure-closed −14%, AccessLog −9% (allocs −35%), Eval bytes −25%. Read +13% (+29% bytes): presence fields `b9cb9d9` +11%, type members `165a05a`/`05f14e1` +4% (+88 allocs) | 0.25 ms | 0.50 ms | 0.75 ms | 8.26k | 1.15 ms | 63.3 µs | 58.8 ms |
