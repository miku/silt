"""E2: asks an LLM for silt's verdict on every recorded idea.

    python3 -I scripts/llm.py ollama http://strix:11434 qwen3.5:9b
    python3 -I scripts/llm.py openai http://strix:8080 qwen3.5-122b
    python3 -I scripts/llm.py ollama http://strix:11434 qwen3.5:9b 005 039

Reads data/prompts.jsonl (`make prompts`) and appends one line per idea to
data/llm-<model>.jsonl: the raw answer, the level in it, seconds, and the
server's token counts and timings where it reports them. Ideas already in
that file are skipped, so a run that stops can be started again. Optional
ids after the model limit the run to those ideas. Run from the experiment
directory.

The request is the one silt sends:

- ollama: POST /api/generate, think false, temperature 0, num_predict 512
  (llm/ollama).
- openai: POST /v1/chat/completions, one user message, temperature 0,
  max_tokens 2048 (llm/openai). silt sends nothing about thinking there;
  here chat_template_kwargs turns it off, as silt's think false does for
  Ollama, since a model that thinks runs out of tokens (todo.md, M9).

silt's client gives up after 120 s; this one waits 900 s, so slow answers
are measured rather than lost. "over_120s" marks those silt would not get.
"""

import json
import os
import re
import sys
import time
import urllib.error
import urllib.request

TIMEOUT = 900
SILT_TIMEOUT = 120
LEVELS = {"Open": "open", "Crowded": "crowded", "Saturated": "saturated"}


def extract_json(raw):
    """verdict.extractJSON: drop a <think> trace, then take the first fenced
    block if there is one."""
    s = raw.strip()
    if (end := s.rfind("</think>")) >= 0:
        s = s[end + len("</think>"):].strip()
    if (start := s.find("```")) >= 0:
        content = s[start + 3:]
        content = content[4:] if content.startswith("json") else content
        content = content.lstrip()
        if (end := content.find("```")) >= 0:
            return content[:end].strip()
    return s


def parse_level(raw):
    """The model's level, before silt floors it, or "" with the reason."""
    try:
        obj = json.loads(extract_json(raw))
    except json.JSONDecodeError as e:
        return "", f"parse: {e}"
    level = obj.get("level") if isinstance(obj, dict) else None
    if level not in LEVELS:
        return "", f"invalid level: {level!r}"
    if not isinstance(obj.get("headline"), str):
        return "", "missing 'headline'"
    return LEVELS[level], ""


def post(url, body):
    req = urllib.request.Request(url, json.dumps(body).encode(),
                                 {"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
        return json.load(resp)


def ask_ollama(base, model, prompt):
    v = post(base.rstrip("/") + "/api/generate", {
        "model": model, "prompt": prompt, "stream": False, "think": False,
        "options": {"temperature": 0, "num_predict": 512},
    })
    ns = lambda k: round(v.get(k, 0) / 1e9, 3)
    return v.get("response", ""), {
        "prompt_tokens": v.get("prompt_eval_count"), "output_tokens": v.get("eval_count"),
        "load_s": ns("load_duration"), "prefill_s": ns("prompt_eval_duration"),
        "decode_s": ns("eval_duration"),
    }


def ask_openai(base, model, prompt):
    v = post(base.rstrip("/") + "/v1/chat/completions", {
        "model": model, "messages": [{"role": "user", "content": prompt}],
        "temperature": 0, "max_tokens": 2048,
        "chat_template_kwargs": {"enable_thinking": False},
    })
    msg = v["choices"][0]["message"]
    u, t = v.get("usage", {}), v.get("timings", {})
    return msg.get("content") or "", {
        "prompt_tokens": u.get("prompt_tokens"), "output_tokens": u.get("completion_tokens"),
        "prefill_s": round(t["prompt_ms"] / 1e3, 3) if "prompt_ms" in t else None,
        "decode_s": round(t["predicted_ms"] / 1e3, 3) if "predicted_ms" in t else None,
        "finish": v["choices"][0].get("finish_reason"),
        "reasoning_chars": len(msg.get("reasoning_content") or ""),
    }


def main():
    if len(sys.argv) < 4 or sys.argv[1] not in ("ollama", "openai"):
        sys.exit(__doc__)
    api, base, model, only = sys.argv[1], sys.argv[2], sys.argv[3], set(sys.argv[4:])
    ask = ask_ollama if api == "ollama" else ask_openai
    out = f"data/llm-{re.sub(r'[^A-Za-z0-9.-]+', '-', model)}.jsonl"
    done = set()
    if os.path.exists(out):
        with open(out) as f:
            done = {json.loads(l)["id"] for l in f}
    with open("data/prompts.jsonl") as f:
        prompts = [json.loads(l) for l in f]
    todo = [p for p in prompts if p["id"] not in done and (not only or p["id"] in only)]
    print(f"{out}: {len(done)} done, {len(todo)} to go", file=sys.stderr)
    for i, p in enumerate(todo, 1):
        t0 = time.monotonic()
        row = {"id": p["id"], "api": api, "url": base, "model": model,
               "at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
        try:
            raw, stats = ask(base, model, p["prompt"])
            level, err = parse_level(raw)
            row |= {"level": level, "error": err, **stats, "raw": raw}
        except (urllib.error.URLError, TimeoutError, OSError, KeyError) as e:
            row |= {"level": "", "error": f"request: {e}", "raw": ""}
        row["seconds"] = round(time.monotonic() - t0, 2)
        row["over_120s"] = row["seconds"] > SILT_TIMEOUT
        with open(out, "a") as f:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")
        print(f"[{i}/{len(todo)}] {p['id']} {row['level'] or row['error']} {row['seconds']}s",
              file=sys.stderr)


if __name__ == "__main__":
    main()
