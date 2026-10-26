"""Writes testdata/patent/assess-cases.json, the input of the assess golden test.

Each case is a raw model response and the matches it is judged against. The
harness runs patent's verdict::assess on every case, through an Llm that
returns the response as it is, and records the verdict or the error.

The phrase lists are read from patent's own src/verdict.rs (the path is the
first argument), so that every phrase patent guards against has a case, and a
phrase silt copied wrongly shows up as a difference. Invisible characters are
built with chr(), so that the file shows what each response contains.
"""
import json
import re
import sys


def rust_list(source, name):
    """The string literals of the Rust slice constant `name`, comments dropped."""
    block = re.search(r"const %s: &\[&str\] = &\[(.*?)\];" % name, source, re.S).group(1)
    block = re.sub(r"//[^\n]*", "", block)
    return re.findall(r'"([^"]*)"', block)


source = open(sys.argv[1], encoding="utf-8").read()
absence = rust_list(source, "ABSENCE_PHRASES")
no_match = rust_list(source, "NO_MATCH_PHRASES")
hedges = rust_list(source, "HEDGE_WORDS")
assert len(absence) > 40 and len(no_match) > 10 and len(hedges) > 5


def match(name, similarity):
    return {
        "name": name,
        "source": "npm",
        "url": "https://example.com/" + name,
        "description": "",
        "popularity": None,
        "similarity": similarity,
        "last_updated": None,
    }


match_sets = {
    "none": [],
    # Floors to Crowded.
    "sample": [match("kill-port", 0.85), match("fkill-cli", 0.60)],
    # Stays Open, with one close match.
    "one-close": [match("patent", 0.57)],
    "very-strong": [match("twin", 0.72)],
    "saturated": [match("s%d" % i, s) for i, s in enumerate([0.9, 0.8, 0.7, 0.65, 0.6, 0.6])],
    "weak": [match("weak", 0.40)],
    # Top-10 names for the gap filter, and an 11th that is not one.
    "names": [
        match(n, 0.80 - i / 100)
        for i, n in enumerate([
            "at",
            "go",
            "C#",
            "ÜNÏCODE-TOOL",
            "node.js",
            "",
            "kill port",
            "İstanbul",  # Rust lowercases it to i + U+0307
            "ΣΙΣΥΦΟΣ",  # and this to σισυφος, with a final sigma
            "x",
            "eleventh",
        ])
    ],
}


def reply(level, headline, gaps=(), **extra):
    return json.dumps({"level": level, "headline": headline, "gaps": list(gaps), **extra}, ensure_ascii=False)


ok = reply("Crowded", "Several tools turned up in the sources checked.", ["a gap"])
cases = []


def case(name, matches, response):
    cases.append({"name": name, "matches": matches, "response": response})


# Extracting the JSON.
case("plain", "sample", ok)
case("fenced json", "sample", "```json\n" + ok + "\n```")
case("fenced bare", "sample", "```\n" + ok + "\n```")
case("fenced json no newline", "sample", "```json" + ok + "```")
case("fenced uppercase tag", "sample", "```JSON\n" + ok + "\n```")
case("fenced jsonl tag", "sample", "```jsonl\n" + ok + "\n```")
case("prose around fence", "sample", "Here you go:\n```json\n" + ok + "\n```\nHope this helps!")
case("unclosed fence", "sample", "```json\n" + ok)
case("two fences", "sample", "```json\n" + ok + "\n```\n```json\n" + reply("Open", "second") + "\n```")
case("think draft", "none",
     "<think>Let me draft: ```json\n" + reply("Saturated", "Draft from the trace.") +
     "\n```\nNo, answer Open.</think>\n" + reply("Open", "Nothing found in the sources checked."))
case("think closer only", "none", "Searching.</think>\n" + reply("Open", "Nothing found."))
case("think unterminated", "none", "<think>Let me consider the crates.io results")
case("think twice", "sample", "<think>a</think> " + reply("Open", "draft") + " </think>\n" + ok)
case("think then fence", "sample", "<think>" + reply("Saturated", "draft") + "</think>\n```json\n" + ok + "\n```")
case("unicode whitespace", "sample", chr(0xA0) + "\n" + ok + chr(0x3000) + chr(0x2029))
case("byte order mark", "sample", chr(0xFEFF) + ok)
case("trailing prose", "sample", ok + " Hope this helps!")
case("garbage", "none", "I don't know what JSON is")
case("empty", "none", "")
case("null", "none", "null")
case("array", "none", "[]")
case("string", "none", '"Open"')
case("empty object", "none", "{}")
case("nested verdict", "sample", json.dumps({"verdict": json.loads(ok)}))

# Levels and headlines.
for level in ["open", "OPEN", "Saturated ", "", "Öffen", 'Ope"n\\', "a\nb"]:
    case("level %r" % level, "sample", reply(level, "x"))
case("level number", "sample", '{"level": 2, "headline": "x"}')
case("level null", "sample", '{"level": null, "headline": "x"}')
case("level missing", "sample", '{"headline": "x"}')
case("headline missing", "sample", '{"level": "Crowded"}')
case("headline null", "sample", '{"level": "Crowded", "headline": null}')
case("headline number", "sample", '{"level": "Crowded", "headline": 5}')
case("headline empty", "sample", reply("Crowded", ""))
case("headline markup", "sample", reply("Crowded", "<b>kill-port</b> & fkill-cli \"cover\" this"))
case("duplicate keys", "sample", '{"level": "Open", "level": "Saturated", "headline": "a", "headline": "b"}')
case("extra fields", "sample", reply("Crowded", "x", confidence=0.9, notes=["n"]))
case("gaps missing", "sample", '{"level": "Crowded", "headline": "x"}')
case("gaps not a list", "sample", '{"level": "Crowded", "headline": "x", "gaps": "none"}')
case("gaps mixed", "sample", '{"level": "Crowded", "headline": "x", "gaps": [1, "kept", null, {"a": 1}, ["b"], ""]}')

# The floor.
case("floor open to crowded", "sample", reply("Open", "This is a brand-new idea about a testing framework."))
case("floor keeps a higher level", "sample", reply("Saturated", "A benign headline."))
case("floor crowded to saturated", "saturated", reply("Crowded", "A benign headline."))
case("floor single very strong", "very-strong", reply("Open", "A benign headline."))
case("model raises weak", "weak", reply("Crowded", "A benign headline."))
case("open nothing close", "none", reply("Open", "A benign headline."))
case("open weak", "weak", reply("Open", "A benign headline."))
case("open one close", "one-close", reply("Open", "A benign headline."))
case("not software", "none", reply("Open", "This does not appear to be a software tool idea — "
                                            "patent searches developer tool registries only."))

# Absence claims, in the headline and in the gaps.
for p in absence:
    case("absence headline %r" % p, "sample", reply("Crowded", "Crowded, but %s here." % p))
case("absence headline uppercase", "sample", reply("Crowded", "THIS DOES NOT EXIST."))
for h in hedges:
    case("hedged headline %r" % h, "sample", reply("Crowded", "There is no %s maintained tool." % h))
case("hedge stripped from phrase", "sample", reply("Crowded", "No developed tool covers this."))
case("hedge inside a word", "sample", reply("Crowded", "We distill no maintained ideas."))
case("honest unmaintained note", "sample",
     reply("Crowded", "Crowded — fkill-cli covers this, though it is no longer maintained."))
case("absence gaps", "sample", reply("Crowded", "x", ["gap %d: %s" % (i, p) for i, p in enumerate(absence)] +
                                     ["none of the matches offer an async API"]))
case("hedged gaps", "sample", reply("Crowded", "x", ["no %s maintained tool offers a TUI" % h for h in hedges] +
                                    ["a clean gap"]))

# "Nothing found" claims, with and without a close match.
for p in no_match:
    case("no-match one close %r" % p, "one-close", reply("Open", "%s in the sources checked." % p.capitalize()))
    case("no-match crowded %r" % p, "sample", reply("Crowded", "Crowded, though %s." % p))
case("no-match weak", "weak", reply("Crowded", "No close match turned up."))

# Gaps naming a top-10 match.
case("gaps naming matches", "names", reply("Saturated", "x", [
    "automatically handles port conflicts",
    "at startup it reads config",
    "atat is not a word",
    "golang bindings",
    "Go bindings",
    "c# bindings missing",
    "better c#-interop",
    "abc# something",
    "ünïcode-tool lacks docs",
    "node.js port",
    "nodejs port",
    "kill port helpers",
    "killport helpers",
    "istanbul coverage",
    "i" + chr(0x307) + "stanbul coverage",
    "σισυφος game",
    "σισυφοσ game",
    "x marks the spot",
    "eleventh is fine",
    "",
]))
case("gaps naming sample", "sample", reply("Crowded", "x", [
    "kill-port does not support IPv6",
    "KILL-PORT lacks Windows",
    "fkill-cli-like UI",
    "a fkill-clix fork",
    "none of the tools support Windows arm64",
]))

assert len({c["name"] for c in cases}) == len(cases), "case names must be unique"
json.dump(
    {
        "sources_checked": ["npm", "crates-io"],
        "sources_failed": ["pypi"],
        "match_sets": match_sets,
        "cases": cases,
    },
    sys.stdout,
    ensure_ascii=False,
    indent=1,
)
print()
