"""E1: what the recorded data looks like.

    python3 -I scripts/e1.py

Writes results/e1-ideas.tsv (one row per idea) and results/e1-strata.tsv (one
row per stratum), and prints the second. Run from the experiment directory;
reads data/runs.jsonl.

For the positive ideas, "target" is the package the idea rewords; it counts
as found if a match has that name, in any source (ripgrep may turn up on
GitHub or PyPI rather than crates.io). Its rank is by similarity, from 1.
"""

import csv
import json
import os
import re
import statistics
import sys


def target_rank(target, matches):
    if target == "-":
        return None
    names = target.split(":", 1)[1].lower().split("|")
    for i, m in enumerate(matches, 1):
        name = m["name"].lower()
        last = re.split(r"[/:]", name)[-1]
        for t in names:
            if name == t or name.endswith("/" + t) or name.endswith(":" + t) or last == t \
                    or name.startswith(t + " "):
                return i
    return 0


def row(r):
    base = {"id": r["id"], "stratum": r["stratum"], "idea": r["idea"]}
    if "error" in r:
        return base | {"floor": "rejected", "floor_kw": "rejected"}
    emb, kw = r["embeddings"], r.get("keyword_only") or {}
    ms = emb["matches"]
    rank = target_rank(r["target"], ms)
    return base | {
        "floor": emb["verdict"]["level"],
        "floor_kw": (kw.get("verdict") or {}).get("level", ""),
        "candidates": len(ms),
        "max_sim": f"{ms[0]['similarity']:.2f}" if ms else "",
        "c55": sum(m["similarity"] >= 0.55 for m in ms),
        "sources_ok": ",".join(emb["verdict"]["sources_checked"]),
        "sources_failed": ",".join(emb["verdict"]["sources_failed"]),
        "target_rank": "" if rank is None else rank,
        "seconds": f"{r['seconds']:.1f}",
    }


def strata(rows):
    out = []
    for s in ["parity", "positive", "specific", "vague", "all"]:
        rs = [r for r in rows if s in ("all", r["stratum"])]
        ok = [r for r in rs if r["floor"] != "rejected"]
        pos = [r for r in ok if r.get("target_rank") != ""]
        level = lambda col, v: sum(r[col] == v for r in ok)
        out.append({
            "stratum": s,
            "ideas": len(rs),
            "rejected": len(rs) - len(ok),
            "candidates_median": statistics.median([r["candidates"] for r in ok]) if ok else "",
            "with_failed_source": sum(bool(r["sources_failed"]) for r in ok),
            "floor_o/c/s": "/".join(str(level("floor", v)) for v in ("open", "crowded", "saturated")),
            "floor_kw_o/c/s": "/".join(str(level("floor_kw", v)) for v in ("open", "crowded", "saturated")),
            "target_found": f"{sum(r['target_rank'] > 0 for r in pos)}/{len(pos)}" if pos else "",
            "target_top15": f"{sum(0 < r['target_rank'] <= 15 for r in pos)}/{len(pos)}" if pos else "",
        })
    return out


def write(path, rows):
    cols = list(dict.fromkeys(k for r in rows for k in r))
    with open(path, "w", newline="") as f:
        w = csv.DictWriter(f, cols, delimiter="\t", lineterminator="\n", restval="")
        w.writeheader()
        w.writerows(rows)


def main():
    with open("data/runs.jsonl") as f:
        rows = sorted((row(json.loads(l)) for l in f), key=lambda r: r["id"])
    os.makedirs("results", exist_ok=True)
    write("results/e1-ideas.tsv", rows)
    st = strata(rows)
    write("results/e1-strata.tsv", st)
    w = csv.DictWriter(sys.stdout, list(st[0]), delimiter="\t", lineterminator="\n")
    w.writeheader()
    w.writerows(st)


if __name__ == "__main__":
    main()
