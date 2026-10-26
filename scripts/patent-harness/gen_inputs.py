"""Writes testdata/embeddings/inputs.json, the input of the embedding golden test.

Invisible and look-alike characters (control characters, zero-width space,
combining marks, emoji) are built with chr(), so that the file shows what each
test string contains.
"""
import json
import sys

texts = [
    # Ideas.
    "interactive cli to kill whatever's on a port",
    "a fast async runtime for Rust",
    "react component for infinite scroll",
    "kubernetes log viewer",
    "vector database",
    "CLI tool that kills a process on a given port",
    "an ide plugin that highlights spelling mistakes in code comments",
    "a python pandas data pipeline",
    # Match texts, as rank_with builds them ("name: description").
    "kill-port: Kill the process running on a given port",
    "fkill-cli: Fabulously kill processes. Cross-platform.",
    "tokio: An event-driven, non-blocking I/O platform for writing asynchronous applications with the Rust programming language.",
    "recipes: A collection of baking recipes and kitchen tips",
    "jkfran/killport: A command-line tool to easily kill processes running on a specified port.",
    "react-infinite-scroll-component: An awesome Infinite Scroll component in react.",
    "stern: " + chr(0x2388) + " Multi pod and container log tailing for Kubernetes",
    "Show HN: portkill " + chr(0x2013) + " kill whatever is on a port",
    "qdrant/qdrant: Qdrant - High-performance, massive-scale Vector Database and Vector Search Engine for the next AI. Also available in the cloud https://cloud.qdrant.io/",
    "smol",
    "pkg.go.dev/github.com/spf13/cobra: A Commander for modern Go CLI interactions",
    "sk: Fuzzy Finder in rust!",
    # Degenerate input.
    "",
    " ",
    "a",
    "!!!",
    "tabs\tand\nnewlines\r\nmixed",
    chr(0) + chr(7) + "bell and nul" + chr(0x7f),
    "UPPERCASE SHOUTING TEXT",
    "snake_case_identifier and camelCaseIdentifier and kebab-case-name",
    "1234567890 3.14159 0xDEADBEEF",
    "node.js/express middleware @ v4.18.2 " + chr(0x2014) + " see https://example.com/docs?x=1&y=2",
    "C# .NET library for JSON (de)serialization",
    "fill[MASK] in, [CLS] or [cls] and [UNK]s",
    # Scripts, accents, symbols.
    chr(0x7AEF) + chr(0x53E3) + chr(0x6740) + chr(0x624B) + chr(0x547D) + chr(0x4EE4) + chr(0x884C) + chr(0x5DE5) + chr(0x5177),  # Chinese
    "用Rust写的CLI工具",
    "日本語の説明 with emoji " + chr(0x1F680),
    "포트를 죽이는 명령줄 도구",
    "инструмент для управления процессами на порту",
    "أداة سطر الأوامر",
    "पोर्ट पर प्रक्रिया",
    "ΟΔΟΣ εργαλείο",
    "Ein schnelles Werkzeug f" + chr(0xFC) + "r Prozesse: " + chr(0xE4) + chr(0xF6) + chr(0xFC) + chr(0xDF),
    "caf" + chr(0xE9) + " na" + chr(0xEF) + "ve r" + chr(0xE9) + "sum" + chr(0xE9) + " " + chr(0xC5) + "ngstr" + chr(0xF6) + "m",
    "e" + chr(0x301) + " vs " + chr(0xE9),  # decomposed and precomposed
    "zero" + chr(0x200B) + "width" + chr(0xA0) + "nbsp",
    chr(0x16A0) + chr(0x16C7) + chr(0x16BB) + " runes",
    chr(0x216B) + " " + chr(0xBD) + " " + chr(0x24D0),
    chr(0x1F680) + " blazing fast " + chr(0x1F525) + " tool " + chr(0x2728),
    chr(0x1F600) + chr(0x1F603) + chr(0x1F604) + chr(0x1F601),
    # Length: one word over max_input_chars_per_word (100), and text over
    # the 512-token truncation limit.
    "a" * 150 + " overlong",
    "the quick brown fox jumps over the lazy dog " * 80,
    "A terminal UI for exploring large JSON documents, with jq-style filters, "
    "fuzzy search across keys and values, syntax highlighting, mouse support, "
    "bookmarks, and the ability to copy any path or value to the clipboard. "
    "Works over SSH and inside tmux.",
]

# rank_with embeds a batch of match texts at once (padded to the longest),
# then scores each against the idea.
rankings = [
    {
        "idea": "interactive cli to kill whatever's on a port",
        "matches": [
            {"name": "kill-port", "description": "Kill the process running on a given port"},
            {"name": "fkill-cli", "description": "Fabulously kill processes. Cross-platform."},
            {"name": "jkfran/killport", "description": "A command-line tool to easily kill processes running on a specified port."},
            {"name": "Show HN: portkill " + chr(0x2013) + " kill whatever is on a port", "description": "Show HN: portkill " + chr(0x2013) + " kill whatever is on a port"},
            {"name": "recipes", "description": "A collection of baking recipes and kitchen tips"},
            {"name": "port-killer", "description": ""},
            {"name": "lsof", "description": "List open files"},
            {"name": "x/long", "description": "the quick brown fox jumps over the lazy dog " * 80},
        ],
    },
    {
        "idea": "a fast async runtime for Rust",
        "matches": [
            {"name": "tokio", "description": "An event-driven, non-blocking I/O platform for writing asynchronous applications with the Rust programming language."},
            {"name": "async-std", "description": "Async version of the Rust standard library"},
            {"name": "smol", "description": "A small and fast async runtime"},
            {"name": "glommio", "description": "Glommio is a thread-per-core crate that makes writing highly parallel asynchronous applications in a thread-per-core architecture easier for rustaceans."},
            {"name": "bread", "description": "Baking sourdough bread at home"},
            {"name": "端口", "description": "用Rust写的CLI工具"},
        ],
    },
    {
        "idea": "端口杀手命令行工具",
        "matches": [
            {"name": "kill-port", "description": "Kill the process running on a given port"},
            {"name": "端口", "description": "端口杀手"},
            {"name": "empty", "description": ""},
        ],
    },
]

json.dump({"texts": texts, "rankings": rankings}, sys.stdout, ensure_ascii=False, indent=2)
sys.stdout.write("\n")
