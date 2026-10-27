# silt

A prior-art search for code ideas: has this developer tool already been
shipped? silt is an Go port of [patent](https://github.com/r14dd/patent).

> silt can prove that something **exists**, but never that it **doesn't** — it
> only searched some sources. Every verdict is scoped to "found in the sources
> checked", and sources that were selected but failed are reported as such.

**Status:** work in progress; see [notes/todo.md](notes/todo.md). Today silt
searches all 19 of patent's sources (crates.io, npm, PyPI, GitHub, Go, Maven,
NuGet, RubyGems, Docker Hub, Homebrew, Packagist, Hex, Artifact Hub, AUR,
Hackage, Nixpkgs, the VS Code and JetBrains marketplaces, Hacker News), ranks
matches by semantic similarity (all-MiniLM-L6-v2 in pure Go, downloaded once,
about 90 MB) or by keyword overlap, and asks an LLM for the verdict: a local
Ollama (`qwen3.5` by default) or any OpenAI-compatible API. The verdict's
level never drops below what the similarity data shows, and a headline or
gap that claims something doesn't exist is replaced or dropped. If the model
fails, or with `--fast`, the verdict comes from the similarity data alone.
On a terminal, the results open in a TUI.

```shell
$ go install github.com/miku/silt/cmd/silt@latest
$ silt                                                                   # search interactively
$ silt "cli tool that kills a process on a given port"                   # results in the TUI
$ silt --json "cli tool that kills a process on a given port"            # verdict via local Ollama
$ silt --json --api-base https://api.openai.com/v1 --model gpt-4o-mini "embedded vector database for mobile apps"
$ silt --fast --json "cli tool that kills a process on a given port"     # no LLM
$ silt --keyword-only --fast --json "react component for infinite scroll"   # no model at all
$ silt --print-prompt "kubernetes log viewer with live filtering"   # the verdict prompt, for your own agent
$ silt --list-sources
```

The exit code is the verdict, so scripts can gate on it: 0 Open, 1 Crowded,
2 Saturated. Errors exit 1, argument errors 2. Output that isn't a terminal
gets JSON.

Binaries for Linux, macOS and Windows are on the
[releases page](https://github.com/miku/silt/releases).

## Flags

| Flag | |
|---|---|
| `IDEA` | the idea to search for; without it, silt asks in the TUI |
| `--limit N` | how many matches to keep after ranking (default 50) |
| `--keyword-only` | rank by keyword overlap instead of with the embedding model |
| `--fast` | skip the LLM: the verdict comes from the similarity data alone |
| `--model NAME` | the LLM for the verdict: `qwen3.5` on Ollama by default, required with `--api-base` |
| `--api-base URL` | an OpenAI-compatible API instead of Ollama, e.g. `https://api.openai.com/v1` |
| `--api-key KEY` | the key for `--api-base`; omit it for servers without auth |
| `--sources LIST` | search only these sources, comma-separated, instead of picking them from the idea |
| `--exclude LIST` | leave these sources out |
| `--json` | print JSON instead of opening the TUI |
| `--print-prompt` | print the verdict prompt, for your own agent, and exit |
| `--list-sources` | list the source names and exit |
| `--completions SHELL` | print completions for bash, fish, powershell or zsh |
| `-V`, `--version` | print the version |
| `-h`, `--help` | print the help |

## Configuration

`--model`, `--api-base` and `--api-key` can also be set with `SILT_MODEL`,
`SILT_API_BASE` and `SILT_API_KEY`, or in `config.toml` in silt's config
directory: `~/.config/silt` on Linux, `~/Library/Application Support/silt`
on macOS, `%AppData%\silt` on Windows.

```toml
model = "gpt-4o-mini"
api_base = "https://api.openai.com/v1"
api_key = "sk-..."
```

A flag beats the environment, which beats the file. The key falls back to
`OPENAI_API_KEY` last. Without `--api-base`, the verdict comes from the
Ollama daemon that `OLLAMA_HOST` names, as for the `ollama` CLI, or from
`localhost:11434` if it is unset. `GITHUB_TOKEN`, if set, lifts GitHub's
rate limit, and `NO_COLOR` turns off colour in the TUI.

## TUI

| Key | Action |
|---|---|
| `↑`/`k`, `↓`/`j` | previous / next match |
| `g`/`Home`, `G`/`End`, `PgUp`, `PgDn` | first / last match, page up / down |
| `Enter` | details: description, popularity, last update, URL |
| `o` | open the URL in the browser |
| `y` | copy the URL |
| `/`, `Esc` | filter by name or description, clear the filter |
| `s` | sort by similarity, popularity or name |
| `n` | new search |
| `?` | help |
| `q`, `Ctrl+C` | quit |

The mouse wheel scrolls, a click selects a match, a second click opens it.
`⚑` marks a match with no update in 2+ years, and only where the source
reports a date, so its absence is not a claim of freshness. The verdict's
caveat is always shown in full: on a terminal too small for it, silt asks
for a larger one instead of showing a verdict without it.

## Development

```shell
$ go run ./scripts/fetchmodel   # once: the model for the embedding tests
$ make test                     # hermetic, no network
$ make test-live                # smoke tests against the real registries
$ make lint
$ go run ./scripts/parity       # side by side with patent 0.14.1, live
```

## License

MIT. silt is a derivative of patent, © Riad Mukhtarov, used under the MIT
license; see [NOTICE](NOTICE).
