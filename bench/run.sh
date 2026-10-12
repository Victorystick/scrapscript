#!/bin/sh
# Runs the benchmarks and saves their output as bench/<date>-<commit>.txt,
# which benchstat compares. BENCH selects benchmarks and COUNT sets the runs
# of each, as for go test's -bench and -count.
set -eu
cd "$(dirname "$0")/.."
rev=$(git rev-parse --short HEAD)
git diff --quiet HEAD -- . ':!bench' || rev="$rev-dirty"
out="bench/$(date +%Y-%m-%d)-$rev.txt"
go test ./... -run '^$' -bench "${BENCH:-.}" -benchmem -count "${COUNT:-10}" -cpu 1 | tee "$out"
echo "wrote $out"
