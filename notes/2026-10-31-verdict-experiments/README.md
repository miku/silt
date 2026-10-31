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

Both sheets are done (2026-10-31). A person labelled a random sample of 12
ideas, 4 each from parity, positive and specific. Claude labelled all 87
ideas silt didn't turn down, in a session that hadn't seen the person's
labels. `make labels` reads the sheets into `data/pairs.tsv` and
`data/levels.tsv` and writes the agreement to `results/labels-agreement.tsv`.

| sheet | ideas | matches labelled | y/p/n | levels O/C/S/vague |
|---|---|---|---|---|
| person (`labels/human.txt`) | 12 | 201 of 203 | 97/48/56 | 1/6/4/1 |
| Claude (`labels/claude.txt`) | 87 | 1224 of 1437 | 334/282/608 | 14/28/33/12 |

Claude's 213 unlabelled matches belong to the 12 ideas it called vague,
which get a level only: the 5 vague ideas silt let through, and 7 parity
ideas that name a stack but no job ("a python pandas data pipeline", "a helm
chart to deploy a kubernetes operator", "rust crate for embedded firmware",
"anything at all with no signal", ...). Six of Claude's levels depart from
the y-count guide, each with a note: 044 and 058 go up because the y
matches are the standard tools (cloc and scc; GitLens and Git Blame); 045,
046, 050 and 075 go down because mirrors and forks of one project make up
most of their y.

### The 12 shared ideas

| idea | | person | y/p/n | Claude | y/p/n |
|---|---|---|---|---|---|
| 005 | CLI tool that kills a process on a given port | saturated | 13/2/2 | saturated | 14/1/2 |
| 016 | a vscode extension for markdown preview | saturated | 9/5/0 | saturated | 6/8/1 |
| 020 | a helm chart to deploy a kubernetes operator | vague | 16/2/0 | vague | |
| 025 | rust crate for embedded firmware | crowded | 8/8/0 | **vague** | |
| 034 | load environment variables from a .env file in a node app | saturated | 11/1/5 | saturated | 9/7/1 |
| 039 | a friendly command line http client for trying out apis | crowded | 2/1/15 | crowded | 3/2/13 |
| 040 | a python library that watches a directory for file changes | crowded | 4/6/5 | **saturated** | 5/6/4 |
| 049 | user sign up and login for a rails application | crowded | 7/7/2 | **saturated** | 6/8/3 |
| 066 | a vscode extension that estimates the carbon footprint of a cloud deployment | open | 0/3/14 | open | 0/3/14 |
| 068 | a python library to read and write gpx track files | crowded | 4/7/6 | crowded | 2/6/9 |
| 070 | an elixir library for rate limiting with a token bucket | crowded | 9/3/4 | **saturated** | 8/8/0 |
| 078 | a command line tool that checks a bibliography for retracted papers | saturated | 14/3/3 | saturated | 11/5/4 |

What the person's labelling showed:

- **Often really good**: half the matches shown implement the idea, and for
  005 (port killers) and 078 (retraction checkers) nearly all do.
- **The partial matches have value of their own.** Several are a nearby
  project the user may not have thought of at all. That argues for showing
  them, not only the implementations.
- **Some lists are off entirely, carried by a generic word.** 039: 15 of 18
  are no ("command line", "http client": a Minecraft modpack converter, an
  API gateway, a Cardano wallet). 066: 14 of 17 are no, mostly popular
  vscode extensions with nothing about carbon.
- **Spaces are crowded with near-copies.** 14 retraction checkers for 078,
  13 port killers for 005. The pace of code publication shows: all 7 dated
  y matches of 078 were updated in 2026, most with 0 to 15 stars. The
  recording has only the date of the last update, not of creation, so the
  pace can't be measured from it.
- **The level is a judgement, not a count.** 025 (8 y), 049 (7 y) and 070
  (9 y) are crowded, although the guide says 5 or more y make saturated.
  Decided: the labeller's level stands; `data/levels.tsv` keeps the level
  the y count gives as `from_y`, a second target.

### Agreement

| measure | n | agree | kappa |
|---|---|---|---|
| match y/p/n | 167 | 0.71 | 0.56 |
| match y vs p+n | 167 | 0.83 | 0.65 |
| match y+p vs n | 167 | 0.85 | 0.66 |
| level | 12 | 0.67 | 0.52 |

The 167 matches are the person's 201 without the 34 of 020 and 025, which
Claude called vague and left unlabelled.

| Claude \ person | y | p | n |
|---|---|---|---|
| y | 54 | 6 | 3 |
| p | 18 | 23 | 12 |
| n | 1 | 9 | 41 |

**Where the match labels differ.** Only 4 of the 49 disagreements are y
against n; 45 have a p on one side, and most of those come from where each
labeller puts p:

- **A variant in another form, or with a part not stated: Claude p, the
  person y (18).** For a CLI idea, an MCP server or a LaTeX language
  server; for a VS Code idea, a JetBrains plugin or a style for the built-in
  preview; a C++ watcher with Python bindings; a Rails starter app with
  Devise; a Gleam library on Hex for an Elixir idea; tools whose description
  doesn't mention one part of the idea (the token-bucket algorithm, the
  retraction check, killing the process). Claude marked the gap; the
  person counted the tool as doing the job.
- **The same job in another language or protocol: Claude p, the person n
  (12).** dotenv for Java, Kotlin, Go and Dart against a Node idea; Bucket4j
  (Java, four HN posts) against an Elixir one; gpxgo against a Python one;
  a GUI HTTP client, grpcurl, a FIT-to-GPX converter.
- **A nearby tool on the same data: Claude n, the person p (9).** Five GPX
  tools that use GPX files but aren't libraries for them (a map viewer, an
  animator, a renamer, gpx2exif, a GPS extractor), a Markdown-to-PDF
  converter, timelimit-cli, any_login, a graph of retracted papers. This is
  the person's "nearby project the user may not have thought of".
- **Case by case (10).** Claude y, the person p: snipe-port (kills only the
  project's own processes), a client for `.http` files, watchit,
  passwordless, ralitobu's Plug, DONTCITEME. Claude y, the person n:
  checkport, @expo/env, wotw. Claude n, the person y: cross-env. checkport
  (a port killer by its description) and cross-env (sets variables, reads
  no `.env`) are worth a second look if the labels are ever adjudicated.

So the person's y is wider (73 against 63) and Claude's p is wider (53
against 38). Claude's p held both "the same job with a gap" and "the same
job elsewhere"; the person's p means "near the idea, worth seeing".

**Where the levels differ** (4 of 12):

- **025**: Claude vague, the person crowded. The line between a stack-only
  idea and a judgeable one; the person called 020 vague too, but labelled
  its matches.
- **040**: Claude 5 y, saturated; the person 4 y, crowded. Both follow the
  guide; the match labels (watchit, wotw, e-dant/watcher) decide.
- **049 and 070**: the person crowded with 7 and 9 y, Claude saturated with
  6 and 8 y. The person's bar for saturated is higher than the guide's 5:
  their saturated ideas have 9 to 14 y, their crowded ones up to 9. Claude
  followed the guide on all 12 (its six departures are outside the sample).

Open against the rest, the labellers agree on all 10 ideas both gave a
level other than vague: 066 open, the other 9 not. On the 10 ideas both
labelled match by match, the levels the y counts give agree on 9, the
labelled levels on 7.

**What this means for the experiments:**

- The open/not-open line, on which false Opens rest, is where the labels
  agree; the crowded/saturated line is where they don't, mostly over how
  many y make saturated. So false Opens and QWK say more than exact
  accuracy, and `from_y`, the more reproducible target here, belongs next
  to the labelled level in every report.
- For E3, y vs p+n and y+p vs n are about equally reliable (kappa 0.65 and
  0.66), both more than y/p/n. With labellers agreeing on 83 to 85% of
  matches, scorers a few AUC points apart may not really differ.
- Round 2 needs one meaning of p, written into the sheet header (see Next).

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
- Labels, round 1 (above): the person's sheet; Claude's sheet, labelled
  blind in a session that hadn't seen the person's labels; the agreement
  and where it breaks down. The person's notes, kept apart in
  `labels/human-notes.md` until then, are folded into that section.
- `scripts/labels.py` (`make labels`): reads the sheets into
  `data/pairs.tsv` and `data/levels.tsv`, flags unlabelled matches, missing
  levels and levels that don't fit the y count, and reports Cohen's kappa
  per match (y/p/n, y against the rest, y+p against n) and per level.

### Next

1. **E2, baselines.** Compare the floor (`embeddings.verdict.level`), the
   keyword floor (`keyword_only.verdict.level`) and qwen3.5 with the level
   labels. The LLM gets the prompt from `verdict.BuildPrompt`
   (`verdict/verdict.go`) over the recorded matches, not a new search.
   - Targets: Claude's levels for the 75 ideas it didn't call vague (14
     open, 28 crowded, 33 saturated), each also against `from_y`; on the 12
     shared ideas, the person's levels as a check.
   - The 12 ideas labelled vague stay out of the confusion matrices; list
     what each method says for them. 7 of them are parity ideas, so a
     CPU-only LLM run on the parity ideas has 25 with a target.
   - Report false Opens and QWK first, exact agreement after them (see
     "What this means for the experiments").
2. **E3, per-match scorers.** Rescore the recorded candidates with each
   scorer: MiniLM cosine (recorded), lexical IDF, bge-small and e5-small,
   the ms-marco cross-encoder (raw and Platt-calibrated), an NLI
   cross-encoder, Qwen3-Reranker-0.6B (homelab). Output per scorer:
   `scores/<scorer>.tsv` (idea id, URL, score, ms). Then **labels, round
   2**: each scorer's top 5 per idea that round 1 didn't cover gets
   labelled too, so that no scorer is judged only on the cosine's picks.
   Report AUC, NDCG@10 and ms per idea, for y vs p+n and y+p vs n.
   To decide before round 2: one meaning of p, written into the sheet
   header. Round 1 suggests the person's: a variant that does the job in
   another form is y, the same job in another language is n, a nearby tool
   on the same data is p. Round-1 labels stay as they are, so the
   agreement above keeps measuring blind labels.
3. **E4, aggregators.** Features per idea from the best E3 scorer (see the
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
- `make labels` lists the 213 unlabelled matches of Claude's 12 vague ideas
  among its checks; that is expected.
- The session that labelled Claude's sheet has since seen the person's
  labels. If round 2 needs a blind Claude sheet, start a new session.
- `data/runs.jsonl` is 4.2 MB; gzip it if that is too big for the
  repository (the scripts then need to read the .gz).
