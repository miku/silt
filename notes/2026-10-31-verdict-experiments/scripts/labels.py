"""Reads the filled-in labelling sheets into tables, checks them, and reports
how far the labellers agree on the ideas they share.

    python3 -I scripts/labels.py                 # every labels/*.txt
    python3 -I scripts/labels.py labels/human.txt

Writes

    data/pairs.tsv     idea, url, label (y, p, n), labeller, note
    data/levels.tsv    idea, level, labeller, the y/p/n counts, the level the
                       y count gives (0 open, 1-4 crowded, 5+ saturated), note
    results/labels-agreement.tsv

and prints the checks and the agreement. A match still marked "_" is left
out of pairs.tsv; an idea without a level is left out of levels.tsv. The
labeller is the sheet's name, "human" for labels/human.txt. A sheet named
<labeller>-round2.txt holds that labeller's matches of round 2: pairs.tsv
gets them with round 2, and it has no levels. The agreement counts round 1
only, so that it keeps measuring blind labels. Run from the experiment
directory.
"""

import collections
import csv
import glob
import os
import re
import sys

LABELS = ("y", "p", "n")
LEVELS = ("open", "crowded", "saturated", "vague")

IDEA = re.compile(r"^== (\d+)  (.*)$")
LEVEL = re.compile(r"^level:\s*([a-z]*)\s*(?:#\s*(.*))?$")
# The URL is the last <...> on the line; a description may hold "#" or "<".
MATCH = re.compile(r"^(\S+)\s+\[[^\]]+\] .*<([^<>\s]+)>\s*(?:#\s*(.*))?$")


def from_y(y):
    return "open" if y == 0 else "crowded" if y <= 4 else "saturated"


def parse(path):
    """Returns (pairs, levels, problems) of one sheet."""
    who, _, rnd = os.path.splitext(os.path.basename(path))[0].partition("-round")
    rnd = int(rnd or 1)
    pairs, levels, problems = [], [], []
    idea, level = None, None

    def close():
        if idea is None:
            return
        if rnd > 1:
            got = sum(p["label"] == "_" for p in pairs if p["idea"] == idea)
            if got:
                problems.append(f"{who} round {rnd} {idea}: {got} match(es) not labelled")
            return
        got = collections.Counter(p["label"] for p in pairs if p["idea"] == idea)
        row = {"idea": idea, "level": level[0] if level else "", "labeller": who,
               "y": got["y"], "p": got["p"], "n": got["n"], "unlabelled": got["_"],
               "from_y": from_y(got["y"]), "note": level[1] if level else ""}
        if not row["level"]:
            problems.append(f"{who} {idea}: no level (the y count gives {row['from_y']})")
        elif row["level"] != "vague" and row["level"] != row["from_y"]:
            # Allowed (the count is a guide), but worth a look.
            problems.append(f"{who} {idea}: level {row['level']}; its {got['y']} y alone give {row['from_y']}")
        if got["_"]:
            problems.append(f"{who} {idea}: {got['_']} match(es) not labelled")
        levels.append(row)

    with open(path) as f:
        for n, line in enumerate(f, 1):
            line = line.rstrip("\n")
            if not line.strip() or line.startswith("#"):
                continue
            if m := IDEA.match(line):
                close()
                idea, level = m.group(1), None
            elif m := LEVEL.match(line):
                if m.group(1) and m.group(1) not in LEVELS:
                    problems.append(f"{path}:{n}: unknown level {m.group(1)!r}")
                level = (m.group(1), m.group(2) or "")
            elif m := MATCH.match(line):
                label = m.group(1)
                if label not in LABELS + ("_",):
                    problems.append(f"{path}:{n}: unknown label {label!r}")
                    continue
                pairs.append({"idea": idea, "url": m.group(2), "label": label,
                              "labeller": who, "note": m.group(3) or "", "round": rnd})
            else:
                problems.append(f"{path}:{n}: not understood: {line[:60]}")
    close()
    return pairs, [r for r in levels if r["level"]], problems


def kappa(a, b, cats):
    """Cohen's kappa of two equally long lists of labels."""
    n = len(a)
    if not n:
        return None
    po = sum(x == y for x, y in zip(a, b)) / n
    ca, cb = collections.Counter(a), collections.Counter(b)
    pe = sum(ca[c] * cb[c] for c in cats) / n / n
    return 1.0 if pe == 1 else (po - pe) / (1 - pe)


def confusion(a, b, cats, names):
    """A labeller-by-labeller table as text, rows the first labeller."""
    c = collections.Counter(zip(a, b))
    w = max(map(len, cats)) + 2
    out = [f"{names[0]} \\ {names[1]}".ljust(w + 4) + "".join(x.rjust(w) for x in cats)]
    for x in cats:
        out.append(x.ljust(w + 4) + "".join(str(c[x, y]).rjust(w) for y in cats))
    return "\n".join(out)


def agreement(pairs, levels):
    """Agreement of every two labellers on what both labelled."""
    rows, text = [], []
    who = sorted({p["labeller"] for p in pairs} | {l["labeller"] for l in levels})
    for i, a in enumerate(who):
        for b in who[i + 1:]:
            la = {(p["idea"], p["url"]): p["label"] for p in pairs if p["labeller"] == a and p["label"] != "_"}
            lb = {(p["idea"], p["url"]): p["label"] for p in pairs if p["labeller"] == b and p["label"] != "_"}
            both = sorted(la.keys() & lb.keys())
            xa, xb = [la[k] for k in both], [lb[k] for k in both]
            va = {l["idea"]: l["level"] for l in levels if l["labeller"] == a}
            vb = {l["idea"]: l["level"] for l in levels if l["labeller"] == b}
            ideas = sorted(va.keys() & vb.keys())
            ya, yb = [va[k] for k in ideas], [vb[k] for k in ideas]
            for what, xs, ys, cats in [
                ("match y/p/n", xa, xb, LABELS),
                ("match y vs p+n", [x == "y" for x in xa], [x == "y" for x in xb], (True, False)),
                ("match y+p vs n", [x != "n" for x in xa], [x != "n" for x in xb], (True, False)),
                ("level", ya, yb, LEVELS),
            ]:
                if not xs:
                    continue
                k = kappa(xs, ys, cats)
                rows.append({"labellers": f"{a}-{b}", "measure": what, "n": len(xs),
                             "agree": f"{sum(x == y for x, y in zip(xs, ys)) / len(xs):.2f}",
                             "kappa": "" if k is None else f"{k:.2f}"})
            if xa:
                text.append(confusion(xa, xb, LABELS, (a, b)))
            if ya:
                text.append(confusion(ya, yb, LEVELS, (a, b)))
    return rows, text


def write(path, rows, cols):
    with open(path, "w", newline="") as f:
        w = csv.DictWriter(f, cols, delimiter="\t", lineterminator="\n", extrasaction="ignore")
        w.writeheader()
        w.writerows(rows)


def main():
    paths = sys.argv[1:] or sorted(glob.glob("labels/*.txt"))
    pairs, levels, problems = [], [], []
    for path in paths:
        p, l, pr = parse(path)
        done = [x for x in p if x["label"] != "_"]
        pairs += p
        levels += l
        # A sheet nobody has started would only list every idea as missing.
        problems += pr if done or l else [f"{path}: not started"]
        print(f"{path}: {len({x['idea'] for x in p})} ideas, {len(done)}/{len(p)} matches "
              f"labelled ({', '.join(f'{c} {sum(x['label'] == c for x in done)}' for c in LABELS)}), "
              f"{len(l)} levels", file=sys.stderr)
    for pr in problems:
        print("  " + pr, file=sys.stderr)

    write("data/pairs.tsv", [p for p in pairs if p["label"] != "_"],
          ["idea", "url", "label", "labeller", "note", "round"])
    write("data/levels.tsv", levels,
          ["idea", "level", "labeller", "y", "p", "n", "unlabelled", "from_y", "note"])
    rows, text = agreement([p for p in pairs if p["round"] == 1], levels)
    os.makedirs("results", exist_ok=True)
    write("results/labels-agreement.tsv", rows, ["labellers", "measure", "n", "agree", "kappa"])
    if rows:
        print()
        w = csv.DictWriter(sys.stdout, list(rows[0]), delimiter="\t", lineterminator="\n")
        w.writeheader()
        w.writerows(rows)
        for t in text:
            print("\n" + t)


if __name__ == "__main__":
    main()
