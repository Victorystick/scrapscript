#!/bin/bash
# Counts the instructions per op of the evaluator's benchmarks with
# cachegrind, and saves them as bench/<date>-<commit>.ir, which benchstat
# compares like bench/run.sh's output. Unlike times, the counts repeat on a
# noisy machine: within 1% for Eval, 0.1% for the AccessLogs.
#
# Each benchmark runs twice, for a few iterations and for more, and the
# difference is divided by the extra iterations, so setup cancels out. The
# collector stops the world (GODEBUG=gcstoptheworld=1), since otherwise
# when it marks depends on time, which makes counts vary by several
# percent; so write barriers during marking aren't counted. Instructions
# aren't time: cache misses and branch mispredictions are free here.
#
# BENCH selects benchmarks by regexp. Needs valgrind; takes a few minutes.
set -eu
cd "$(dirname "$0")/.."
root=$(pwd)
rev=$(git rev-parse --short HEAD)
git diff --quiet HEAD -- . ':!bench' || rev="$rev-dirty"
out="bench/$(date +%Y-%m-%d)-$rev.ir"
bin=$(mktemp -d)
trap 'rm -rf "$bin"' EXIT
go test -c -o "$bin/eval.test" ./eval
go test -c -o "$bin/terminal.test" ./cmd/example/scrap-terminal

# package, benchmark, fewer and more iterations. Infer is left out: it
# stops its timer for setup, which the counts would include.
benchmarks="
eval Eval/fib 20 480
eval Eval/loop 10 240
eval Eval/sum 50 1200
eval Eval/fold 10 240
eval Eval/records 30 720
eval Eval/fields 20 480
eval Eval/rows 20 480
eval Eval/destructure 20 480
eval Eval/destructure-closed 20 480
eval Eval/tree 20 480
eval Eval/text 20 480
eval Eval/echo 10 240
eval Read 50 1200
eval Compile/Eval 50 1200
eval Compile/AccessLogLib 10 240
terminal AccessLog 1 11
terminal AccessLogLib 1 3
"

count() { # package benchmark iterations
	local dir=$root/eval
	[ "$1" = terminal ] && dir=$root/cmd/example/scrap-terminal
	(cd "$dir" && GODEBUG=gcstoptheworld=1 GOMAXPROCS=1 valgrind --tool=cachegrind \
		--cache-sim=no --cachegrind-out-file=/dev/null "$bin/$1.test" -test.run '^$' \
		-test.bench "^Benchmark$2\$" -test.benchtime "$3x" -test.cpu 1 2>&1 |
		awk '/I *refs/ { gsub(",", "", $4); print $4 }')
}

one() { # package benchmark fewer more
	local a b
	a=$(count "$1" "$2" "$3")
	b=$(count "$1" "$2" "$4")
	echo "Benchmark$2 1 $(((b - a) / ($4 - $3))) instructions/op"
}
export -f count one
export root bin

echo "$benchmarks" | grep -E "^\S+ (${BENCH:-.*})( |$)" |
	xargs -P "$(nproc)" -L 1 bash -c 'one "$@"' _ | sort | tee "$out"
echo "wrote $out"
