# Verdict experiments

Started 2026-10-31. Runs experiments E1–E4 from
[2026-10-29-verdict.md](../2026-10-29-verdict.md).

## The question

silt tells you whether your idea already exists: Open, Crowded or Saturated.
Today that answer comes from counting matches above some similarity
thresholds (the "floor"), or from a slow LLM. We want to know whether a fast
method without an LLM can do as well or better.

The problem has two parts:

- **(a) Is this match really the same thing as the idea?** For "a CLI that
  kills a process on a port", `kill-port` is the same thing; a general
  process monitor is only related. Today a similarity score decides this, and
  it mixes up "same topic" with "does the same job".
- **(b) Given the matches, how crowded is the space?** This turns the
  per-match answers into Open, Crowded or Saturated.

## What we do

1. **Gather data once.** Run silt on about 100 ideas and save everything it
   found. All later tests run on this saved data, so every method sees
   exactly the same matches and the results can be reproduced.
2. **Write down the right answers.** For each idea, mark its top matches as
   *implements*, *partial* or *no*. Also give each idea the level it should
   get, and flag ideas that are too vague to judge.
3. **Try several ways of answering (a)** and see which one agrees best with
   those marks.
4. **Try several ways of answering (b)** and see which gets the level right
   most often.
5. **Compare** everything against what silt does today: the floor and the
   LLM.

## What we measure

| question | measure | in plain words |
|---|---|---|
| (a) Does the method tell real implementations from look-alikes? | AUC | If you pick one real implementation and one non-implementation at random, how often does the method score the real one higher? 0.5 is a coin flip, 1.0 is perfect. |
| (a) Are the real implementations at the top of the list? | NDCG@10 | How good the top 10 looks, counting mistakes near the top more heavily. |
| (a), (b) Is it fast enough? | ms per idea | Time spent on one idea. |
| (b) Does it get the level right? | confusion matrix | A 3×3 table of the correct level against the predicted level, so you can see exactly which mistakes happen. |
| (b) How far off are the mistakes? | QWK | One number for agreement, where calling Saturated "Open" counts worse than calling it "Crowded". |
| (b) The worst mistake | false Opens | How often it says "nobody has built this" when someone has. A new method must not make this mistake more often than the floor does today. |
| labels | agreement between labellers | When two people label the same sample, how often they agree. This shows how much the labels can be trusted. |

## What we hope to learn

- Which per-match method works best.
- Whether a small model beats the floor's fixed thresholds.
- Whether the LLM is needed for the level at all.

## Files

| file | what |
|---|---|
| `ideas.tsv` | the ideas: id, stratum, target (for positives: the package the idea rewords), idea |
| `scripts/record.sh` | runs silt on every idea, once; `make record` |
| `data/runs.jsonl` | one line per idea: silt's JSON output with every candidate, twice (embeddings, keyword-only) |
| `scripts/e1.py` | summary of the data; `make e1` writes `results/e1-*.tsv` |
| `scripts/labelsheet.py` | writes a labelling sheet; `make sheets` |
| `labels/human.txt`, `labels/claude.txt` | labelling sheets: a sample of 12 ideas for a person, every idea for Claude |

## E1: the data set

Recorded 2026-10-31, 10:30–10:42 UTC, silt at 9550edd, `--fast --limit
10000`, with a result cache of its own, so no earlier search leaked in. Per
idea silt ran twice: with the embedding model against the live registries,
then with `--keyword-only` on the same results (from the cache the first run
filled), so both rankings see the same candidates.

97 ideas in four strata:

- **parity** (32): the reference ideas of `scripts/parity`, which include the
  22 of the differential test.
- **positive** (30): a well-known package's description in other words, e.g.
  "a fast recursive grep that skips files listed in .gitignore" for ripgrep.
  The package should turn up, and the level should be at least Crowded.
- **specific** (20): narrow ideas, some likely Open.
- **vague** (15): "a python library", "a database".

| stratum | ideas | rejected | candidates (median) | floor O/C/S | keyword floor O/C/S | package found | package in top 15 |
|---|---|---|---|---|---|---|---|
| parity | 32 | 0 | 86 | 3/9/20 | 9/5/18 | | |
| positive | 30 | 0 | 89 | 1/9/20 | 12/8/10 | 18/30 | 14/30 |
| specific | 20 | 0 | 92 | 3/7/10 | 13/6/1 | | |
| vague | 15 | 10 | 128 | 1/0/4 | 1/0/4 | | |
| all | 97 | 10 | 89 | 8/25/54 | 35/19/33 | | |

Per idea: `results/e1-ideas.tsv`.

What the data shows before any labelling:

- **Every source was reached in every run**, PyPI through CloakBrowser. So
  missing coverage doesn't explain anything in this data set.
- **silt turns down 10 of the 15 vague ideas itself** ("Too vague"). Of the
  5 that get through, 4 come out Saturated, which is true but tells you
  nothing.
- **Search, not judgement, limits the positives.** The reworded package was
  among the candidates for 18 of 30 ideas, and in the top 15 for 14. Missing
  entirely: rich, black, watchdog, clap, tokei, cobra, lazygit,
  Newtonsoft.Json, tldr, vaultwarden, cert-manager, nvd. No scorer can rank a
  package that wasn't retrieved, so for these ideas the best any judge can
  do is find other implementations.
- **The two floors disagree a lot**: 8 Open with embeddings, 35 with keyword
  scores, on the same candidates. The thresholds were set for cosines; the
  keyword fraction lives on another scale.
- **The floor's rule makes false Opens of its own.** "a rust crate for
  parsing marc21 library catalog records" has 4 candidates; the first,
  marc-rs (0.62), is exactly that. The floor says Open: one match ≥ 0.60
  counts only from 0.70.
