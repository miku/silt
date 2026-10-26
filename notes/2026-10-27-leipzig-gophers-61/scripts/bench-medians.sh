#!/usr/bin/env bash
#
# Median seconds per kernel from data/bench.txt, slowest first, for
# plots/bench.gp. Writes data/bench-medians.tsv.
set -euo pipefail
cd "$(dirname "$0")/.."

{
	printf '# kernel\tseconds\tcolor\n'
	grep '^BenchmarkKernel/kernel=' data/bench.txt |
		awk '{ k = $1; sub(/^BenchmarkKernel\/kernel=/, "", k); sub(/-[0-9]+$/, "", k); print k, $3 / 1e9 }' |
		sort -k1,1 -k2,2g |
		awk '
			{ v[$1, ++n[$1]] = $2 }
			END {
				label["go"] = "plain Go"
				label["asm"] = "assembly, 4×24"
				label["archsimd16"] = "archsimd, 4×16"
				label["archsimd24"] = "archsimd, 4×24"
				for (k in n) {
					c = n[k]
					m = c % 2 ? v[k, (c + 1) / 2] : (v[k, c / 2] + v[k, c / 2 + 1]) / 2
					# the assembly in clay, the rest in blue
					printf "%s\t%.3f\t%d\n", (k in label ? label[k] : k), m, k == "asm" ? 12737058 : 2781880
				}
			}' |
		sort -t "$(printf '\t')" -k2,2gr
} >data/bench-medians.tsv

cat data/bench-medians.tsv
