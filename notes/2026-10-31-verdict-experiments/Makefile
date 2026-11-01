# Verdict experiments; see README.md.
#
#   make record   search the live registries for every idea in ideas.tsv
#                 (once; resumes where it stopped, keeps what is recorded)
#   make e1       summarise the recorded data into results/e1-*.tsv
#   make sheets   write the labelling sheets (never overwrites one)
#   make labels   read the filled-in sheets into data/pairs.tsv and
#                 data/levels.tsv, check them, report the agreement
#   make prompts  write silt's verdict prompt for every idea (once; never
#                 overwrites data/prompts.jsonl)
#   make llm      ask an LLM for every verdict, appending to
#                 data/llm-<model>.jsonl (resumes; LLM_API, LLM_URL, LLM_MODEL)
#   make e2       compare the floors and the LLMs with the level labels

PYTHON ?= python3 -I
LLM_API ?= ollama
LLM_URL ?= http://strix:11434
LLM_MODEL ?= qwen3.5:9b

.PHONY: all
all: e1

.PHONY: record
record:
	./scripts/record.sh

.PHONY: e1
e1: results/e1-strata.tsv

results/e1-strata.tsv results/e1-ideas.tsv: scripts/e1.py data/runs.jsonl ideas.tsv
	$(PYTHON) scripts/e1.py

.PHONY: sheets
sheets: labels/claude.txt labels/human.txt

labels/claude.txt:
	$(PYTHON) scripts/labelsheet.py $@

labels/human.txt:
	$(PYTHON) scripts/labelsheet.py --sample 4 $@

.PHONY: labels
labels:
	$(PYTHON) scripts/labels.py

# No prerequisites on purpose: the prompt renders the age of a match relative
# to the time it is built, so it is built once and every LLM reads the same
# text. Delete the file to build it again.
.PHONY: prompts
prompts: data/prompts.jsonl

data/prompts.jsonl:
	go run ./scripts/prompts < data/runs.jsonl > $@.tmp && mv $@.tmp $@

.PHONY: llm
llm: data/prompts.jsonl
	$(PYTHON) scripts/llm.py $(LLM_API) $(LLM_URL) $(LLM_MODEL)

.PHONY: e2
e2:
	$(PYTHON) scripts/e2.py
