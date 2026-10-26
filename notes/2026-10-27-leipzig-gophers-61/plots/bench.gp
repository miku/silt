# Seconds to embed 300 texts, per matrix kernel: data/bench-medians.tsv,
# from scripts/bench.sh and scripts/bench-medians.sh. The assembly is clay,
# the rest blue.

load "plots/style.gp"
set output "img/bench.png"
set datafile separator "\t"

runs = system("grep -c '^BenchmarkKernel/kernel=asm' data/bench.txt")
cpu = system("sed -n 's/^cpu: //p' data/bench.txt | head -1")
stats "data/bench-medians.tsv" using 2 nooutput

set title sprintf("Embedding 300 texts, median of %s runs\n{/=16 %s}", runs, cpu) enhanced
set border 1
set xtics nomirror out
set ytics nomirror scale 0 textcolor rgb ink
set grid xtics back lc rgb grid
set xrange [0:STATS_max * 1.15]
set yrange [STATS_records - 0.4:-0.6]
set xlabel "seconds (less is better)"
unset key

plot "data/bench-medians.tsv" \
	using ($2 / 2):0:($2 / 2):(0.36):3:yticlabels(1) with boxxyerror lc rgb variable, \
	"" using 2:0:(sprintf("%.2f s", $2)) with labels left offset 0.5,0 textcolor rgb ink
