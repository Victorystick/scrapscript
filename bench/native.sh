#!/bin/sh
# Compiles BenchmarkEval's programs to native code, as scrap build does, and
# runs them, saving the output as bench/<date>-<commit>-native.txt, whose
# BenchmarkEval benchstat compares with run.sh's. COUNT sets the runs of
# each, as for go test's -count.
set -eu
cd "$(dirname "$0")/.."
rev=$(git rev-parse --short HEAD)
git diff --quiet HEAD -- . ':!bench' || rev="$rev-dirty"
out="bench/$(date +%Y-%m-%d)-$rev-native.txt"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
NATIVE_BENCH_DIR="$dir" go test ./eval -run '^TestNativeBenchGen$' -count 1 >/dev/null
(cd "$dir" && go test -run '^$' -bench . -benchmem -count "${COUNT:-10}" -cpu 1) | tee "$out"
echo "wrote $out"
