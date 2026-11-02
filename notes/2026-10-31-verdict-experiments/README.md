# Verdict experiments

silt answers "has my idea been built already?" with one of three levels:
**Open** (nothing does it), **Crowded** (a few things do) or **Saturated**
(many things do). These experiments ask whether that answer can be made
better, and whether it needs an LLM.

This file is the summary, as of 2026-11-02. The full log, with every number
and the reasoning behind each decision, is [Notes.md](Notes.md). The plan
the experiments come from is [../2026-10-29-verdict.md](../2026-10-29-verdict.md).

## In short

| step | question | status | answer |
|---|---|---|---|
| E1 | Build one fixed data set that every method is tested on | done | 97 ideas, 8,747 candidate matches, recorded once |
| labels | Write down the right answers | done, 2 rounds | 1,570 matches and 75 levels labelled |
| E2 | Does an LLM give a better level than today's thresholds? | done | no |
| E3 | Which method best tells a real implementation from a look-alike? | done | two 0.6B Qwen3 models; every smaller model is worse than what silt uses today |
| E4 | Does a better per-match score give a better level? | not started | |

- **The LLM doesn't earn its place for the level.** Combined with the
  thresholds, as silt does it today, it makes the level worse than the
  thresholds alone. On its own, the large model (122B) only ties them, at
  about 9 s per idea. Both LLMs say Saturated far too often.
- **Only the Qwen3 0.6B models, given an instruction, beat today's
  similarity score** at telling real implementations from look-alikes: AUC
  0.85 to 0.88 against 0.83 on all labels, and 0.83 to 0.87 against 0.76 on
  the labels made blind. All the small
  models that could run inside silt's Go code do worse than what it has.
- **No method finds the Open ideas yet.** The thresholds find 4 of 14. That
  is the room for E4, and it needs the better per-match judgement from E3.
- **Some limits are outside any judge.** Search doesn't retrieve 12 of the
  30 well-known packages at all. And 4 of the 14 ideas labelled Open turned
  out to have an implementation further down the list, which round 1 of the
  labelling never showed.

## The setup

### The question, in two parts

- **(a) Per match: does this candidate do what the idea describes?** For
  "a CLI that kills a process on a port", `kill-port` does; a general
  process monitor is only related. Today silt decides this with the
  **cosine**: how similar the idea's text is to the candidate's
  "name: description", by a small sentence-embedding model
  (all-MiniLM-L6-v2). The cosine mixes up "same topic" with "does the same
  job".
- **(b) Per idea: given the matches, how crowded is the space?** Today this
  is the **floor**, a counting rule over the top 50 matches: 5 or more with
  a cosine of at least 0.60 make Saturated; 2 or more at 0.55, or 1 at
  0.70, make Crowded; anything else is Open. Optionally an LLM is asked, and
  silt shows the higher of its level and the floor's.

### The data (E1)

silt was run once, on 2026-10-31, on 97 ideas, and everything it found was
saved (`data/runs.jsonl`). Every later method judges exactly these
candidates, so the results can be compared and reproduced. Each idea was
ranked twice over the same candidates: with embeddings, and with silt's
`--keyword-only` score.

| stratum | ideas | what |
|---|---|---|
| parity | 32 | the reference ideas of silt's parity tests |
| positive | 30 | a well-known package's description in other words, e.g. "a fast recursive grep that skips files listed in .gitignore" for ripgrep; the level should be at least Crowded |
| specific | 20 | narrow ideas, some likely Open |
| vague | 15 | "a python library", "a database" |

silt itself turns down 10 of the vague ideas as too vague, which leaves 87.
An idea has a median of 89 candidates.

What the data shows before any judging:

- **Search, not judgement, limits the positives.** The reworded package is
  among the candidates for only 18 of the 30, and in the top 15 for 14.
  rich, black, watchdog, clap, cobra, lazygit, vaultwarden and five others
  are missing entirely. No scorer can rank what wasn't retrieved.
- **The floor's rule makes false Opens of its own.** "a rust crate for
  parsing marc21 library catalog records" has 4 candidates, and the first,
  marc-rs (cosine 0.62), is exactly that. The floor says Open, because a
  single match counts only from 0.70.
- **The thresholds only fit the cosine.** The same rule on the keyword
  scores says Open for 35 ideas, against 8 with the cosine.
- Every source was reached in every run, so missing coverage explains
  nothing here.

### The labels

Each match is marked **y** (implements the idea), **p** (partial, or nearby)
or **n** (no). Each idea gets a level, or "vague" if it is too generic to
judge (12 of the 87). That leaves 75 ideas with a level: 14 Open,
28 Crowded, 33 Saturated.

The guide for the level is the number of y: none is Open, 1 to 4 Crowded,
5 or more Saturated. The labeller may depart from it, for instance when
most y are mirrors of one project. The level the y count gives is kept as a
second target, `from_y`.

- **Round 1.** Claude labelled all 87 ideas: for each, the cosine's top 15
  and the keyword score's top 5, 1,224 matches (the 12 it called vague got
  a level only). As a check, a person labelled a
  random 12 ideas (201 matches) independently.
- **Round 2.** Claude labelled, blind (in a new session that had seen no
  scores and no results), each scorer's top 5 that round 1 hadn't shown:
  346 matches. Round 2 used a sharper meaning of p (below).

### How far the labels can be trusted

The person and Claude on the same 167 matches and 12 levels:

| what | agree | kappa |
|---|---|---|
| y / p / n | 71% | 0.56 |
| y against p or n | 83% | 0.65 |
| y or p against n | 85% | 0.66 |
| level | 8 of 12 | 0.52 |

- **They almost never disagree on y against n** (4 of 49 disagreements).
  Nearly all disagreements are about p. Claude's p meant "does the job, but
  with a gap or for another platform"; the person's p meant "a nearby tool
  worth seeing". From round 2 on, p has one meaning: the same job in
  another form is y, the same job for another language or platform is n, a
  nearby tool on the same data is p. Round 1's labels were left as they
  were.
- **They agree on Open against the rest** for all 10 ideas both judged.
  They disagree on Crowded against Saturated: the person's bar for
  Saturated is higher than 5 y.

So: the count of false Opens is more trustworthy than exact accuracy, and
scorers a few AUC points apart may not really differ.

### What is measured

| measure | in plain words |
|---|---|
| AUC | Pick one y and one non-y match at random: how often does the scorer score the y higher? 0.5 is a coin flip, 1.0 is perfect. Taken over all ideas together, so it also asks whether one threshold works for every idea, which a level needs. |
| y in top 5 | The share of a scorer's top 5 per idea that are y. |
| false Opens | How often a method says Open although something does the job. The worst mistake: a new method must not make it more often than the floor. |
| Opens found | Of the 14 Open ideas, how many the method calls Open. |
| QWK | One number for agreement on the level, where Open for a Saturated idea counts worse than Crowded. 1 is perfect, 0 is chance. |
| exact | The share of ideas with exactly the right level. |

## E2: the level, floor against LLM

Each method judges the recorded matches of the 75 ideas with a level. The
LLMs get exactly the prompt silt builds.

| method | false Opens (of 61) | Opens found (of 14) | QWK | exact | says O/C/S | time per idea |
|---|---|---|---|---|---|---|
| floor (silt `--fast`) | **2** | 4 | **0.44** | 57% | 6/22/47 | ~0 |
| keyword floor | 22 | 11 | 0.43 | 49% | 33/18/24 | ~0 |
| qwen3.5:9b (silt's default LLM) | 3 | 6 | 0.40 | 55% | 9/3/63 | 5 s |
| qwen3.5:9b + floor (silt today) | 1 | 4 | 0.36 | 55% | 5/7/63 | 5 s |
| qwen3.5-122b | 3 | 7 | **0.44** | 59% | 10/4/61 | 9 s |
| qwen3.5-122b + floor | 1 | 4 | 0.33 | 56% | 5/7/63 | 9 s |
| *right answer* | | | | | *14/28/33* | |

"+ floor" is what silt shows: the higher of the LLM's level and the
floor's. Times are medians on a home GPU with the model loaded; loading the
122b cold took 129 s, longer than silt's 120 s timeout.

- **The LLMs say Saturated for 61 to 63 of 75 ideas**, where the labels
  have 33. The prompt asks for it: it says to "lean toward Crowded or
  Saturated when in doubt", and defines Saturated as "5+ strong matches
  (>= 0.60)", a cosine that a mere topic match reaches.
- **silt's combination is worse than the floor alone.** It fixes one false
  Open (the marc21 idea) and adds 7 or 8 over-calls. It also throws away
  the LLM's Opens below the floor, which were right at least as often as
  wrong.
- **Alone, the 122b only ties the floor**, and neither LLM beats the floor
  on false Opens.
- **Nobody finds the Opens.** The Open ideas are ones where only near
  matches were retrieved; telling a near match from one that does the job
  is question (a), which E3 takes up.
- **The keyword floor is no use as it is**: 22 false Opens, because its
  thresholds were set for cosines.
- The one false Open that every method makes is a search miss: the package
  (nvd) was never retrieved.
- Asked twice, the 9b gave the same level for 65 of 66 ideas.
- For vague ideas the LLMs notice the vagueness (about half the time they
  say Open and explain why), but silt has no level for "too generic to
  judge".

**So:** for the level, use the data by default and keep the LLM, if at all,
for explanations. The bar for E4 is the floor: 2 false Opens in 61, QWK
0.44, 4 of 14 Opens found.

## E3: per match, which scorer tells implementations from look-alikes

Each scorer scores all 8,747 candidates, idea against "name: description".

| scorer | what it is |
|---|---|
| cosine | silt today: all-MiniLM-L6-v2 embeddings, 22M parameters, runs in silt's Go code |
| keyword | silt's `--keyword-only` score |
| idf | the share of the idea's rare words that the candidate contains |
| bge-small, e5-small | small embedding models trained for search (query against document) |
| ms-marco | a small cross-encoder (reads idea and candidate together), trained for web-search relevance |
| nli-minilm, nli-deberta | entailment models: how likely is "This software is {idea}." given the candidate? |
| qwen3-embed | Qwen3-Embedding-0.6B, with an instruction: "Given a description of a software tool, retrieve packages and repositories that implement it" |
| qwen3-embed-plain | the same, without the instruction |
| qwen3-reranker | Qwen3-Reranker-0.6B, a cross-encoder, with its default instruction |
| qwen3-rr-ours | the same reranker with an instruction of our own that defines "implements" the way the labels do |

Results on all labels of both rounds (1,570 matches of 75 ideas, 371 y),
for y against p or n. "Blind" is round 2 alone: matches outside the
cosine's top 15, labelled without seeing any score. They are not a random
sample, but they are the strongest check.

| scorer | AUC | better than cosine (share of resamples) | AUC, blind | y in top 5 | time per idea (50 matches) |
|---|---|---|---|---|---|
| qwen3-rr-ours | **0.88** | 100% | **0.87** | **51%** | 8.5 s (laptop CPU) |
| qwen3-embed | 0.86 | 92% | **0.87** | 48% | 1.1 s (GPU) |
| qwen3-reranker | 0.85 | 84% | 0.83 | 48% | 0.5 s (GPU) |
| **cosine (today)** | 0.83 | | 0.76 | 45% | 0.5 s (Python), ~0.15 s (Go) |
| qwen3-embed-plain | 0.81 | 31% | 0.84 | 47% | 1.2 s (GPU) |
| e5-small | 0.81 | 18% | 0.74 | 42% | 0.9 s |
| bge-small | 0.79 | 1% | 0.75 | 43% | 0.9 s |
| nli-deberta | 0.77 | 0% | 0.66 | 35% | 2.2 s |
| idf | 0.76 | 0% | 0.68 | 36% | < 1 ms |
| ms-marco | 0.75 | 0% | 0.59 | 41% | 0.6 s |
| nli-minilm | 0.73 | 0% | 0.58 | 35% | 1.8 s |
| keyword | 0.73 | 0% | 0.71 | 40% | (recorded) |

The other task, y or p against n, gives nearly the same order. CPU times
were taken on a busy laptop and are only indicative.

- **Only the Qwen3 models with an instruction beat the cosine, and the
  lead holds on the blind labels** (0.83 to 0.87 against 0.76). They are also much larger: 0.6B
  parameters against 22M to 140M for the rest. None fits silt's pure-Go
  path; both need a model server. Ollama, which silt already talks to,
  serves Qwen3-Embedding.
- **Everything small is worse than the cosine**: the search embedders, the
  NLI models, the web-search cross-encoder and the lexical scores. They
  were trained for other notions of "relevant" than "does the same job".
- **What the better scorers do differently.** For "load environment
  variables from a .env file in a node app", the reranker scores cross-env
  (sets variables but reads no `.env`) 0.10 and every Node dotenv package
  0.97 or more. The cosine ranks cross-env 5th.
- **The instruction matters for levels, not for ranking.** Without it,
  Qwen3-Embedding orders one idea's matches just as well, but its scores
  for different ideas aren't on one scale, so its AUC across ideas drops to
  the cosine's. A level built on a threshold needs that one scale.
- **Our own instruction partly fitted the labels.** On round 1 the
  reranker with our instruction led Qwen3-Embedding by 3.5 points; on the
  blind labels the two are level. It was written from the labels'
  definition of y, so part of its round-1 edge was agreement with that
  definition. Running it with that instruction on a GPU would need a server
  that accepts the instruction, which the current one doesn't.
- **At the top of the list the gain is small**: 48 to 51% y in the top 5,
  against 45% for the cosine. The larger gain is further down: the Qwen3
  models push the non-implementations lower, and give scores that one
  threshold can work with.
- **Scores are not probabilities.** 38% of the default reranker's n
  matches score above 0.5 (28% with our instruction). Before scores are added up into a level they need calibrating.

### A side finding: four "Open" ideas aren't

Round 1 labelled only the cosine's top 15, so the levels were judged on
that list. The Qwen3 models ranked high an implementation further down for
4 of the 14 Open ideas:

| idea | implementation | cosine rank | qwen3-embed rank |
|---|---|---|---|
| 032 (in Japanese: kill the process on a port) | okamyuji/devpulse | 48 of 63 | 1 |
| 052 (convert between markdown, latex and word) | jgm/pandoc | 23 of 87 | 6 |
| 060 (self-hosted, Bitwarden-compatible) | lj5645/FlexVault | 46 of 68 | 1 |
| 079 (browse OAI-PMH endpoints) | oai-pmh-mcp | 47 of 117 | 5 |

With round 2's y counted, two more ideas (047, 064) reach 5 y, Saturated by
the count. So the target levels for E4 need revising first, or E4 would
count finding pandoc as a mistake.

## Next

1. **Revise the levels after round 2.** Proposal: keep the labelled level
   as it is, so the agreement keeps measuring blind labels, and add a
   revised level beside it, as `from_y` is; let `from_y` count the y of
   both rounds. E2's bar then needs recounting against the revised levels.
2. **E4: from per-match scores to a level.** Features per idea from
   Qwen3-Embedding and from the reranker with our instruction; a refitted
   floor, an ordinal logistic regression and a small decision tree,
   checked by leaving one idea out at a time. Compared with the floor on
   false Opens, Opens found and QWK.
3. **To decide for E4:** the plan keeps the floor as a hard lower bound (a
   model may raise the level, never lower it). If that stays today's floor,
   E4 can't call Open any of the 10 Open ideas the floor misses, which is
   where most of the room is.

## Caveats

- Most labels come from one labeller, Claude; a person checked 12 ideas.
- The numbers are small: 75 ideas with a level, 14 of them Open, so each
  false Open moves the rate by more than a point.
- Round 1 labelled the cosine's top 15, which favours the cosine on the
  top-of-list measures. Round 2 corrects that only for each scorer's top 5.
- Round 1's p and round 2's p don't mean quite the same thing.
- Times are indicative: Python on a shared laptop CPU, or a home GPU.

## Reproducing

`data/runs.jsonl` is the frozen recording; `make record` doesn't need to run
again. The rest runs offline from it:

| command | does |
|---|---|
| `make e1` | data summary → `results/e1-*.tsv` |
| `make sheets`, `make round2` | labelling sheets → `labels/` |
| `make labels` | reads the sheets → `data/pairs.tsv`, `data/levels.tsv`, `results/labels-agreement.tsv` |
| `make prompts`, `make llm` | silt's verdict prompt per idea, and the LLM answers → `data/` |
| `make e2` | floors and LLMs against the levels → `results/e2-*.tsv` |
| `make scores` | every scorer over every candidate → `scores/` (the Qwen3 scorers need the GPU server) |
| `make e3` | scorers against the match labels → `results/e3-*.tsv` |

Details, gotchas and the full tables are in [Notes.md](Notes.md).
