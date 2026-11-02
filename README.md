# silt

A prior-art search for code ideas: has this already been shipped? silt is a Go
port of [patent](https://github.com/r14dd/patent).

Searches all 19 of patent's sources (crates.io, npm, PyPI, GitHub, Go, Maven,
NuGet, RubyGems, Docker Hub, Homebrew, Packagist, Hex, Artifact Hub, AUR,
Hackage, Nixpkgs, the VS Code and JetBrains marketplaces, Hacker News) and two
of its own (the official Arch Linux repositories, and Codeberg on request),
ranks the matches by semantic similarity to the idea
([all-MiniLM-L6-v2](https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2)
in pure Go, downloaded once, about 90 MB) or by keyword overlap, and estimates
from those similarities how crowded the space is.

```shell
$ go install github.com/miku/silt/cmd/silt@latest
$ silt                                                              # search interactively
$ silt "cli tool that kills a process on a given port"              # results in the TUI
$ silt --json "cli tool that kills a process on a given port"       # JSON
$ silt --keyword-only --json "react component for infinite scroll"  # no model at all
$ silt --sources npm,crates-io "embedded vector database for mobile apps"
$ silt --list-sources
```

## Which sources are searched

GitHub and Hacker News are searched for every idea. The other sources are
picked by words in the idea: "rust" picks crates.io, "cli" picks crates.io,
Go, npm, PyPI, Homebrew and Arch Linux, and so on. When the idea picks none,
npm, PyPI and crates.io stand in. Codeberg, whose search takes 5–15 s, is
searched only when asked for, with `--sources` or `--add`. `--list-sources`
says which is which:

```shell
$ silt --list-sources
crates-io              crates.io     if the idea suggests it, or suggests none
github                 GitHub        always
...
codeberg               Codeberg      only with --sources or --add
arch-linux             Arch Linux    if the idea suggests it
```

## The verdict

silt embeds the idea and each match ("name: description") with
all-MiniLM-L6-v2, and scores the match by the cosine similarity of the two:
near 0 for unrelated text, 1 for the same meaning. The verdict counts the
matches that come close:

| Level | When |
|---|---|
| 🔴 Saturated | 5 or more matches with a similarity of at least 0.60 |
| 🟡 Crowded | 2 or more with at least 0.55, or 1 with at least 0.70 |
| 🟢 Open | anything less |

The headline words the level as the estimate it is, and counts the
closely-related matches, those at 0.55 or more: "We believe this space is
crowded: 3 closely-related tools turned up in the sources checked." The TUI
shows their scores in yellow, or in green from 0.70. A `--limit` below 50
only shortens the list: the verdict still counts among the best 50.

The thresholds are patent's. With `--keyword-only`, a match's score is the
share of the idea's keywords it contains, and the same thresholds apply, so
the level is a rougher estimate there.

The JSON output has the verdict, with a fixed caveat that the TUI leaves out,
and the matches, best first:

```json
{
  "schema_version": 2,
  "query": "cli tool that kills a process on a given port",
  "verdict": {
    "level": "saturated",
    "headline": "We believe this space is saturated: 7 closely-related tools turned up in the sources checked.",
    "sources_checked": ["crates-io", "npm"],
    "sources_failed": [],
    "caveat": "Not proof it doesn't exist — only that nothing close turned up in the sources checked. Keep looking (web, app stores, niche communities) before committing."
  },
  "matches": [
    {
      "name": "kill-port",
      "source": "npm",
      "url": "https://www.npmjs.com/package/kill-port",
      "description": "Kill process running on given port",
      "popularity": 1000000,
      "similarity": 0.7999609,
      "last_updated": "2022-06-21T23:47:36Z"
    }
  ]
}
```

`popularity` (stars, downloads, points, depending on the source) and
`last_updated` are `null` where the source doesn't report them. Neither
changes the score: an old project is still prior art.

## Flags

| Flag | |
|---|---|
| `IDEA` | the idea to search for; without it, silt asks in the TUI |
| `--limit N` | how many matches to keep after ranking (default 50) |
| `--keyword-only` | rank by keyword overlap instead of with the embedding model |
| `--sources LIST` | search only these sources, comma-separated, instead of picking them from the idea |
| `--add LIST` | also search these sources, on top of the ones picked from the idea; Codeberg is searched only when asked for, here or with `--sources`, since its search takes 5–15 s |
| `--exclude LIST` | leave these sources out |
| `--cache-ttl DURATION` | how long a source's results are reused for the same idea (default `1h`; `0` turns the cache off) |
| `--catalog-ttl DURATION` | how long downloaded catalogs (Homebrew's, PyPI's project names) are kept (default `24h`; `0` downloads them on every run) |
| `--json` | print JSON instead of opening the TUI |
| `--list-sources` | list the source names, and when each is searched, and exit |
| `--check-sources` | search each source once, without the caches, print whether it was reached and how long it took, and exit |
| `--completions SHELL` | print completions for bash, fish, powershell or zsh |
| `-V`, `--version` | print the version |
| `-h`, `--help` | print the help |

## Environment and caches

`GITHUB_TOKEN`, if set, lifts GitHub's rate limit, and `NO_COLOR` turns off
colour in the TUI.

When a source refuses a search because of its rate limit, silt tries again
once the limit lifts, if that is within the 15 s each source gets. Otherwise
the progress output says so, e.g. "GitHub: rate-limited until 21:39; set
GITHUB_TOKEN for a higher limit". Without a token, GitHub allows 10
searches a minute.

Each source's results are reused for an hour from silt's cache directory
(`~/.cache/silt` on Linux), so running the same idea again doesn't send the
same requests to the registries again. A source that failed is never cached,
and the progress lines say which sources the cache answered. `--cache-ttl` or
`SILT_CACHE_TTL` sets another duration, such as `10m` or `24h`; `0` turns the
cache off.

Results are kept for a day, though, so that when a source fails, its result
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
Scores of 0.55 or more, the close matches the verdict counts, are yellow,
and green from 0.70. `⚑` marks a match with no update in 2+ years, and only
where the source reports a date, so its absence is not a claim of freshness.
The verdict is
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
