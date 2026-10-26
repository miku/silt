#!/usr/bin/env bash
#
# Benchmarks the matrix kernels of internal/minilm in one session: plain Go,
# the AVX2 assembly, and two simd/archsimd tiles from archsimd.patch. The
# patch is applied to a copy of the committed tree (REV, HEAD by default), so
# the working tree stays as it is.
#
# Needs amd64 with AVX2 and FMA, Go 1.26 or later (GOEXPERIMENT=simd), and the
# model in silt's cache: go run ./scripts/fetchmodel in the repository root.
#
# Writes data/bench.txt (go test output) and data/benchstat.txt.
set -euo pipefail
cd "$(dirname "$0")/.."
here=$PWD
root=$(git rev-parse --show-toplevel)
rev=${REV:-HEAD}
count=${COUNT:-6}
benchstat=${BENCHSTAT:-go run golang.org/x/perf/cmd/benchstat@v0.0.0-20261009192801-be2c69fb417e}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
git -C "$root" archive "$rev" | tar -x -C "$tmp"
git -C "$tmp" apply "$here/archsimd.patch"

cd "$tmp"
# Correctness first: every kernel against patent's embeddings.
GOEXPERIMENT=simd go test -run '^TestEmbeddingsMatchPatentWithKernel$' -v ./internal/minilm/ |
	grep -E -e '--- |worst'

# With -count, go test runs each sub-benchmark count times in a row, so a
# laptop that heats up and slows down penalises the later kernels. Run whole
# rounds instead, all kernels in each, from one test binary.
GOEXPERIMENT=simd go test -c -o "$tmp/minilm.test" ./internal/minilm/
cd internal/minilm
for i in $(seq "$count"); do
	"$tmp/minilm.test" -test.run '^$' -test.bench '^BenchmarkKernel$' -test.count 1 | grep -v '^PASS$'
done | tee "$here/data/bench.txt"

cd "$here"
$benchstat -col /kernel data/bench.txt | tee data/benchstat.txt
