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
| `ScanLib` | The scanner, on the scraps in `lib/` repeated to about 4 MB: indented and commented, unlike `Scan`'s dense source. |
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
| `AccessLogLib` | The same program as `AccessLog`, but the version in `lib/`, which imports the parser and avl libraries and parses each line with parser combinators, one character at a time: a closure-heavy program, with many partial applications. |

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
| `7b430d4` | Since `a692c47`: types in the core (`1ae6853`, `1570ae6`), one core per program (`c588a9c`), and calls to non-recursive where-bound functions inlined as matches in the caller's frame (`7752d68`, `7146dca`, `7b430d4`). destructure-closed −22%, rows −18%, destructure −18%, fields −14%, tree −8%, AccessLog −18% (allocs −40%, bytes +3%); interleaved, inlining alone took AccessLog from 48.9 to 40.8 ms at default `-cpu`. `inlineMax` = 40 tokens from an interleaved sweep of 0–200: 120 is 5% faster, but at 160 frames grow and bytes rise 132% | 0.25 ms | 0.48 ms | 0.76 ms | 7.72k | 1.15 ms | 63.2 µs | 48.5 ms |
| `c8012f4` | Since `7b430d4`: slot reuse (`1ff5c9b`), direct calls from non-tail code (`be8cd99`), closures as the only evaluator (`1d432c7`), and records allocated together with their values (`c8012f4`). rows −32%, records −24%, destructure-closed −23%, destructure −23%, AccessLog −19% (allocs −15%), loop −17%, fields −14%, echo −11%. Interleaved against `1d432c7`, records alone: rows −22%, records −14%, destructure −13%, Eval allocs −11% | 0.24 ms | 0.45 ms | 0.67 ms | 7.01k | 1.15 ms | 63.8 µs | 39.5 ms |
| `49dc53f` | Since `c8012f4`: no equality of values (`9f5545c`), hosts' `eval.Value` a struct (`f109fb9`), both neutral, and values as two words, with scalars and payloadless tags unboxed and list tails unallocated (`49dc53f`). sum −35%, fields −27%, loop −23%, destructure-closed −23%, fold −22%, tree −20%, AccessLog −11% (allocs −18%), Eval allocs −45%. Interleaved against `f109fb9`: Eval −21% (geomean), allocs −50%; AccessLog −13% | 0.21 ms | 0.35 ms | 0.58 ms | 3.86k | 1.16 ms | 64.2 µs | 35.3 ms |
| `1dcd351` | Records hold their shape's ID in the value instead of a header: values are one plain allocation, laid out as a list's. Interleaved against `0d8206c`: Eval −1.8% (geomean), bytes −15%, allocs unchanged; rows −7%, destructure −7%, destructure-closed −5%, tree −3%; AccessLog −2.7%, bytes −7.6% | 0.21 ms | 0.35 ms | 0.57 ms | 3.86k | 1.15 ms | 63.3 µs | 34.3 ms |
| `159e64b` | Since `1dcd351`: partial applications of script functions take all a call's arguments at once (`7997ef5`) and are one allocation with their arguments (`159e64b`); adds `AccessLogLib`, 462 ms in this run (±14% at `-cpu 1`). Interleaved against `c1d5691`, the two together: AccessLogLib −12% time, −23% allocs; AccessLog −2% time, −9% allocs; Eval unchanged | 0.21 ms | 0.35 ms | 0.59 ms | 3.86k | 1.16 ms | 64.0 µs | 33.7 ms |

## Native code

`bench/native.sh` compiles `Eval`'s programs to native code, as `scrap build`
does, and runs them as a `BenchmarkEval` of their own, saved as
`bench/<date>-<commit>-native.txt`. Each run evaluates the program from its
root, as `Eval` does.

Against the evaluator on the same commit, medians of 5 interleaved runs,
Intel Xeon @ 2.80 GHz (a cloud container, so slower than the M3 above),
`-cpu 1`:

| Program | Evaluator | Native | Speedup | Allocs (eval → native) |
|---|---|---|---|---|
| fib | 503 µs | 32.6 µs | 15.4× | 5188 → 20 |
| loop | 1270 µs | 7.5 µs | 169× | 10004 → 2 |
| sum | 151 µs | 23.0 µs | 6.6× | 1005 → 3 |
| fold | 838 µs | 146 µs | 5.8× | 10001 → 2 |
| records | 245 µs | 67.4 µs | 3.6× | 2005 → 1003 |
| fields | 442 µs | 11.2 µs | 39.5× | 2004 → 2 |
| rows | 656 µs | 271 µs | 2.4× | 6004 → 5002 |
| destructure | 372 µs | 127 µs | 2.9× | 3004 → 2002 |
| destructure-closed | 275 µs | 4.2 µs | 65.8× | 2004 → 2 |
| tree | 536 µs | 107 µs | 5.0× | 6145 → 2049 |
| text | 312 µs | 239 µs | 1.3× | 2001 → 1001 |
| echo | 1413 µs | 1048 µs | 1.35× | 9024 → 6015 |

Geomean 7.9×. Frames are Go variables, so calls allocate nothing, and the Go
compiler inlines direct calls of known lambdas: in fields and
destructure-closed, records that don't escape stay on the stack. Programs
that allocate their values anyway, such as text's concatenation, or that
call through closures they don't know, such as echo's step function, gain
least. The `lib/` access-log, run on 10k lines by the reference semantics
of `Eval/echo`, took 1.24 s with `scrap eval`, parsing and checking
included, and 0.54 s as a program from `scrap build`.
