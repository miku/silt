# The shared look of the charts: a sand background that reads the same in
# light and dark mode, recessive axes and grid, and three series colors
# (blue, clay, green; checked for color-vision deficiencies).

set terminal pngcairo size 1400,780 font "DejaVu Sans,18" background rgb "#F3EEE4"
set encoding utf8

ink = "#1E2A2E"
muted = "#4A5355"
rule = "#8C8473"
grid = "#DDD5C6"
blue = "#2A72B8"
clay = "#C25A22"
green = "#3E8E5E"

set border lc rgb rule
set tics textcolor rgb muted
set xlabel textcolor rgb muted
set ylabel textcolor rgb muted
set title textcolor rgb ink font "DejaVu Sans,22"
set key textcolor rgb ink
set style fill solid 1.0 noborder
