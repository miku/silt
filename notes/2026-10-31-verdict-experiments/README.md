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
