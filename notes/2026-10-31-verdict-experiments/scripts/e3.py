"""E3: how well each per-match scorer tells implementations from the rest.

    python3 -I scripts/e3.py

Reads the labelled matches (data/pairs.tsv) and every scores/<scorer>.tsv
(scripts/score.py), and writes results/e3-scorers.tsv, one row per scorer,
labeller, rounds and task:

    rounds        the labelling rounds the row uses: each round alone, and
                  "1+2" for all of a labeller's rounds together
    task          y|pn: y against p and n; yp|n: y and p against n
    pairs, pos    labelled matches, and how many count as positive
    auc           over all labelled matches of all ideas: can one threshold
                  separate them? (what a level built on the score needs)
    auc_lo, auc_hi  its 95% interval, resampling ideas (1000 times)
    beats_cosine  share of the resamples in which the scorer's AUC is above
                  the recorded cosine's
    auc_idea      the mean AUC within an idea (ideas with both classes):
                  does it rank one idea's matches well?
    ndcg10        graded (y 2, p 1, n 0), over the labelled matches only
                  (a condensed list), averaged over ideas
    prec5         the share of the scorer's top 5 among all of an idea's
                  candidates that is labelled positive, averaged over
                  ideas; an unlabelled candidate counts as negative
    unjudged10    unlabelled candidates in the scorer's top 10, averaged
                  over ideas: how far the labels cover its picks
    ms50          median time for an idea with 50 matches

prec5 and unjudged10 are blank for a scorer run on the labelled matches
only (score.py --labelled), and for rows without round 1, whose labels
the later rounds only add to.

It prints the rows of Claude's labels, all rounds. A candidate without a
score (the recorded cosine has none for the few that only the keyword run
found) ranks last.

It also writes results/e3-targets.tsv, which needs no labels: for each
positive idea whose package was retrieved, the package's rank under each
scorer (1 is first; ties count against it). The package is found by
e1.py's rule, by name, so a namesake in another ecosystem can stand in
for it (059). Run from the experiment directory.
"""

import collections
import csv
import glob
import json
import math
import os
import random
import re
import statistics
import sys

GAIN = {"y": 2, "p": 1, "n": 0}
TASKS = {"y|pn": {"y"}, "yp|n": {"y", "p"}}
BOOT = 1000
SEED = 20261101


def read_tsv(path):
    with open(path) as f:
        return list(csv.DictReader(f, delimiter="\t"))


def auc(pairs):
    """Mann-Whitney AUC of (score, positive) pairs; ties count half."""
    pairs = sorted(pairs)
    pos = sum(p for _, p in pairs)
    neg = len(pairs) - pos
    if not pos or not neg:
        return None
    ranks, i = 0.0, 0
    while i < len(pairs):
        j = i
        while j < len(pairs) and pairs[j][0] == pairs[i][0]:
            j += 1
        r = (i + j + 1) / 2  # the mean rank of the tied block, from 1
        ranks += r * sum(p for _, p in pairs[i:j])
        i = j
    return (ranks - pos * (pos + 1) / 2) / (pos * neg)


def ndcg(labels, k=10):
    """labels: the labels in the scorer's order."""
    dcg = lambda ls: sum(GAIN[l] / math.log2(i + 2) for i, l in enumerate(ls[:k]))
    ideal = dcg(sorted(labels, key=GAIN.get, reverse=True))
    return dcg(labels) / ideal if ideal else None


def target_url(target, matches):
    """e1.target_rank's rule: the URL of the first match (by cosine) that is
    the target, or None."""
    names = target.split(":", 1)[1].lower().split("|")
    for m in matches:
        name = m["name"].lower()
        url = (m.get("url") or "").lower().rstrip("/")
        last = re.split(r"[/:]", name)[-1]
        for t in names:
            if name == t or name.endswith("/" + t) or name.endswith(":" + t) \
                    or url.endswith("/" + t) or name.startswith(t + " ") \
                    or ("/" not in t and last == t):
                return m["url"]
    return None


def targets(scorers):
    """Per positive idea with its package retrieved: the package's rank
    under each scorer."""
    meta = {r["id"]: r for r in read_tsv("ideas.tsv")}
    rows = []
    with open("data/runs.jsonl") as f:
        for r in map(json.loads, f):
            t = meta[r["id"]]["target"]
            if "error" in r or t == "-":
                continue
            url = target_url(t, r["embeddings"]["matches"])
            if url is None:
                continue
            row = {"idea": r["id"], "target": t.split("|")[0], "url": url}
            for name, sc in scorers.items():
                if sc["partial"]:
                    continue
                mine = sc["by_idea"][r["id"]]
                row[name] = 1 + sum(v >= mine[url] for u, v in mine.items() if u != url)
            rows.append(row)
    return rows


def main():
    pairs = read_tsv("data/pairs.tsv")
    scorers = {}
    for path in sorted(glob.glob("scores/*.tsv")):
        name = os.path.basename(path)[:-4]
        if name == "runs":
            continue
        rows = read_tsv(path)
        scorers[name] = {
            "score": {(r["idea"], r["url"]): float(r["score"]) if r["score"] else -math.inf
                      for r in rows},
            "ms": collections.defaultdict(list),
            "by_idea": collections.defaultdict(dict),
        }
        for r in rows:
            if r["ms"]:
                scorers[name]["ms"][r["idea"]].append(float(r["ms"]))
        for (idea, url), v in scorers[name]["score"].items():
            scorers[name]["by_idea"][idea][url] = v
    if "cosine" not in scorers:
        sys.exit("scores/cosine.tsv is missing: uv run scripts/score.py cosine")
    every = len(scorers["cosine"]["score"])
    for sc in scorers.values():
        sc["partial"] = len(sc["score"]) < every

    groups = []
    for who in ("claude", "human"):
        rounds = sorted({p["round"] for p in pairs if p["labeller"] == who}, key=int)
        groups += [(who, [r]) for r in rounds]
        if len(rounds) > 1:
            groups.append((who, rounds))

    out = []
    for who, rounds in groups:
        mine = [p for p in pairs if p["labeller"] == who and p["round"] in rounds]
        label_of = {(p["idea"], p["url"]): p["label"] for p in mine}
        whole = "1" in rounds  # prec5 and unjudged10 need round 1's labels
        by_idea = collections.defaultdict(list)
        for p in mine:
            by_idea[p["idea"]].append((p["url"], p["label"]))
        ideas = sorted(by_idea)
        rng = random.Random(SEED)
        samples = [[rng.choice(ideas) for _ in ideas] for _ in range(BOOT)]

        def scored(name, idea):
            s = scorers[name]["score"]
            return [(s.get((idea, url), -math.inf), label) for url, label in by_idea[idea]]

        cache = {}

        def boot_aucs(name, task):
            if (name, task) not in cache:
                per = {i: [(s, l in TASKS[task]) for s, l in scored(name, i)] for i in ideas}
                cache[name, task] = [auc([x for i in smp for x in per[i]]) for smp in samples]
            return cache[name, task]

        for name, sc in scorers.items():
            # The scorer's order of all of an idea's candidates. Ties keep
            # the file's order, which is the cosine's, as labelsheet.py's
            # round 2 does.
            cands = collections.defaultdict(list)
            for (idea, url), v in sc["score"].items():
                if idea in by_idea:
                    cands[idea].append((v, url))
            top = {i: [u for _, u in sorted(cands[i], key=lambda x: -x[0])] for i in ideas}
            covered = whole and not sc["partial"]
            unjudged = [sum((i, u) not in label_of for u in top[i][:10]) for i in ideas]
            nd = [ndcg([l for _, l in sorted(scored(name, i), key=lambda x: -x[0])]) for i in ideas]
            nd = [x for x in nd if x is not None]
            ms = [50 * statistics.mean(v) for i, v in sc["ms"].items() if i in by_idea]
            for task, positive in TASKS.items():
                flat = [(s, l in positive) for i in ideas for s, l in scored(name, i)]
                per_idea = [auc([(s, l in positive) for s, l in scored(name, i)]) for i in ideas]
                per_idea = [x for x in per_idea if x is not None]
                b = sorted(x for x in boot_aucs(name, task) if x is not None)
                c = boot_aucs("cosine", task)
                beats = statistics.mean(x > y for x, y in zip(boot_aucs(name, task), c)
                                        if x is not None and y is not None)
                prec5 = statistics.mean(
                    sum(label_of.get((i, u)) in positive for u in top[i][:5]) / len(top[i][:5])
                    for i in ideas)
                out.append({
                    "scorer": name, "labeller": who, "rounds": "+".join(rounds),
                    "task": task, "ideas": len(ideas),
                    "pairs": len(flat), "pos": sum(p for _, p in flat),
                    "auc": f"{auc(flat):.3f}",
                    "auc_lo": f"{b[int(0.025 * len(b))]:.3f}",
                    "auc_hi": f"{b[int(0.975 * len(b)) - 1]:.3f}",
                    "beats_cosine": "" if name == "cosine" else f"{beats:.2f}",
                    "auc_idea": f"{statistics.mean(per_idea):.3f}",
                    "ndcg10": f"{statistics.mean(nd):.3f}",
                    "prec5": f"{prec5:.3f}" if covered else "",
                    "unjudged10": f"{statistics.mean(unjudged):.1f}" if covered else "",
                    "ms50": f"{statistics.median(ms):.1f}" if ms else "",
                })

    os.makedirs("results", exist_ok=True)
    cols = list(out[0])
    with open("results/e3-scorers.tsv", "w", newline="") as f:
        w = csv.DictWriter(f, cols, delimiter="\t", lineterminator="\n")
        w.writeheader()
        w.writerows(out)
    w = csv.writer(sys.stdout, delimiter="\t", lineterminator="\n")
    show = ["scorer", "task", "auc", "auc_lo", "auc_hi", "beats_cosine", "auc_idea",
            "ndcg10", "prec5", "unjudged10", "ms50"]
    every_round = "+".join(max((r for who, r in groups if who == "claude"), key=len))
    print(f"# Claude's labels, rounds {every_round}")
    w.writerow(show)
    for r in sorted((r for r in out if r["labeller"] == "claude" and r["rounds"] == every_round),
                    key=lambda r: (r["task"], -float(r["auc"]))):
        w.writerow([r[c] for c in show])

    trows = targets(scorers)
    with open("results/e3-targets.tsv", "w", newline="") as f:
        tw = csv.DictWriter(f, list(trows[0]), delimiter="\t", lineterminator="\n")
        tw.writeheader()
        tw.writerows(trows)
    print(f"\n# the reworded package's rank, {len(trows)} positive ideas")
    w.writerow(["scorer", "top1", "top5", "top15", "median"])
    full = [n for n in scorers if not scorers[n]["partial"]]
    for name in sorted(full, key=lambda n: statistics.median(r[n] for r in trows)):
        ranks = [r[name] for r in trows]
        w.writerow([name, sum(x == 1 for x in ranks), sum(x <= 5 for x in ranks),
                    sum(x <= 15 for x in ranks), statistics.median(ranks)])


if __name__ == "__main__":
    main()
