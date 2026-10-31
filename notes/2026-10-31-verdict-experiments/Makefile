# Verdict experiments; see README.md.
#
#   make record   search the live registries for every idea in ideas.tsv
#                 (once; resumes where it stopped, keeps what is recorded)
#   make e1       summarise the recorded data into results/e1-*.tsv
#   make sheets   write the labelling sheets (never overwrites one)

PYTHON ?= python3 -I

.PHONY: all
all: e1

.PHONY: record
record:
	./scripts/record.sh

.PHONY: e1
e1: results/e1-strata.tsv

results/e1-strata.tsv results/e1-ideas.tsv: scripts/e1.py data/runs.jsonl
	$(PYTHON) scripts/e1.py

.PHONY: sheets
sheets: labels/claude.txt labels/human.txt

labels/claude.txt:
	$(PYTHON) scripts/labelsheet.py $@

labels/human.txt:
	$(PYTHON) scripts/labelsheet.py --sample 3 $@
