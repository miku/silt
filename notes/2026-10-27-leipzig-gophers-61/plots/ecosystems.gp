# Packages per registry, the ten largest: data/ecosystems-top10.tsv, from
# scripts/fetch-ecosystems.sh.

load "plots/style.gp"
set output "img/ecosystems.png"
set datafile separator "\t"

fetched = system("awk '/^# fetched/ {print $3}' data/ecosystems-top10.tsv")
stats "data/ecosystems-top10.tsv" using 2 nooutput

set title sprintf("Packages per registry, the ten largest (ecosyste.ms, %s)", fetched)
set border 1
set xtics nomirror out
set ytics nomirror scale 0 textcolor rgb ink
set grid xtics back lc rgb grid
set xrange [0:STATS_max / 1e6 * 1.15]
set yrange [9.6:-0.6]
set xlabel "packages, millions"
unset key

fmt(n) = n >= 1e6 ? sprintf("%.2fM", n / 1e6) : sprintf("%.0fk", n / 1e3)

plot "data/ecosystems-top10.tsv" \
	using ($2 / 2e6):0:($2 / 2e6):(0.36):yticlabels(1) with boxxyerror lc rgb blue, \
	"" using ($2 / 1e6):0:(fmt($2)) with labels left offset 0.5,0 textcolor rgb ink
