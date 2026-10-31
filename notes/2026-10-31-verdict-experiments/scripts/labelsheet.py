"""Writes a labelling sheet: the matches to judge per idea, one per line, each
with a "_" to replace by y, p or n.

    python3 -I scripts/labelsheet.py labels/claude.txt              # every idea
    python3 -I scripts/labelsheet.py --sample 4 labels/human.txt    # 4 per stratum

The matches per idea are the top 15 by similarity and the top 5 by keyword
score, so that the labels don't only cover what the cosine ranks high. They
are shuffled, and the similarity isn't shown, so that the order and the score
don't suggest an answer. An existing sheet is never overwritten: it may hold
someone's labels.

Run from the experiment directory; reads data/runs.jsonl.
"""

import argparse
import hashlib
import json
import os
import random
import sys

TOP_COSINE = 15
TOP_KEYWORD = 5
SEED = 20261031

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
    ap.add_argument("--runs", default="data/runs.jsonl")
    args = ap.parse_args()

    if os.path.exists(args.out):
        sys.exit(f"{args.out} exists; it may hold labels, so it is left alone")

    with open(args.runs) as f:
        runs = [r for r in map(json.loads, f) if "embeddings" in r and r["embeddings"]["matches"]]
    runs.sort(key=lambda r: r["id"])
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
