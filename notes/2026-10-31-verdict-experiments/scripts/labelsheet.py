"""Writes a labelling sheet: the matches to judge per idea, one per line, each
with a "_" to replace by y, p or n.

    python3 -I scripts/labelsheet.py labels/claude.txt              # every idea
    python3 -I scripts/labelsheet.py --sample 4 labels/human.txt    # 4 per stratum
    python3 -I scripts/labelsheet.py --round2 claude labels/claude-round2.txt

--round2 LABELLER writes round 2: for each idea LABELLER labelled match by
match, the matches in the top 5 of any scorer in scores/ (E3) that round 1
didn't list. A scorer run on the labelled matches only is left out. Ties in
a score keep the cosine's order. Round 2 has no levels, and its header
gives p the one meaning decided after round 1.

The matches per idea are the top 15 by similarity and the top 5 by keyword
score, so that the labels don't only cover what the cosine ranks high. They
are shuffled, and the similarity isn't shown, so that the order and the score
don't suggest an answer. An existing sheet is never overwritten: it may hold
someone's labels.

Run from the experiment directory; reads data/runs.jsonl.
"""

import argparse
import collections
import csv
import glob
import hashlib
import json
import os
import random
import sys

TOP_COSINE = 15
TOP_KEYWORD = 5
TOP_ROUND2 = 5
SEED = 20261031

HEADER_ROUND2 = """\
# Labelling sheet for the verdict experiments, round 2 (README.md, E3).
#
# The matches below are those the per-match scorers rank in their top 5
# that round 1 didn't list. Label each one; there is no level this time.
# Replace the "_" at the start of a line with
#
#   y  implements the idea: it is such a tool, or does the same job in
#      another form (a CLI idea met by a library or an MCP server, a VS
#      Code idea by a JetBrains plugin), even if its description doesn't
#      mention every part of the idea
#   p  near the idea and worth seeing: a nearby tool on the same data or
#      problem, or one that does only part of the job
#   n  no: only the same topic, ecosystem or category; or the same job for
#      another language or platform (a .env loader for Java, for a Node
#      idea)
#
# Judge the match as a tool, not its description's wording; follow the link
# when unsure, or leave the "_" and move on. Notes after a "#" are welcome.
"""

HEADER = """\
# Labelling sheet for the verdict experiments (README.md).
#
# For each match, replace the "_" at the start of its line with
#
#   y  implements the idea: it is such a tool, or does the same job
#   p  partial: covers part of the idea, or a close variant of it
#   n  no: only related (same topic, ecosystem or category)
#
# Then fill in "level:" for each idea: how crowded the space is, judged from
# the matches listed. The number of y is a guide, not the rule; the age, size
# and quality of the matches may move it (say why in a note after "#"):
#
#   open       nothing listed implements the idea (no y)
#   crowded    a few implement it (guide: 1 to 4 y)
#   saturated  many implement it (guide: 5 or more y)
#   vague      the idea is too vague to judge
#
# Judge the match as a tool, not its description's wording; follow the link
# when unsure, or leave the "_" and move on. Notes after a "#" are welcome
# anywhere, e.g. a well-known tool that should have turned up but didn't.
"""


def key(m):
    return m.get("url") or f"{m['source']}:{m['name']}"


def pool(run):
    """The matches to label for one run, without duplicates."""
    picked, seen = [], set()
    kw = (run.get("keyword_only") or {}).get("matches") or []
    for m in run["embeddings"]["matches"][:TOP_COSINE] + kw[:TOP_KEYWORD]:
        if key(m) not in seen:
            seen.add(key(m))
            picked.append(m)
    # A fixed shuffle per idea: the same sheet on every run of this script.
    return sorted(picked, key=lambda m: hashlib.sha1((run["id"] + key(m)).encode()).hexdigest())


def round2(runs, who):
    """{idea: [match]}: the top TOP_ROUND2 of every full scorer that round 1
    didn't list, for the ideas `who` labelled match by match."""
    with open("data/pairs.tsv") as f:
        done = collections.defaultdict(set)
        for r in csv.DictReader(f, delimiter="\t"):
            if r["labeller"] == who:
                done[r["idea"]].add(r["url"])
    with open(f"labels/{who}.txt") as f:
        listed = collections.defaultdict(set)  # round 1, labelled or not
        idea = None
        for ln in f:
            if ln.startswith("== "):
                idea = ln.split()[1]
            elif "<" in ln and idea and not ln.startswith("#"):
                listed[idea].add(ln.rsplit("<", 1)[1].split(">")[0])
    scores = {}
    for path in sorted(glob.glob("scores/*.tsv")):
        if path.endswith("/runs.tsv"):
            continue
        with open(path) as f:
            rows = list(csv.DictReader(f, delimiter="\t"))
        by = collections.defaultdict(list)
        for i, r in enumerate(rows):
            # File order is the cosine's order, which breaks ties.
            by[r["idea"]].append((-(float(r["score"]) if r["score"] else -1e30), i, r["url"]))
        scores[path] = by
    full = max(sum(map(len, s.values())) for s in scores.values())
    scores = {p: s for p, s in scores.items() if sum(map(len, s.values())) == full}
    out = {}
    for r in runs:
        if r["id"] not in done:
            continue
        ms = {key(m): m for m in r["embeddings"]["matches"] + r["keyword_only"]["matches"]}
        new = set()
        for by in scores.values():
            new |= {u for _, _, u in sorted(by[r["id"]])[:TOP_ROUND2]}
        new -= listed[r["id"]]
        if new:
            out[r["id"]] = sorted((ms[u] for u in new),
                                  key=lambda m: hashlib.sha1((r["id"] + key(m)).encode()).hexdigest())
    return out, sorted(os.path.basename(p)[:-4] for p in scores)


def line(m):
    desc = " ".join((m.get("description") or "").split())
    if len(desc) > 160:
        desc = desc[:157] + "..."
    facts = []
    if m.get("popularity") is not None:
        facts.append(f"pop {m['popularity']}")
    if m.get("last_updated"):
        facts.append(f"updated {m['last_updated'][:7]}")
    extra = f" ({', '.join(facts)})" if facts else ""
    return f"_  [{m['source']}] {m['name']}: {desc}{extra}  <{key(m)}>"


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("out", help="the sheet to write, e.g. labels/human.txt")
    ap.add_argument("--sample", type=int, default=0, help="ideas per stratum (default: all ideas)")
    ap.add_argument("--round2", metavar="LABELLER", help="round 2 for LABELLER's ideas")
    ap.add_argument("--runs", default="data/runs.jsonl")
    args = ap.parse_args()

    if os.path.exists(args.out):
        sys.exit(f"{args.out} exists; it may hold labels, so it is left alone")

    with open(args.runs) as f:
        runs = [r for r in map(json.loads, f) if "embeddings" in r and r["embeddings"]["matches"]]
    runs.sort(key=lambda r: r["id"])
    if args.round2:
        picked, used = round2(runs, args.round2)
        os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
        with open(args.out, "w") as f:
            f.write(HEADER_ROUND2)
            for r in runs:
                if r["id"] in picked:
                    f.write(f"\n== {r['id']}  {r['idea']}\n\n")
                    for m in picked[r["id"]]:
                        f.write(line(m) + "\n")
        print(f"{args.out}: {len(picked)} ideas, {sum(map(len, picked.values()))} matches "
              f"to label, from {', '.join(used)}", file=sys.stderr)
        return
    if args.sample:
        # Not from the vague ideas: "does this implement 'a tool for
        # developers'?" has no answer, so they only get a level.
        rng = random.Random(SEED)
        strata = sorted({r["stratum"] for r in runs} - {"vague"})
        chosen = set()
        for s in strata:
            ids = [r["id"] for r in runs if r["stratum"] == s]
            chosen.update(rng.sample(ids, min(args.sample, len(ids))))
        runs = [r for r in runs if r["id"] in chosen]

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    n = 0
    with open(args.out, "w") as f:
        f.write(HEADER)
        for r in runs:
            f.write(f"\n== {r['id']}  {r['idea']}\nlevel: \n\n")
            for m in pool(r):
                f.write(line(m) + "\n")
                n += 1
    print(f"{args.out}: {len(runs)} ideas, {n} matches to label", file=sys.stderr)


if __name__ == "__main__":
    main()
