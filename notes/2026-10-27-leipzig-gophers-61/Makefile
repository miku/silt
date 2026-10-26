# Data, charts and demo for the talk. The charts need gnuplot, the demo vhs.
#
#   make           charts from the data in data/ (offline)
#   make data      fetch registry counts and history, rerun the Docker Hub check
#   make bench     benchmark the matrix kernels (amd64 with AVX2, Go 1.26+)
#   make demo      record img/demo.gif with vhs (searches the live registries)

PYTHON ?= python3

.PHONY: all
all: charts

.PHONY: charts
charts: img/ecosystems.png img/growth.png img/bench.png

img/ecosystems.png: plots/ecosystems.gp plots/style.gp data/ecosystems-top10.tsv
	gnuplot plots/ecosystems.gp

data/growth-per-month.tsv: scripts/growth.py data/growth.tsv
	$(PYTHON) -I scripts/growth.py

img/growth.png: plots/growth.gp plots/style.gp data/growth-per-month.tsv
	gnuplot plots/growth.gp

data/bench-medians.tsv: scripts/bench-medians.sh data/bench.txt
	./scripts/bench-medians.sh

img/bench.png: plots/bench.gp plots/style.gp data/bench-medians.tsv data/bench.txt
	gnuplot plots/bench.gp

.PHONY: data
data:
	./scripts/fetch-ecosystems.sh
	./scripts/fetch-growth.sh
	./scripts/docker-hub.sh

.PHONY: bench
bench:
	./scripts/bench.sh

.PHONY: demo
demo:
	go build -o bin/silt ../../cmd/silt
	PATH="$(CURDIR)/bin:$$PATH" vhs demo.tape

.PHONY: clean
clean:
	rm -rf bin
