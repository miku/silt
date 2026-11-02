# silt

A prior-art search for code ideas: has this developer tool already been
shipped? silt is an Go port of [patent](https://github.com/r14dd/patent).

> silt can prove that something **exists**, but never that it **doesn't** — it
> only searched some sources. Every verdict is scoped to "found in the sources
> checked", and sources that were selected but failed are reported as such.

**Status:** work in progress; see [notes/todo.md](notes/todo.md). Today silt
searches all 19 of patent's sources (crates.io, npm, PyPI, GitHub, Go, Maven,
NuGet, RubyGems, Docker Hub, Homebrew, Packagist, Hex, Artifact Hub, AUR,
Hackage, Nixpkgs, the VS Code and JetBrains marketplaces, Hacker News) and two
of its own (the official Arch Linux repositories, and Codeberg on request),
ranks
matches by semantic similarity (all-MiniLM-L6-v2 in pure Go, downloaded once,
about 90 MB) or by keyword overlap, and estimates the verdict from the
similarity data: how many matches come close to the idea. Since that is an
estimate, the headline says so: "We believe this space is crowded: 3
closely-related tools turned up in the sources checked." On a terminal, the
results open in a TUI.

With `--llm-verdict`, an LLM gives the verdict instead, with a headline and
gaps in its own words: a local Ollama (`qwen3.5` by default) or any
OpenAI-compatible API. Its level never drops below what the similarity data
shows, and a headline or gap that claims something doesn't exist is
replaced or dropped. It is slower (about 5 s more per idea with `qwen3.5` on
a GPU), and in [our experiments](notes/2026-10-31-verdict-experiments/README.md)
it picked the level no better than the similarity data.

```shell
$ go install github.com/miku/silt/cmd/silt@latest
$ silt                                                                   # search interactively
$ silt "cli tool that kills a process on a given port"                   # results in the TUI
$ silt --json "cli tool that kills a process on a given port"            # JSON
$ silt --llm-verdict --json "cli tool that kills a process on a given port"   # verdict via local Ollama
$ silt --llm-verdict --json --api-base http://localhost:1234/v1 --model qwen3.5-122b "embedded vector database for mobile apps"
$ silt --keyword-only --json "react component for infinite scroll"       # no model at all
$ silt --print-prompt "kubernetes log viewer with live filtering"   # the LLM verdict's prompt, for your own agent
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
| `--llm-verdict` | ask an LLM for the verdict instead of estimating it from the similarity data |
| `--model NAME` | the LLM for `--llm-verdict`: `qwen3.5` on Ollama by default, required with `--api-base` |
| `--api-base URL` | an OpenAI-compatible API instead of Ollama for `--llm-verdict`, e.g. `http://localhost:1234/v1` |
| `--api-key KEY` | the key for `--api-base`; omit it for servers without auth |
| `--sources LIST` | search only these sources, comma-separated, instead of picking them from the idea |
| `--add LIST` | also search these sources, on top of the ones picked from the idea; Codeberg is searched only when asked for, here or with `--sources`, since its search takes 5–15 s |
| `--exclude LIST` | leave these sources out |
| `--cache-ttl DURATION` | how long a source's results are reused for the same idea (default `1h`; `0` turns the cache off) |
| `--catalog-ttl DURATION` | how long downloaded catalogs (Homebrew's, PyPI's project names) are kept (default `24h`; `0` downloads them on every run) |
| `--json` | print JSON instead of opening the TUI |
| `--print-prompt` | print the verdict prompt, for your own agent, and exit |
| `--list-sources` | list the source names and exit |
| `--check-sources` | search each source once, without the caches, print whether it was reached and how long it took, and exit |
| `--completions SHELL` | print completions for bash, fish, powershell or zsh |
| `-V`, `--version` | print the version |
| `-h`, `--help` | print the help |

## Configuration

`--model`, `--api-base` and `--api-key` can also be set with `SILT_MODEL`,
`SILT_API_BASE` and `SILT_API_KEY`, or in `config.toml` in silt's config
directory: `~/.config/silt` on Linux, `~/Library/Application Support/silt`
on macOS, `%AppData%\silt` on Windows.

```toml
model = "qwen3.5-122b"
api_base = "http://localhost:1234/v1"
api_key = "..."
```

A flag beats the environment, which beats the file. `OPENAI_API_KEY` is not
read. These settings only apply with `--llm-verdict`.
Without `--api-base`, the LLM verdict comes from the Ollama daemon that
`OLLAMA_HOST` names, as for the `ollama` CLI, or from `localhost:11434` if
it is unset. `GITHUB_TOKEN`, if set, lifts GitHub's
rate limit, and `NO_COLOR` turns off colour in the TUI.

When a source refuses a search because of its rate limit, silt tries again
once the limit lifts, if that is within the 15 s each source gets. Otherwise
the progress output says so, e.g. "GitHub: rate-limited until 21:39; set
GITHUB_TOKEN for a higher limit". Without a token, GitHub allows 10
searches a minute.

Each source's results are kept for an hour in silt's cache directory
(`~/.cache/silt/results` on Linux, `~/Library/Caches/silt/results` on macOS,
`%LocalAppData%\silt\results` on Windows), so running the same idea again
doesn't send the same requests to the registries again. A source that failed
is never cached, and the progress lines say which sources the cache
answered. `--cache-ttl` or `SILT_CACHE_TTL` sets another duration, such as
`10m` or `24h`; `0` turns the cache off.

Results are kept for a day, though, and when a source fails, its result
for the same idea from within the last day stands in, labelled: "not
reached now, from the local cache instead: crates.io (3h old)".

`silt --check-sources` searches each source once for "json", without the
caches, and prints whether it was reached (directly, through CloakBrowser,
or by names only), walled, rate-limited, timed out or failed, and how long
it took: useful to see whether a network is walled before trusting a
verdict. It downloads Homebrew's catalogs, and PyPI's names if PyPI needs
them.

Homebrew has no search API, so silt downloads its whole catalog of formulae
and casks (51 MB) and searches it locally. It keeps the parts it searches
(under 2 MB) for a day in `catalogs` next to `results`, so the next ideas
are searched without downloading it again; the same goes for PyPI's list of
project names (15 MB), when silt needs it (see below). A failed download is
never kept. `--catalog-ttl` or `SILT_CATALOG_TTL` sets another duration,
such as `6h` or `168h`; `0` downloads the catalogs on every run.

### PyPI and CloakBrowser

PyPI has no search API, and its search page answers every client that isn't
a browser with a JavaScript challenge, so silt can't search PyPI on its own:
it reports it as not reached. With [CloakBrowser](https://github.com/CloakHQ/CloakBrowser)
installed, silt loads the search page in it, which passes the challenge, and
reaches PyPI. silt doesn't ship the browser (about 700 MB, and its license
forbids redistribution). To install it:

```shell
uv tool install cloakbrowser
cloakbrowser install
```

silt uses the newest version under `~/.cloakbrowser` (or
`CLOAKBROWSER_CACHE_DIR`), or the executable that `CLOAKBROWSER_BINARY_PATH`
or `SILT_BROWSER` names. It keeps a browser profile in its cache directory
(`~/.cache/silt/browser` on Linux). Without CloakBrowser nothing changes.

The browser is only used when PyPI walls off silt's own request, once per
search, and the result is cached like any other. The progress line then says
"PyPI (through CloakBrowser)". Through the browser PyPI no longer sees silt's
User-Agent, but a browser's; PyPI's wall is deliberate. `SILT_BROWSER=off`
turns the browser off.

Without the browser, silt matches the idea's keywords against the names of
all PyPI projects, from the index pip reads (downloaded at most once a day,
like Homebrew's catalog), and fetches the descriptions of the 20 best from
PyPI's JSON API. That finds less than PyPI's search, which also reads
descriptions and knows what is popular, so the progress line says "PyPI
(names only)". `SILT_PYPI_INDEX=off` turns it off, and PyPI is then not
reached.

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
reports a date, so its absence is not a claim of freshness. The verdict is
always shown in full: on a terminal too small for it, silt asks for a
larger one instead of showing part of it.

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
