# /// script
# requires-python = ">=3.11"
# dependencies = [
#   "sentence-transformers==6.1.0",
#   "transformers==5.20.1",
#   "torch==2.14.1",
# ]
#
# [[tool.uv.index]]
# name = "pytorch-cpu"
# url = "https://download.pytorch.org/whl/cpu"
# explicit = true
#
# [tool.uv.sources]
# torch = { index = "pytorch-cpu" }
# ///
"""E3: scores every recorded candidate with one or more per-match scorers.

    uv run scripts/score.py SCORER...     # e.g. idf ms-marco
    uv run scripts/score.py all

Writes scores/<scorer>.tsv: idea, url, score, ms (the idea's time divided by
its candidates, so 50 × ms is the cost of an idea with 50 matches), and
appends a line to scores/runs.tsv: scorer, model, revision, threads,
candidates, seconds, date. Run from the experiment directory.

The candidates of an idea are the matches of both recorded runs (embeddings
and keyword-only), so that every labelled match has a score. A candidate's
text is what silt embeds: "name: description", or the name alone
(rank/ranker.go). Each idea is scored in one batch, as silt would.

Scorers:

    cosine          the recorded MiniLM cosine (silt today); no time
    keyword         the recorded --keyword-only score, 0 where it has none
    idf             share of the idea's IDF weight that the candidate's text
                    covers; IDF over all candidates of all ideas
    minilm          all-MiniLM-L6-v2 cosine, recomputed: checks the text
                    against the recorded cosine and times it in Python
    bge-small       BAAI/bge-small-en-v1.5, with its query instruction
    e5-small        intfloat/e5-small-v2, "query: " and "passage: "
    ms-marco        cross-encoder/ms-marco-MiniLM-L6-v2, the raw logit
    nli-minilm      cross-encoder/nli-MiniLM2-L6-H768, P(entailment) of
    nli-deberta     cross-encoder/nli-deberta-v3-small,  "This software is
                    {idea}." given the candidate's text
    qwen3-reranker  Qwen3-Reranker-0.6B on strix (llama-swap, /v1/rerank)
    qwen3-embed     Qwen3-Embedding-0.6B on strix (Ollama), with an
                    instruction on the query
"""

import collections
import csv
import json
import math
import os
import re
import sys
import time
import urllib.request

STRIX_SWAP = "http://strix:8080"
STRIX_OLLAMA = "http://strix:11434"


def text(m):
    """rank/ranker.go: the text silt embeds for a match."""
    d = m.get("description") or ""
    return m["name"] + ": " + d if d else m["name"]


def candidates():
    """[(id, idea, [(url, text, cosine, keyword)])] for every recorded idea."""
    out = []
    with open("data/runs.jsonl") as f:
        for r in map(json.loads, f):
            if "error" in r:
                continue
            kw = {m["url"]: m for m in r["keyword_only"]["matches"]}
            cs, seen = [], set()
            for m in r["embeddings"]["matches"]:
                k = kw.get(m["url"])
                cs.append((m["url"], text(m), m["similarity"], k["similarity"] if k else 0.0))
                seen.add(m["url"])
            for url, m in kw.items():
                if url not in seen:
                    cs.append((url, text(m), None, m["similarity"]))
            out.append((r["id"], r["idea"], cs))
    return out


# ── scorers: each takes (idea, texts) and returns a list of scores ─────────


def tokens(s):
    """Lower-case words, a few function words dropped, a plural "s" cut."""
    stop = {"a", "an", "the", "for", "of", "to", "in", "on", "and", "or", "with", "that",
            "which", "is", "from", "by", "as", "at", "into", "its", "it", "this", "be", "your"}
    out = []
    for w in re.findall(r"[a-z0-9]+", s.lower()):
        if w in stop:
            continue
        if len(w) > 3 and w.endswith("s") and not w.endswith("ss"):
            w = w[:-1]
        out.append(w)
    return out


def make_idf(cands):
    df = collections.Counter()
    n = 0
    for _, _, cs in cands:
        for _, t, _, _ in cs:
            df.update(set(tokens(t)))
            n += 1
    idf = lambda w: math.log((n + 1) / (df[w] + 1)) + 1

    def score(idea, texts):
        q = set(tokens(idea))
        total = sum(idf(w) for w in q) or 1.0
        return [sum(idf(w) for w in q & set(tokens(t))) / total for t in texts]
    return score, "idf over all candidates", ""


def hf_path(repo):
    """The local snapshot of a Hugging Face model (downloaded if missing)
    and its revision."""
    from huggingface_hub import snapshot_download
    path = snapshot_download(repo)
    return path, os.path.basename(path)


def make_bi(repo, qprefix="", dprefix=""):
    from sentence_transformers import SentenceTransformer
    path, rev = hf_path(repo)
    model = SentenceTransformer(path, device="cpu")

    def score(idea, texts):
        q = model.encode([qprefix + idea], normalize_embeddings=True)
        d = model.encode([dprefix + t for t in texts], batch_size=64, normalize_embeddings=True)
        return (d @ q[0]).tolist()
    return score, repo, rev


def make_cross(repo):
    import torch
    from sentence_transformers import CrossEncoder
    path, rev = hf_path(repo)
    model = CrossEncoder(path, device="cpu")

    def score(idea, texts):
        return model.predict([(idea, t) for t in texts], batch_size=64,
                             activation_fn=torch.nn.Identity()).tolist()
    return score, repo, rev


def make_nli(repo):
    import torch
    from sentence_transformers import CrossEncoder
    path, rev = hf_path(repo)
    model = CrossEncoder(path, device="cpu")
    labels = model.config.id2label
    ent = next(i for i, l in labels.items() if l.lower() == "entailment")

    def score(idea, texts):
        hyp = f"This software is {idea}."
        logits = model.predict([(t, hyp) for t in texts], batch_size=64,
                               activation_fn=torch.nn.Identity(), convert_to_tensor=True)
        return torch.softmax(logits, dim=-1)[:, int(ent)].tolist()
    return score, repo, rev


def post(url, body):
    req = urllib.request.Request(url, json.dumps(body).encode(), {"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=600) as resp:
        return json.load(resp)


def make_qwen3_reranker():
    model = "qwen3-reranker"

    def score(idea, texts):
        v = post(STRIX_SWAP + "/v1/rerank",
                 {"model": model, "query": idea, "documents": texts, "top_n": len(texts)})
        out = [0.0] * len(texts)
        for r in v["results"]:
            out[r["index"]] = r["relevance_score"]
        return out
    return score, f"{model} @ {STRIX_SWAP}", ""


QWEN3_EMBED_TASK = ("Given a description of a software tool, retrieve packages and "
                    "repositories that implement it")


def make_qwen3_embed():
    model = "qwen3-embedding:0.6b"

    def score(idea, texts):
        v = post(STRIX_OLLAMA + "/api/embed",
                 {"model": model, "input": [f"Instruct: {QWEN3_EMBED_TASK}\nQuery:{idea}"] + texts})
        es = v["embeddings"]
        norm = lambda e: math.sqrt(sum(x * x for x in e)) or 1.0
        q = es[0]
        nq = norm(q)
        return [sum(a * b for a, b in zip(q, e)) / (nq * norm(e)) for e in es[1:]]
    return score, f"{model} @ {STRIX_OLLAMA}", ""


SCORERS = {
    "idf": make_idf,
    "minilm": lambda c: make_bi("sentence-transformers/all-MiniLM-L6-v2"),
    "bge-small": lambda c: make_bi("BAAI/bge-small-en-v1.5",
                                   "Represent this sentence for searching relevant passages: "),
    "e5-small": lambda c: make_bi("intfloat/e5-small-v2", "query: ", "passage: "),
    "ms-marco": lambda c: make_cross("cross-encoder/ms-marco-MiniLM-L6-v2"),
    "nli-minilm": lambda c: make_nli("cross-encoder/nli-MiniLM2-L6-H768"),
    "nli-deberta": lambda c: make_nli("cross-encoder/nli-deberta-v3-small"),
    "qwen3-reranker": lambda c: make_qwen3_reranker(),
    "qwen3-embed": lambda c: make_qwen3_embed(),
}
RECORDED = ("cosine", "keyword")


def write(name, rows):
    os.makedirs("scores", exist_ok=True)
    with open(f"scores/{name}.tsv", "w", newline="") as f:
        w = csv.writer(f, delimiter="\t", lineterminator="\n")
        w.writerow(["idea", "url", "score", "ms"])
        w.writerows(rows)


def log_run(name, model, rev, n, seconds):
    path = "scores/runs.tsv"
    new = not os.path.exists(path)
    threads = ""
    if "torch" in sys.modules:
        threads = sys.modules["torch"].get_num_threads()
    with open(path, "a", newline="") as f:
        w = csv.writer(f, delimiter="\t", lineterminator="\n")
        if new:
            w.writerow(["scorer", "model", "revision", "threads", "candidates", "seconds", "at"])
        w.writerow([name, model, rev, threads, n, f"{seconds:.1f}",
                    time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())])


def run(name, cands):
    if name in RECORDED:
        col = 2 if name == "cosine" else 3
        write(name, [(id_, c[0], "" if c[col] is None else c[col], "")
                     for id_, _, cs in cands for c in cs])
        return
    score, model, rev = SCORERS[name](cands)
    rows, total, n = [], 0.0, 0
    for i, (id_, idea, cs) in enumerate(cands, 1):
        t0 = time.perf_counter()
        ss = score(idea, [c[1] for c in cs])
        dt = time.perf_counter() - t0
        total += dt
        n += len(cs)
        rows += [(id_, c[0], f"{s:.6g}", f"{1000 * dt / len(cs):.3f}") for c, s in zip(cs, ss)]
        print(f"\r{name}: {i}/{len(cands)}", end="", file=sys.stderr)
    print(f"\r{name}: {n} candidates in {total:.1f}s", file=sys.stderr)
    write(name, rows)
    log_run(name, model, rev, n, total)


def main():
    names = sys.argv[1:]
    if names == ["all"]:
        names = [*RECORDED, *SCORERS]
    if not names or any(n not in SCORERS and n not in RECORDED for n in names):
        sys.exit(__doc__)
    cands = candidates()
    for name in names:
        run(name, cands)


if __name__ == "__main__":
    main()
