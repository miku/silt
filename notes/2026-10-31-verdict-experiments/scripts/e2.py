"""E2: the baselines against the level labels.

    python3 -I scripts/e2.py

Compares the levels of

    floor        silt --fast: the level from the similarity data
                 (embeddings.verdict.level in data/runs.jsonl)
    floor_kw     the same rule on the --keyword-only scores
    <model>      the level an LLM answered (data/llm-<model>.jsonl, written
                 by scripts/llm.py), before silt floors it
    <model>+floor  what silt shows with that LLM today: Open if the best
                 similarity is below 0.35 (no LLM call), Open if the answer
                 doesn't parse or took over 120 s (silt's fallback),
                 otherwise the higher of the model's level and the floor

with the targets

    claude, claude_from_y   Claude's level and the level its y count gives,
                            for the ideas it didn't call vague
    human, human_from_y     the same for the person's sample

Writes results/e2-ideas.tsv (one row per idea), results/e2-summary.tsv (per
method, target and stratum: false Opens, QWK, exact agreement),
results/e2-confusion.tsv and results/e2-llm.tsv (per model: answers,
errors, time), and prints the main numbers. Ideas a labeller called vague
are left out of the scores; the summary lists what each method says for
them (target "vague"). Run from the experiment directory.
"""

import csv
import glob
import json
import os
import re
import statistics
import sys

LEVELS = ("open", "crowded", "saturated")
RANK = {l: i for i, l in enumerate(LEVELS)}
MIN_RELEVANCE = 0.35  # internal/pipeline.MinRelevance
STRATA = ("all", "parity", "positive", "specific", "vague")
TARGETS = ("claude", "claude_from_y", "human", "human_from_y")


def read_tsv(path):
    with open(path) as f:
        return list(csv.DictReader(f, delimiter="\t"))


def write_tsv(path, rows):
    cols = list(dict.fromkeys(k for r in rows for k in r))
    with open(path, "w", newline="") as f:
        w = csv.DictWriter(f, cols, delimiter="\t", lineterminator="\n", restval="")
        w.writeheader()
        w.writerows(rows)


def read_llm(path):
    """The last answer per idea of one data/llm-<model>.jsonl."""
    with open(path) as f:
        return {r["id"]: r for r in map(json.loads, f)}


def failed(answer):
    """A request that failed: the server's fault, not the model's."""
    return answer["error"].startswith("request:")


def silt_level(answer, floor, max_sim):
    """What silt shows with the LLM on, from the recorded answer."""
    if max_sim < MIN_RELEVANCE:
        return "open"
    if answer is None or failed(answer):
        return ""
    if not answer["level"] or answer["over_120s"]:
        return "open"
    return max(answer["level"], floor, key=RANK.get)


def qwk(pairs):
    """Quadratic weighted kappa of (target, predicted) level pairs."""
    k = len(LEVELS)
    obs = [[0] * k for _ in range(k)]
    for t, p in pairs:
        obs[RANK[t]][RANK[p]] += 1
    n = len(pairs)
    rows = [sum(r) for r in obs]
    cols = [sum(obs[i][j] for i in range(k)) for j in range(k)]
    w = lambda i, j: (i - j) ** 2 / (k - 1) ** 2
    num = sum(w(i, j) * obs[i][j] for i in range(k) for j in range(k))
    den = sum(w(i, j) * rows[i] * cols[j] / n for i in range(k) for j in range(k)) if n else 0
    return 1 - num / den if den else None


def ratio(k, n):
    return f"{k}/{n}" if n else ""


def fmt(x):
    return "" if x is None else f"{x:.2f}"


def score(ideas, method, target):
    """One summary row's numbers. Ideas without the target (vague, not
    labelled) are left out; so are ideas the method has no level for, which
    are counted as missing."""
    have = [i for i in ideas if i.get(target) in LEVELS]
    pairs = [(i[target], i[method]) for i in have if i.get(method) in LEVELS]
    not_open = [(t, p) for t, p in pairs if t != "open"]
    is_open = [(t, p) for t, p in pairs if t == "open"]
    return {
        "n": len(pairs),
        "missing": len(have) - len(pairs),
        "false_open": ratio(sum(p == "open" for _, p in not_open), len(not_open)),
        "missed_open": ratio(sum(p != "open" for _, p in is_open), len(is_open)),
        "qwk": fmt(qwk(pairs)),
        "exact": fmt(sum(t == p for t, p in pairs) / len(pairs)) if pairs else "",
        "under": sum(RANK[p] < RANK[t] for t, p in pairs),
        "over": sum(RANK[p] > RANK[t] for t, p in pairs),
        "pred_o/c/s": "/".join(str(sum(p == l for _, p in pairs)) for l in LEVELS),
    }, pairs


def main():
    meta = {r["id"]: r for r in read_tsv("ideas.tsv")}
    labels = {}
    for r in read_tsv("data/levels.tsv"):
        labels.setdefault(r["idea"], {})[r["labeller"]] = r
    with open("data/runs.jsonl") as f:
        runs = {r["id"]: r for r in map(json.loads, f) if "error" not in r}
    llms = {re.sub(r"^llm-|\.jsonl$", "", os.path.basename(p)): read_llm(p)
            for p in sorted(glob.glob("data/llm-*.jsonl"))}

    ideas = []
    for id_, r in sorted(runs.items()):
        ms = r["embeddings"]["matches"]
        max_sim = ms[0]["similarity"] if ms else 0.0
        row = {"id": id_, "stratum": meta[id_]["stratum"], "idea": meta[id_]["idea"],
               "max_sim": f"{max_sim:.2f}"}
        for who in ("claude", "human"):
            lab = labels.get(id_, {}).get(who)
            row[who] = lab["level"] if lab else ""
            row[who + "_from_y"] = lab["from_y"] if lab and lab["level"] in LEVELS else ""
        row["floor"] = r["embeddings"]["verdict"]["level"]
        row["floor_kw"] = r["keyword_only"]["verdict"]["level"]
        for m, answers in llms.items():
            a = answers.get(id_)
            row[m] = a["level"] if a else ""
            row[m + "+floor"] = silt_level(a, row["floor"], max_sim)
            row[m + "_s"] = a["seconds"] if a else ""
        ideas.append(row)

    methods = ["floor", "floor_kw"] + [x for m in llms for x in (m, m + "+floor")]
    summary, confusion = [], []
    for method in methods:
        for target in TARGETS:
            for stratum in STRATA:
                if stratum == "vague" or (stratum != "all" and target != "claude"):
                    continue
                sub = [i for i in ideas if stratum in ("all", i["stratum"])]
                s, pairs = score(sub, method, target)
                summary.append({"method": method, "target": target, "stratum": stratum} | s)
                if stratum == "all":
                    for t in LEVELS:
                        confusion.append({"method": method, "target": target, "true": t} | {
                            "pred_" + p: sum(pair == (t, p) for pair in pairs) for p in LEVELS})
        # What the method says for the ideas Claude called vague.
        vague = [i[method] for i in ideas if i["claude"] == "vague"]
        summary.append({"method": method, "target": "vague", "stratum": "all", "n": len(vague),
                        "missing": sum(v not in LEVELS for v in vague),
                        "pred_o/c/s": "/".join(str(vague.count(l)) for l in LEVELS)})

    timing = []
    for m, answers in llms.items():
        asked = [answers[i["id"]] for i in ideas if i["id"] in answers]
        rs = [r for r in asked if not failed(r)]
        ok = [r for r in rs if r["level"]]
        secs = sorted(r["seconds"] for r in rs)
        med = lambda k: statistics.median(v) if (v := [r[k] for r in ok if r.get(k) is not None]) else ""
        floor = {i["id"]: i["floor"] for i in ideas}
        timing.append({
            "model": m, "api": rs[0]["api"] if rs else "", "url": rs[0]["url"] if rs else "",
            "ideas": len(ideas), "answered": len(rs), "levels": len(ok),
            "unparsed": len(rs) - len(ok), "request_failed": len(asked) - len(rs),
            "below_floor": sum(RANK[r["level"]] < RANK[floor[r["id"]]] for r in ok),
            "at_floor": sum(r["level"] == floor[r["id"]] for r in ok),
            "above_floor": sum(RANK[r["level"]] > RANK[floor[r["id"]]] for r in ok),
            "s_median": statistics.median(secs) if secs else "",
            "s_p90": secs[int(0.9 * (len(secs) - 1))] if secs else "",
            "s_max": secs[-1] if secs else "",
            "over_120s": sum(r["over_120s"] for r in rs),
            "prompt_tokens_median": med("prompt_tokens"),
            "output_tokens_median": med("output_tokens"),
            "prefill_s_median": med("prefill_s"),
            "decode_s_median": med("decode_s"),
        })

    os.makedirs("results", exist_ok=True)
    write_tsv("results/e2-ideas.tsv", ideas)
    write_tsv("results/e2-summary.tsv", summary)
    write_tsv("results/e2-confusion.tsv", confusion)
    write_tsv("results/e2-llm.tsv", timing)

    out = csv.writer(sys.stdout, delimiter="\t", lineterminator="\n")
    print("# all strata, ideas not called vague")
    cols = ["method", "target", "n", "missing", "false_open", "missed_open", "qwk", "exact",
            "under", "over", "pred_o/c/s"]
    out.writerow(cols)
    for s in summary:
        if s["stratum"] == "all" and s["target"] != "vague":
            out.writerow([s.get(c, "") for c in cols])
    print("\n# confusion against Claude's level (rows: target, columns: predicted o/c/s)")
    for method in methods:
        rows = [c for c in confusion if c["method"] == method and c["target"] == "claude"]
        print(method + "\t" + "  ".join(f"{c['true'][0]}: " + " ".join(
            f"{c['pred_' + p]:>2}" for p in LEVELS) for c in rows))
    if timing:
        print("\n# LLM answers and time")
        out.writerow(timing[0].keys())
        for t in timing:
            out.writerow(t.values())


if __name__ == "__main__":
    main()
