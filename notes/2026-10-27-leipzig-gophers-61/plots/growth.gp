# Average new packages per month, by year: data/growth-per-month.tsv, from
# scripts/growth.py. The values of 2024 and the current year are labelled.

load "plots/style.gp"
set output "img/growth.png"

latest = system("awk '/^# latest/ {print $3, $4}' data/growth-per-month.tsv")

set title "Average new packages per month"
set border 3
set xtics nomirror scale 0 ("2022" 2022, "2023" 2023, "2024" 2024, "2025" 2025)
set xtics add (sprintf("2026, to %s", latest) 2026)
set ytics nomirror out
set grid ytics back lc rgb grid
set xrange [2021.5:2026.5]
set yrange [0:*]
set ylabel "thousands"
set key top left reverse Left samplen 1.5 spacing 1.2

bw = 0.27
set boxwidth bw - 0.03 absolute
lab(year, v) = (year == 2024 || year == 2026) ? sprintf("%.1fk", v / 1000) : ""

plot "data/growth-per-month.tsv" \
	   using ($1 - bw):($2 / 1000) with boxes lc rgb blue title "PyPI", \
	"" using 1:($3 / 1000) with boxes lc rgb clay title "crates.io", \
	"" using ($1 + bw):($4 / 1000) with boxes lc rgb green title "Packagist", \
	"" using ($1 - bw):($2 / 1000):(lab($1, $2)) with labels offset 0,0.7 font ",15" textcolor rgb ink notitle, \
	"" using 1:($3 / 1000):(lab($1, $3)) with labels offset 0,0.7 font ",15" textcolor rgb ink notitle, \
	"" using ($1 + bw):($4 / 1000):(lab($1, $4)) with labels offset 0,0.7 font ",15" textcolor rgb ink notitle
