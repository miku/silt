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
| `scripts/labelsheet.py` | writes a labelling sheet; `make sheets`, and round 2: `make round2` |
| `labels/human.txt`, `labels/claude.txt` | labelling sheets: a sample of 12 ideas for a person, every idea for Claude |
| `labels/claude-round2.txt` | round 2: the scorers' top 5 that round 1 didn't list, matches only |
| `scripts/labels.py` | reads the sheets, checks them, reports the agreement; `make labels` |
| `data/pairs.tsv` | one line per labelled match: idea id, URL, label, labeller, note, round |
| `data/levels.tsv` | one line per level: idea id, level, labeller, the y/p/n counts, the level the y count gives |
| `results/labels-agreement.tsv` | agreement between labellers, per match and per level |
| `scripts/prompts/` | writes silt's verdict prompt for every idea, from the recording; `make prompts` |
| `data/prompts.jsonl` | one line per idea: the prompt, built once (2026-10-31 23:37 UTC) |
| `scripts/llm.py` | asks an LLM for every verdict, as silt asks it; `make llm` |
| `data/llm-<model>.jsonl` | one line per answer: the level, the raw answer, seconds, tokens |
| `scripts/e2.py` | compares the floors and the LLMs with the level labels; `make e2` |
| `results/e2-*.tsv` | per idea, per method (summary, confusion matrices), per LLM (time) |
| `scripts/score.py` | scores every candidate with E3's per-match scorers; `make scores` (uv) |
| `scores/<scorer>.tsv` | one line per candidate: idea, URL, score, ms; `scores/runs.tsv`: model, revision, time |
| `scripts/e3.py` | compares the scorers with the match labels; `make e3` |
| `results/e3-*.tsv` | per scorer, labeller, rounds and task (AUC, NDCG@10, precision in the top 5, time); the reworded packages' ranks |

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

## E2: baselines

Run 2026-11-01. Each method judges the matches recorded in E1 (the
embedding run). The targets are the levels from labels, round 1.

| method | what |
|---|---|
| floor | `silt --fast`: the level from the similarity thresholds (`embeddings.verdict.level`) |
| keyword floor | the same thresholds on the `--keyword-only` scores |
| qwen3.5:9b | silt's default model (`qwen3.5:latest` has the same digest), Q4_K_M, Ollama 0.40.11 on strix, entirely on the GPU |
| qwen3.5-122b | Q4_K_M, llama-swap's OpenAI API on strix, thinking off; the model warm |
| …, floored | what silt shows with that LLM today: the higher of its level and the floor; Open if the answer doesn't parse, takes over 120 s, or the best match is below 0.35 (none of these happened) |

The LLMs get the prompt that `verdict.BuildPrompt` builds over the recorded
matches (`data/prompts.jsonl`) and silt's request options
(`scripts/llm.py`). The level is the model's own, before silt floors it.
All 87 ideas were answered; every answer parsed. `make e2` writes
`results/e2-*.tsv`. Headlines and gaps aren't scored.

### Against Claude's levels

75 ideas: 14 open, 28 crowded, 33 saturated. The 12 Claude called vague
are left out.

| method | false Opens | Opens missed | QWK | exact | QWK, `from_y` | exact, `from_y` | predicted O/C/S |
|---|---|---|---|---|---|---|---|
| floor | 2/61 | 10/14 | 0.44 | 0.57 | 0.52 | 0.65 | 6/22/47 |
| keyword floor | 22/61 | 3/14 | 0.43 | 0.49 | 0.50 | 0.53 | 33/18/24 |
| qwen3.5:9b | 3/61 | 8/14 | 0.40 | 0.55 | 0.42 | 0.57 | 9/3/63 |
| qwen3.5:9b, floored | 1/61 | 10/14 | 0.36 | 0.55 | 0.37 | 0.57 | 5/7/63 |
| qwen3.5-122b | 3/61 | 7/14 | 0.44 | 0.59 | 0.45 | 0.61 | 10/4/61 |
| qwen3.5-122b, floored | 1/61 | 10/14 | 0.33 | 0.56 | 0.35 | 0.59 | 5/7/63 |

Claude's level down the side, the predicted open/crowded/saturated in
each cell:

| Claude | floor | keyword floor | 9b | 9b, floored | 122b | 122b, floored |
|---|---|---|---|---|---|---|
| open (14) | 4/6/4 | 11/3/0 | 6/1/7 | 4/3/7 | 7/0/7 | 4/2/8 |
| crowded (28) | 2/11/15 | 14/8/6 | 3/2/23 | 1/4/23 | 3/4/21 | 1/5/22 |
| saturated (33) | 0/5/28 | 8/7/18 | 0/0/33 | 0/0/33 | 0/0/33 | 0/0/33 |

On the person's 11 ideas, only the keyword floor gives false Opens (3 of
10). QWK there: floor 0.42, keyword floor 0.20, 9b 0.06. The 122b scores
0.00 because it says Saturated for all 11.

### What E2 shows

- **The LLMs say Saturated.** The 9b says Saturated for 63 of 75 ideas, the
  122b for 61; Claude's labels have 33. Both call 21 to 23 of the 28
  crowded ideas saturated. The prompt asks for this: "lean toward Crowded
  or Saturated when in doubt", and Saturated is "5+ strong matches (>=
  0.60)", next to scores that a mere topic match reaches. 66 of the 9b's
  87 headlines cite the 0.60 threshold, mostly as "multiple strong
  matches (similarity >= 0.60)", whether the matches do the job or not:
  Obelisk and TeamPass for a Bitwarden-compatible server (060),
  carbon-footprint libraries, none of them a VS Code extension (066). The
  122b names tools instead and cites the threshold in no headline, but
  its levels are the 9b's for 78 of 87 ideas.
- **Floored, as silt uses it, the LLM makes the level worse.** Compared
  with the floor alone it fixes one false Open, 071 (marc21; the floor's
  rule, see E1), and adds 8 over-calls (7 with the 122b); QWK drops from
  0.44 to 0.36 (0.33). Of the 9b's 16 levels above the floor, 5 are
  closer to Claude's, 10 further away, 1 as far. silt discards the levels
  below the floor, and those are right at least as often as wrong (2 of 4
  with the 9b, 4 of 6 with the 122b): 072 and 079 Open, correctly (the
  122b also gets 080 Open and 076 Crowded); 031 and 057 Open, falsely.
- **Unfloored, the 122b is about as good as the floor** (QWK 0.44, exact
  0.59 against 0.57), with one false Open more and three missed Opens
  fewer; the 9b is a little worse. Neither beats the floor on false
  Opens, and both cost seconds per idea where the floor costs nothing.
- **No method finds the open ideas.** Claude's 14 Opens are ideas where
  nothing retrieved does the job: pandoc, Vaultwarden, Rainbow Brackets
  and chezmoi didn't turn up, but near matches did. The floor calls 4 of
  them Open (023 and 032, the Chinese and Japanese ideas, plus 063 and
  082); the 9b adds 072 and 079, the 122b also 080. Telling a near match
  from one that does the job is question (a), E3. The LLM does it a
  little better than the thresholds, and silt's floor throws that away.
  One of these Opens is debatable: for 060 the 122b reads a Vaultwarden
  backup tool, a secrets API and an AWS deployment as proof that
  Vaultwarden exists, which Claude's label didn't count.
- **The false Open every method makes is a search problem.** 062 (nvd
  wasn't retrieved; best match 0.53) is Open with every method.
- **The keyword floor isn't a usable baseline as it is.** It gives 22
  false Opens, 10 of them on positives; its thresholds were set for
  cosines (E1).
- **Vague ideas.** For the 12 Claude called vague, the floor says
  Saturated 7 times, Crowded 3 and Open 2 ("anything at all with no
  signal", "something to make coding easier"). The 9b says Open for 6 and
  the 122b for 5, with headlines saying that nothing specific matches a
  generic idea; the floor raises all but those two. The LLM notices
  vagueness but has no level for it (the verdict note's open question on
  "too generic to judge").
- **Time.** On strix, the 9b takes a median of 5 s per idea (1.3 s to
  read ~1,580 prompt tokens, 3.5 s to write ~135), never more than 8 s;
  the 122b a median of 9 s, never more than 12 s. None comes near silt's
  120 s. Loading the 122b took 129 s, so silt's first request after a
  cold start would time out (silt retries once). The verdict note
  measured the 9b on a CPU at 83 to 192 s.
- **Run to run.** The 9b, asked twice at temperature 0, gave the same
  level for 65 of 66 ideas (013, a vague one, flipped). Only 11 of the
  answers were the same text.

**What this means:**

- For the level, the LLM doesn't earn its place. Floored, as silt uses
  it, it does worse than the floor on every measure except one false
  Open; unfloored, the larger model only matches the floor. This supports
  the verdict note's plan: the data verdict by default, the LLM for gaps
  only.
- The bar for E4 is the floor: 2 false Opens in 61, QWK 0.44 (0.52
  against `from_y`), 10 of 14 Opens missed. The room is in the Opens, and
  in crowded against saturated (15 of 28 crowded ideas called saturated).
  Both need a per-match judgement that tells a near match from one that
  does the job (E3), not a different count.
- 072, 079 and 080, where the LLM said Open over the floor and was right,
  are the cases a good E3 scorer should get right.

## E3: per-match scorers

Run 2026-11-01, on round 1's labels and then on rounds 1 and 2
([Round 2](#round-2), below). Each scorer scores every candidate of every idea: 8,747,
the matches of both recorded runs. The text of a candidate is what silt
embeds, "name: description" (`rank/ranker.go`). An idea's candidates are
scored in one batch, as silt would score them. `make scores` runs
`scripts/score.py` (uv, dependencies pinned inline; CPU-only torch) and
writes `scores/<scorer>.tsv`, with the model revisions in
`scores/runs.tsv`. `make e3` runs `scripts/e3.py`, which writes
`results/e3-scorers.tsv` and `results/e3-targets.tsv`.

| scorer | what | runs on |
|---|---|---|
| cosine | the recorded all-MiniLM-L6-v2 cosine: silt today | (recorded) |
| keyword | the recorded `--keyword-only` score, 0 where it has none | (recorded) |
| minilm | the same MiniLM, recomputed: it checks the text and gives a Python time. It matches the recorded cosine within 1e-4 on all but 9 of 8,734 candidates, texts over ~1,200 characters that the two cut at different token limits | CPU |
| idf | the share of the idea's IDF weight that the candidate's words cover; IDF over all candidates | CPU |
| bge-small | BAAI/bge-small-en-v1.5, with its query instruction | CPU |
| e5-small | intfloat/e5-small-v2, "query: " and "passage: " | CPU |
| ms-marco | cross-encoder/ms-marco-MiniLM-L6-v2, the raw logit | CPU |
| nli-minilm, nli-deberta | cross-encoder/nli-MiniLM2-L6-H768 and nli-deberta-v3-small: P(entailment) of "This software is {idea}." given the candidate | CPU |
| qwen3-embed | Qwen3-Embedding-0.6B (Ollama, `qwen3-embedding:0.6b`), with an instruction on the query: "Given a description of a software tool, retrieve packages and repositories that implement it" | strix GPU |
| qwen3-reranker | Qwen3-Reranker-0.6B (llama-swap, `/v1/rerank`), the idea as the query, llama.cpp's own instruction | strix GPU |
| qwen3-embed-plain | qwen3-embed without the instruction: the idea alone (check 1, below) | strix GPU |
| qwen3-rr-ours | Qwen/Qwen3-Reranker-0.6B with transformers, by the model card's recipe (log P(yes) − log P(no)), with an instruction of our own (check 2, below) | CPU |

### Results, round 1

Against Claude's round-1 labels: 1,224 matches of 75 ideas, 334 y, 282 p,
608 n. In brackets, the 95% interval from resampling ideas. "Beats cosine" is
the share of resamples in which the scorer's AUC is above the cosine's.

| scorer | AUC, y vs p+n | beats cosine | AUC, y+p vs n | beats cosine | AUC within an idea (y) | NDCG@10 | unlabelled in top 10 | ms per 50 matches |
|---|---|---|---|---|---|---|---|---|
| qwen3-rr-ours | 0.877 (0.85–0.90) | 1.00 | 0.886 (0.85–0.92) | 1.00 | 0.848 | 0.844 | 1.9 | 8,480 (CPU) |
| qwen3-embed | 0.842 (0.80–0.88) | 0.79 | 0.851 (0.81–0.89) | 0.98 | 0.847 | 0.855 | 1.9 | 1,130 (GPU) |
| qwen3-reranker | 0.839 (0.80–0.87) | 0.80 | 0.850 (0.81–0.89) | 0.98 | 0.805 | 0.829 | 1.9 | 540 (GPU) |
| cosine | 0.823 (0.79–0.86) | | 0.808 (0.76–0.85) | | 0.781 | 0.776 | 0.0 | 470 (minilm) |
| e5-small | 0.808 | 0.14 | 0.790 | 0.13 | 0.731 | 0.739 | 1.5 | 940 |
| qwen3-embed-plain | 0.795 (0.74–0.85) | 0.15 | 0.812 (0.76–0.86) | 0.57 | 0.840 | 0.848 | 2.0 | 1,220 (GPU) |
| nli-deberta | 0.784 | 0.02 | 0.765 | 0.03 | 0.717 | 0.721 | 4.2 | 2,220 |
| bge-small | 0.777 | 0.00 | 0.768 | 0.01 | 0.739 | 0.741 | 1.4 | 900 |
| ms-marco | 0.760 | 0.00 | 0.733 | 0.00 | 0.707 | 0.719 | 1.4 | 570 |
| nli-minilm | 0.752 | 0.00 | 0.715 | 0.00 | 0.696 | 0.717 | 3.5 | 1,810 |
| idf | 0.751 | 0.00 | 0.756 | 0.01 | 0.647 | 0.692 | 2.4 | 0.4 |
| keyword | 0.714 | 0.00 | 0.707 | 0.00 | 0.614 | 0.642 | 2.6 | |

AUC is over all labelled matches of all ideas: can one threshold separate
them? A level built on the score needs that. "Within an idea" ranks one
idea's matches. NDCG@10 is graded (y 2, p 1, n 0) and counts the labelled
matches only. The cosine has an edge on both: round 1 labelled its top 15
(and the keyword score's top 5), so its top 10 is fully labelled, while
the other scorers have 1 to 4 unlabelled matches in theirs.

Without labels: for the 18 positive ideas whose package was retrieved,
the package's rank under each scorer (`results/e3-targets.tsv`).

| scorer | first | top 5 | top 15 | median rank |
|---|---|---|---|---|
| qwen3-rr-ours | 6 | 15 | 17 | 3 |
| qwen3-embed | 5 | 12 | 17 | 3 |
| qwen3-reranker | 5 | 11 | 16 | 3 |
| qwen3-embed-plain | 4 | 11 | 16 | 3 |
| cosine | 3 | 10 | 14 | 3 |
| bge-small | 3 | 9 | 14 | 5.5 |
| ms-marco | 3 | 8 | 11 | 6.5 |
| nli-minilm | 1 | 6 | 13 | 9.5 |
| e5-small | 1 | 8 | 14 | 11 |
| nli-deberta | 2 | 6 | 12 | 11.5 |
| idf, keyword | 1 | 4, 2 | 9 | 18.5 |

### What round 1 shows

- **Only the Qwen3 models beat the cosine.** As first run, with their
  usual instructions, their AUC is 0.84 to 0.85, against 0.81 to 0.82.
  For y+p against n they beat it in 98% of the resamples; for y against
  p+n in 80%, which isn't settled. The ranking within an idea gains more
  (0.85 and 0.81 against 0.78), and both put the reworded package in the
  top 15 more often (17 and 16 of 18, against 14). They are also the
  largest models here: 0.6B parameters against 22M to 140M. The reranker
  with an instruction of our own does better still (the two checks,
  below).
- **What they do differently.** 034 (load a .env file in a node app): the
  reranker scores cross-env, which sets variables but reads no `.env`,
  0.10, and react-env 0.15; every Node dotenv package scores 0.97 or
  more. The cosine ranks cross-env 5th, above dotenv-flow and @expo/env.
  Over the 67 ideas with both classes (y+p against n), Qwen3-Embedding
  ranks better than the cosine for 36 and worse for 16. The largest
  losses are on ideas with almost no n to rank (051: one n in 17).
- **Everything else is worse than the cosine**: the small asymmetric
  bi-encoders (bge-small, e5-small), both NLI models, lexical IDF and the
  keyword score. The ms-marco cross-encoder, the "joint encoding" that
  was remembered as not helping, is now written down: AUC 0.76 against
  0.82. It is trained for web-search relevance, not "does the same job".
- **The task depends on what p means.** On the 10 ideas both labellers
  labelled, y against p+n gives the same picture with either sheet: the
  cosine 0.78, Qwen3-Embedding 0.83 (Claude) and 0.81 (person), the
  reranker 0.78 and 0.81. For y+p against n the sheets disagree: with
  Claude's labels the Qwen3 models win (0.92 against 0.90), with the
  person's the cosine does (0.86 against 0.83 and 0.84). That fits round
  1: the person's p is "a nearby tool on the same data", which is what a
  cosine measures; Claude's p is "the same job elsewhere". Until p has one
  meaning, y against p+n is the task to judge scorers on. On all 12 of the
  person's ideas Qwen3-Embedding falls below the cosine (0.74 against
  0.78); the difference comes from 020 and 025, which Claude called vague
  and didn't label.
- **The reranker's score is not a probability of "implements".** Its
  median is 0.999 for y, 0.988 for p and 0.131 for n, but 38% of the n
  score above 0.5 and 25% above 0.9. Ranking doesn't care; summing it
  into S (the verdict note's `S = Σ p_i …`) needs calibration first
  (E4).
- **Time.** On strix's GPU the reranker takes 0.5 s for an idea with 50
  matches, and Qwen3-Embedding through Ollama 1.1 s. On this laptop's
  CPU, in Python: MiniLM 0.5 s (silt's Go: about 0.15 s), the small
  bi-encoders 0.9 s, ms-marco 0.6 s, the NLI models 1.8 to 2.2 s. The CPU
  times were taken alongside other work and are indicative only. Neither
  Qwen3 model fits silt's pure-Go path; both need a model server. silt
  already talks to Ollama, which serves Qwen3-Embedding.

### The two checks

**1. Qwen3-Embedding without its instruction** (qwen3-embed-plain). The
idea alone as the query. Within an idea it ranks as well as with the
instruction (0.840 against 0.847; NDCG@10 0.848 against 0.855), but over
all ideas its AUC falls to the cosine's or below (0.795 against 0.842 for
y against p+n). So the instruction doesn't change which of an idea's
matches come first; it makes the scores of different ideas comparable,
which is what a threshold, and so a level, needs. Scores normalised
within an idea might do the same for any scorer (E4).

**2. Qwen3-Reranker with our own instruction** (qwen3-rr-ours). The
rerank endpoint on strix takes no instruction. The reranker was run here
instead, with transformers and the model card's prompt:

> The Query is an idea for a software tool; the Document describes a
> piece of software. Judge whether the software implements the idea: it
> is such a tool, or does the same job in another form. Software that is
> only on the same topic, works with the same data, or does the job for
> another language or platform does not.

That is the round-2 meaning of y, written as an instruction. As a
control, the same local code with the default instruction scored idea
034 like strix (Spearman 0.998, the same probabilities to three
decimals): llama.cpp applies the model card's template, and the Q8 GGUF
on strix and the full-precision model here agree.

The instruction is worth 4 AUC points on the same model: 0.877 against
0.839 for y against p+n, 0.886 against 0.850 for y+p against n. It
beats the cosine in every resample on both tasks. On the 10 ideas both
labellers labelled, y against p+n gives 0.83 with Claude's labels and
0.84 with the person's, against the cosine's 0.78 with either; only for
y+p against n with the person's labels does the cosine stay ahead (0.86
against 0.85). It also ranks the reworded package highest of all the
scorers: first for 6 of 18, in the top 5 for 15. Its scores are
somewhat closer to probabilities than the default's: 28% of the n score
above 0.5 (default: 38%), 13% above 0.9 (25%).

Two caveats. The instruction was written after round 1, from the labels'
own definition of y, so part of its edge may be agreement with that
definition rather than a better judge; round 2's labels, and the
person's, are the check. And it ran on this laptop's CPU in fp32: 8.5 s
per 50 matches, with the part of the prompt an idea's candidates share
computed once per idea (4.7 times faster on 034, with the same scores to
2e-5). On a GPU it would cost what the strix reranker does, but
llama.cpp's rerank endpoint can't take the instruction; it would need a
GGUF with our template, or a server that returns the yes/no logits.

**What this means:**

- One model is worth taking further: Qwen3-Reranker with an instruction
  that says what "implements" means. Qwen3-Embedding with its instruction
  is close behind and runs on the Ollama silt already talks to. All the
  small models that could run in silt's Go code do worse than the cosine
  it already has.
- The win over the cosine is 5 to 8 AUC points with our instruction, 2
  to 4 without, where the labellers agree on 83 to 85% of matches. Round
  2 has to label what the Qwen3 models rank high before it counts.

### Round 2

Labelled 2026-11-01, blind: in a new session that had seen no score, none
of the results above and not the person's labels. `labels/claude-round2.txt`
lists every scorer's top 5 that round 1 didn't list, 348 matches of 69
ideas, and gives p its round-2 meaning (the same job in another form is y,
the same job for another language or platform is n, a nearby tool on the
same data is p). 346 are labelled: 37 y, 37 p, 272 n. Two are not (057,
080): their pages didn't load and their descriptions don't decide.

`scripts/e3.py` now writes a row for each round and for both together
(`rounds` in `results/e3-scorers.tsv`); its round-1 rows are the table
above, unchanged. It also reports the share of positives in each scorer's
top 5 among all of an idea's candidates, which round 2 makes meaningful:
every scorer's top 5 is now labelled, except where keyword and idf tie at
the fifth place (see Things to know).

Both rounds, against Claude's labels: 1,570 matches of 75 ideas, 371 y,
319 p, 880 n.

| scorer | AUC, y vs p+n | beats cosine | AUC, y+p vs n | beats cosine | AUC within an idea (y) | NDCG@10 | y in top 5 | y+p in top 5 | unlabelled in top 10 |
|---|---|---|---|---|---|---|---|---|---|
| qwen3-rr-ours | 0.883 (0.85–0.91) | 1.00 | 0.891 (0.86–0.92) | 1.00 | 0.853 | 0.813 | 0.51 | 0.75 | 0.8 |
| qwen3-embed | 0.856 (0.82–0.89) | 0.92 | 0.862 (0.82–0.89) | 0.99 | 0.842 | 0.843 | 0.48 | 0.74 | 0.9 |
| qwen3-reranker | 0.847 (0.81–0.88) | 0.84 | 0.856 (0.82–0.89) | 0.96 | 0.812 | 0.797 | 0.48 | 0.73 | 0.8 |
| cosine | 0.826 (0.79–0.86) | | 0.821 (0.78–0.86) | | 0.749 | 0.729 | 0.45 | 0.69 | 0.0 |
| e5-small | 0.813 | 0.18 | 0.798 | 0.07 | 0.724 | 0.681 | 0.42 | 0.65 | 0.8 |
| qwen3-embed-plain | 0.813 (0.76–0.86) | 0.31 | 0.826 (0.78–0.87) | 0.61 | 0.834 | 0.832 | 0.47 | 0.74 | 1.0 |
| bge-small | 0.791 | 0.01 | 0.783 | 0.01 | 0.721 | 0.682 | 0.43 | 0.66 | 0.8 |
| nli-deberta | 0.766 | 0.00 | 0.740 | 0.00 | 0.693 | 0.627 | 0.35 | 0.56 | 2.2 |
| idf | 0.755 | 0.00 | 0.758 | 0.00 | 0.633 | 0.627 | 0.34 | 0.56 | 1.3 |
| ms-marco | 0.752 | 0.00 | 0.729 | 0.00 | 0.691 | 0.658 | 0.41 | 0.63 | 0.5 |
| nli-minilm | 0.732 | 0.00 | 0.687 | 0.00 | 0.661 | 0.613 | 0.35 | 0.52 | 1.7 |
| keyword | 0.728 | 0.00 | 0.717 | 0.00 | 0.610 | 0.570 | 0.29 | 0.50 | 1.8 |

Round 2 alone: the 346 matches the cosine didn't put in its top 15,
labelled blind.

| scorer | AUC, y vs p+n | beats cosine | AUC, y+p vs n | beats cosine |
|---|---|---|---|---|
| qwen3-embed | 0.873 (0.81–0.93) | 0.99 | 0.850 (0.79–0.91) | 1.00 |
| qwen3-rr-ours | 0.868 (0.79–0.93) | 0.99 | 0.867 (0.81–0.92) | 1.00 |
| qwen3-embed-plain | 0.838 (0.77–0.90) | 0.98 | 0.816 (0.75–0.88) | 0.98 |
| qwen3-reranker | 0.829 (0.73–0.92) | 0.90 | 0.827 (0.75–0.89) | 0.99 |
| cosine | 0.762 (0.66–0.85) | | 0.748 (0.66–0.82) | |
| the other seven | 0.58 to 0.75 | 0.00 to 0.34 | 0.56 to 0.73 | 0.00 to 0.25 |

### What round 2 shows

- **The Qwen3 lead holds on blind labels.** On round 2 alone, matches
  the cosine ranks below its 15th and labelled without seeing a score,
  the four Qwen3 scorers reach 0.83 to 0.87 for y against p+n, against
  the cosine's 0.76, and beat it in 90 to 99% of the resamples. Pooled
  with round 1, every AUC moves by 0.03 or less, and the first four keep
  their order on both tasks. Qwen3-Embedding now
  beats the cosine in 92% of the resamples for y against p+n (79% on
  round 1), the default reranker in 84% (80%).
- **Our instruction's edge over Qwen3-Embedding doesn't survive the blind
  labels.** On round 1 qwen3-rr-ours led qwen3-embed by 3.5 points for y
  against p+n (0.877 against 0.842); on round 2 alone the two are level
  (0.868 against 0.873, intervals 0.79–0.93 and 0.81–0.93). It stays
  ahead of the same model with llama.cpp's instruction (0.829). So the
  caveat above holds in part: some of round 1's edge was agreement with
  the definition the instruction was written from.
- **At the top of the list the gain is small.** In the top 5, 51% of
  qwen3-rr-ours's picks are y, 48% of the other two Qwen3 models', and 45%
  of the cosine's (y+p: 75, 74, 73 against 69%). The AUC gain is larger
  (0.88 against 0.83) because it counts every labelled match: the Qwen3
  models push the n further down and, with their instruction, make the
  scores of different ideas comparable (check 1). That is what a level
  built on a threshold needs. For the first five, the cosine already gets
  most of the ranking right.
- **NDCG@10 fell for every scorer** (by 0.01 to 0.10): round 2 added n
  that the other scorers rank high, and y and p that the cosine ranks
  low. Qwen3-Embedding has the highest (0.843).
- **What each scorer brought.** Of the top-5 picks that round 1 hadn't
  listed, each of the Qwen3 models with an instruction had 36 to 41, of
  which 9 or 10 were y. nli-deberta had 125: 15 y, 16 p and 92 n. Twelve
  of round 2's 37 y were in nli-deberta's top 5 and in no Qwen3 scorer's.
  The labels still miss implementations further down the lists: AUC
  doesn't care about that, but a count of y does (E4). (Counted from
  `data/pairs.tsv` and `scores/`, not by `e3.py`.)
- **Four ideas labelled Open have an implementation among their
  candidates**, and the Qwen3 models rank it high:

  | idea | implementation | cosine | qwen3-embed | qwen3-reranker | qwen3-rr-ours |
  |---|---|---|---|---|---|
  | 032 (Japanese: kill the process on a port) | okamyuji/devpulse, a TUI whose `K` ends it | 48 of 63 | 1 | 1 | 1 |
  | 052 (convert between markdown, latex, word) | jgm/pandoc | 23 of 87 | 6 | 11 | 3 |
  | 060 (self-hosted, Bitwarden-compatible) | lj5645/FlexVault, Workers or Node, not Docker | 46 of 68 | 1 | 4 | 4 |
  | 079 (browse OAI-PMH endpoints) | oai-pmh-mcp, an MCP server | 47 of 117 | 5 | 6 | 21 |

  Round 1 set these levels from what it listed, the cosine's top 15 and
  the keyword score's top 5 ("pandoc didn't turn up"). With round 2's y counted, 4 of the 14 Opens
  have one; 047 (4 y to 5) and 064 (4 to 7) cross into Saturated by the
  y count. E2 counted the LLM's Open on 079 as correct.

**What this means:**

- Qwen3-Embedding with its instruction and Qwen3-Reranker with ours are
  the two scorers worth taking into E4. They are level on the blind
  labels; Qwen3-Embedding runs on the Ollama silt already talks to, the
  reranker with our instruction needs a server that takes it.
- The levels were judged on the cosine's top 15. A scorer that ranks
  every candidate finds implementations that list didn't show, so E4
  needs levels that say what the candidates hold, or it counts finding
  pandoc as a mistake.

## Status and next steps

As of 2026-11-01.

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
- **LLM baseline (E2):** qwen3.5:9b (silt's default) and qwen3.5-122b on
  the homelab GPU (strix), all 87 ideas.
- **p, from round 2 on** (2026-11-01): the person's meaning. A variant
  that does the job in another form is y, the same job in another
  language or platform is n, a nearby tool on the same data is p. Written
  into the round-2 sheet's header; round-1 labels stay as they were.
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
- E2 (above): the floor, the keyword floor, qwen3.5:9b and qwen3.5-122b,
  each LLM also floored as silt does it, against Claude's levels,
  `from_y` and the person's sample. `scripts/prompts` and `scripts/llm.py`
  ask the LLMs exactly what silt asks; `scripts/e2.py` scores them.
- E3 on the round-1 labels (above): eleven scorers over all 8,747
  candidates; only Qwen3-Embedding and Qwen3-Reranker beat the cosine.

- E3's two checks (above): Qwen3-Embedding without its instruction, and
  Qwen3-Reranker with an instruction of our own, which is the best scorer
  so far.
- The round-2 sheet, `labels/claude-round2.txt` (`make round2`): 348
  matches of 69 ideas, the top 5 of the 13 scorers that round 1 didn't
  list. Its header gives p the meaning decided after round 1.
  `scripts/labels.py` reads `<labeller>-round2.txt` sheets as round 2 of
  that labeller (a `round` column in `data/pairs.tsv`, no levels) and
  keeps the agreement on round 1.
- Labels, round 2 (above): Claude's round-2 sheet, labelled blind in a
  new session; 346 of 348 matches (37 y, 37 p, 272 n).
- E3 on both rounds (above): `scripts/e3.py` reports each round alone and
  both together (`rounds`), and the share of positives in each scorer's
  top 5. The Qwen3 lead holds on the blind labels; there, our
  instruction's edge over Qwen3-Embedding is gone. E3 is done.

### Next

1. **Levels after round 2.** Round 2 found an implementation among the
   candidates of 4 of the 14 Opens (032, 052, 060, 079), and takes 047
   and 064 past 5 y. Decide what E4 aims at: the levels as labelled
   (judged on round 1's list) or levels revised to what the candidates
   hold. The proposal: keep `level` as it is, so the agreement keeps
   measuring blind labels, and add a revised level beside it, as
   `from_y` is; let `from_y` count both rounds' y. The E2 bar would then
   need recounting against the revised levels (079 was one of the LLM's
   correct Opens).
2. **E4, aggregators.** Features per idea from Qwen3-Embedding and from
   Qwen3-Reranker with our instruction, level on round 2 (see the verdict
   note, "(b) Aggregation into a level"); refit floor, ordinal logistic
   regression, depth-3 tree; leave-one-out cross-validation; the floor
   stays a lower bound. Report confusion matrices, QWK and false Opens
   against the floor, which E2 sets as the bar: 2 false Opens in 61, QWK
   0.44, 10 of 14 Opens missed (against the levels as labelled).

### Things to know

- `scripts/record.sh` doesn't need to run again: `data/runs.jsonl` is the
  frozen data. It resumes and skips recorded ids if it does run. It uses
  its own cache in `work/` (ignored by git), with the model, catalogs and
  browser profile linked in from `~/.cache/silt`.
- The keyword-only run excludes the sources that failed in the embedding
  run (none failed in this recording), but its matches are not the
  embedding run's: `rank.ByKeywords` drops the candidates without a
  keyword in common (a median of 39% of an idea's), and 13 of its
  matches aren't in the embedding run. E3 scores both together.
- A rejected idea has an `error` field and no `embeddings`.
- The positives' targets are read from `ideas.tsv`, not from the
  recording, so they can be corrected without recording again.
- `labelsheet.py` never overwrites a sheet; delete one to regenerate it
  (only if it holds no labels).
- `make labels` lists the 213 unlabelled matches of Claude's 12 vague ideas
  among its checks, and round 2's two (057, 080); that is expected.
- The session that labelled Claude's round-1 sheet has since seen the
  person's labels; round 2 was labelled in a new session, which has since
  seen the results. A further blind round needs a new session again.
- In keyword and idf many candidates tie at the fifth place (81 and 55
  ideas). `e3.py` breaks ties by URL, the round-2 sheet broke them
  otherwise, so 66 of their top-5 picks are unlabelled and count as
  negative: their share in the top 5 is a lower bound.
- `data/runs.jsonl` is 4.2 MB; gzip it if that is too big for the
  repository (the scripts then need to read the .gz).
- `data/prompts.jsonl` is built once and kept (`make prompts` never
  rebuilds it): the prompt gives the age of a match relative to when it
  was built. It differs from a default silt run in one line: the
  recording ranks every candidate (median 89), so the list ends "… and N
  more" with N counting all of them, where silt, ranking the top 50,
  would say "… and 35 more" (84 of the 87 prompts).
- The Ollama on strix restarted four times during the E2 runs: the
  request in flight lost its connection, and then the server refused
  connections, once for about 90 s. It also ran faster after the first
  restart: the 9b took 15 s per idea before (7.5 s to read the prompt),
  5 s after. The 9b was run again in full after that, so all the times
  above come from the faster server. `scripts/llm.py` now waits and
  tries again (10, 20, 30 s), and a request that still fails isn't
  counted as answered, so the next run asks again. The failed request
  stays in the file; `e2.py` uses the last line per idea.
- The 9b's first run is not kept; it served only the run-to-run
  comparison above.
- `scores/` is 5.9 MB. `scripts/score.py` downloads each model's whole
  Hugging Face repository the first time (ONNX and OpenVINO copies
  included), into `~/.cache/huggingface`. The Qwen3 scorers need strix:
  `qwen3-reranker` on llama-swap and `qwen3-embedding:0.6b` on Ollama.
- uv runs `scripts/score.py` under Python 3.12; the other scripts run
  under the system's `python3 -I`.
- `uv run scripts/score.py --labelled SCORER` scores only the labelled
  matches, for a quick AUC from a slow scorer; `e3.py` then leaves out
  its top 10 and the packages' ranks. qwen3-rr-ours takes ~25 minutes for
  all candidates on this laptop.
- `scripts/labels.py` given a sheet writes `data/pairs.tsv` and
  `data/levels.tsv` from that sheet alone. Run it without arguments
  (`make labels`) to keep every sheet's labels in them.
