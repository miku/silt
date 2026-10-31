# Verdict experiments

Started 2026-10-31. Runs experiments E1–E4 from
[2026-10-29-verdict.md](../2026-10-29-verdict.md). Where things stand:
[Status and next steps](#status-and-next-steps) at the end.

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
| `scripts/labels.py` | reads the sheets, checks them, reports the agreement; `make labels` |
| `data/pairs.tsv` | one line per labelled match: idea id, URL, label, labeller, note |
| `data/levels.tsv` | one line per level: idea id, level, labeller, the y/p/n counts, the level the y count gives |
| `results/labels-agreement.tsv` | agreement between labellers, per match and per level |

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

## Labels, round 1

The person's sheet is done (2026-10-31): 201 of 203 matches and 11 of 12
levels. What it shows is in `labels/human-notes.md`. Those notes stay out of
this file until Claude's sheet is done, so that Claude's labeller doesn't
see the person's labels. `make labels` reads the sheets. The agreement goes
here once both sheets are done.

## Status and next steps

As of 2026-10-31.

### Decided

- **Labels:** Claude labels every idea (`labels/claude.txt`); a person labels
  a random sample of 12 (`labels/human.txt`), and the agreement between the
  two is reported. Vague ideas get a level only, no per-match labels, since
  "does this implement 'a tool for developers'?" has no answer.
- **The level is the labeller's judgement** of how crowded the space is.
  The number of y (0, 1 to 4, 5 or more) is a guide, not the rule: the age,
  size and quality of the matches may move it. `data/levels.tsv` keeps the
  level as labelled and, as `from_y`, the level the y count gives. E4 uses
  the first as its target and reports the second alongside.
- **Claude's sheet is labelled in a new session** that hasn't seen the
  person's labels. This session had seen them.
- **Tools:** E3 and E4 in Python, offline, with `uv run` and dependencies
  pinned inline in each script (sentence-transformers, scikit-learn).
  Timings from Python are indicative only. Only a scorer that wins gets
  ported to Go (the cross-encoder can reuse `internal/minilm`).
- **LLM baseline (E2):** qwen3.5 on the homelab GPU if it serves the model;
  otherwise locally on the CPU (~3 min per idea), parity ideas only.
- **Location:** this directory; generated tables in `results/`.

### Done

- E1: ideas, recording, summary (above).
- Labelling sheets generated: `labels/claude.txt` (87 ideas, 1437 matches),
  `labels/human.txt` (12 ideas, 4 each from parity, positive and specific;
  203 matches).
- The person's sheet is labelled (see `labels/human-notes.md`).
- `scripts/labels.py` (`make labels`): reads the sheets into
  `data/pairs.tsv` and `data/levels.tsv`, flags unlabelled matches, missing
  levels and levels that don't fit the y count, and reports Cohen's kappa
  per match (y/p/n, y against the rest, y+p against n) and per level.

### Next

1. **Claude's sheet**, in a new session. Each `_` becomes y, p or n. Each
   `level:` gets open, crowded, saturated or vague, by the rules in the
   sheet's header. That session must not open `labels/human.txt`,
   `labels/human-notes.md`, `data/pairs.tsv`, `data/levels.tsv` or
   `results/labels-*`, and must not look at their git history. It checks
   its progress with `python3 -I scripts/labels.py labels/claude.txt`,
   because `make labels` also prints the checks of the person's sheet.
   The level of 049 in `labels/human.txt` is still empty; the person fills
   it in. Then `make labels` for the agreement on the 12 shared ideas, and
   `labels/human-notes.md` goes back into this file.
2. **E2, baselines.** Compare the floor (`embeddings.verdict.level`), the
   keyword floor (`keyword_only.verdict.level`) and qwen3.5 with the level
   labels. The LLM gets the prompt from `verdict.BuildPrompt` over the
   recorded matches, not a new search.
3. **E3, per-match scorers.** Rescore the recorded candidates with each
   scorer: MiniLM cosine (recorded), lexical IDF, bge-small and e5-small,
   the ms-marco cross-encoder (raw and Platt-calibrated), an NLI
   cross-encoder, Qwen3-Reranker-0.6B (homelab). Output per scorer:
   `scores/<scorer>.tsv` (idea id, URL, score, ms). Then **labels, round
   2**: each scorer's top 5 per idea that round 1 didn't cover gets
   labelled too, so that no scorer is judged only on the cosine's picks.
   Report AUC, NDCG@10 and ms per idea.
4. **E4, aggregators.** Features per idea from the best E3 scorer (see the
   verdict note, "(b) Aggregation into a level"); refit floor, ordinal
   logistic regression, depth-3 tree; leave-one-out cross-validation; the
   floor stays a lower bound. Report confusion matrices, QWK and false
   Opens against the floor.

### Things to know

- `scripts/record.sh` doesn't need to run again: `data/runs.jsonl` is the
  frozen data. It resumes and skips recorded ids if it does run. It uses
  its own cache in `work/` (ignored by git), with the model, catalogs and
  browser profile linked in from `~/.cache/silt`.
- The keyword-only run excludes the sources that failed in the embedding
  run, so both rank the same candidates. None failed in this recording.
- A rejected idea has an `error` field and no `embeddings`.
- The positives' targets are read from `ideas.tsv`, not from the
  recording, so they can be corrected without recording again.
- `labelsheet.py` never overwrites a sheet; delete one to regenerate it
  (only if it holds no labels).
- `data/runs.jsonl` is 4.2 MB; gzip it if that is too big for the
  repository (the scripts then need to read the .gz).
