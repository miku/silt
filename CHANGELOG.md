# Changelog

## 0.1.0 (unreleased)

The first release: a Go port of [patent](https://github.com/r14dd/patent)
0.14.1 (`8d741bd`). Given the same idea, silt selects the same sources, sends
each registry the same queries, ranks with the same model and thresholds,
and gives the same verdict level and exit code as patent's `--fast`, which
estimates the verdict from the similarities as silt does; the side-by-side
check is `go run ./scripts/parity`.

- Searches the 19 sources patent searches, picked from the idea or set with
  `--sources` and `--exclude`.
- Ranks matches by semantic similarity with all-MiniLM-L6-v2, run in pure Go
  (within 1e-6 of patent's ONNX Runtime), or by keyword overlap with
  `--keyword-only`.
- Estimates the verdict from the similarities: Saturated with 5 or more
  matches at 0.60 or more, Crowded with 2 or more at 0.55 or 1 at 0.70,
  Open otherwise.
- Shows the results in a TUI on a terminal, and as JSON otherwise.
- Static binaries for Linux, macOS and Windows, on amd64 and arm64.

Where silt differs from patent:

- It is called `silt`: `SILT_*`, `<cache>/silt/`, and its own User-Agent.
- It has no LLM verdict: the verdict always comes from the similarities, as
  with patent's `--fast`. In our experiments an LLM picked the level no
  better, at about 5 s more per idea. So there is no `--fast`,
  `--model`, `--api-base`, `--api-key` or `--print-prompt`, no config file,
  and no gaps, which only the LLM wrote: the JSON is at `schema_version` 2,
  patent's 1 without `verdict.gaps`. Since the level is an estimate, the
  headline says so: "We believe this space is crowded: 3 closely-related
  tools turned up in the sources checked." where patent says "Crowded — 3
  closely-related tools turned up in the sources checked."
- Two more sources: the official Arch Linux repositories (`arch-linux`),
  searched next to the AUR and for CLI tools, and Codeberg, searched only
  when asked for, since its search takes 5–15 s. `--add` searches any
  source on top of the ones picked from the idea: `--add codeberg`. Both
  narrow a query down to a single keyword when it matches nothing, since
  Codeberg matches a phrase and Arch Linux every word.
- The TUI is silt's own, with patent's layout, keys and integrity rules,
  but without the verdict's caveat; the JSON keeps it. Scores are coloured
  by the verdict's thresholds: yellow from 0.55, the close matches the
  headline counts, and green from 0.70.
- Duplicate matches are resolved in source order; in patent, which duplicate
  wins changes from run to run.
- Each source's results are cached for an hour, so the same idea run again
  doesn't send the same requests again (`--cache-ttl`, `SILT_CACHE_TTL`; `0`
  turns it off). Failed searches are never cached. patent sends every
  request on every run.
- When a source fails, its result for the same idea from the last day
  stands in, labelled as such (`--cache-ttl 0` turns this off with the
  cache). `--check-sources` searches each source once and prints whether
  it was reached, and how long it took.
- Homebrew's catalogs (51 MB) are downloaded once a day rather than on
  every run that searches Homebrew; silt keeps the 2 MB it searches
  (`--catalog-ttl`, `SILT_CATALOG_TTL`; `0` downloads them every run).
- A rate-limited search is tried again only when the source's reset
  (`Retry-After`, GitHub's `X-RateLimit-Reset`) comes within its 15 s;
  otherwise silt says "rate-limited until HH:MM". Other retries wait
  800 ms give or take half. Hackage's `.cabal` files are fetched 4 at a
  time, not 20. patent retries every failure once after 800 ms.
- With CloakBrowser installed, PyPI is reached: when its search page walls
  off silt's request, silt loads the page in the browser, which passes the
  challenge. `SILT_BROWSER` names the browser, or turns it off with `off`.
  Without it, silt matches the keywords against the names of all PyPI
  projects and describes the 20 best from PyPI's JSON API ("PyPI (names
  only)"; `SILT_PYPI_INDEX=off` turns it off). patent reports PyPI as not
  reached.
- `--list-sources` also says when each source is searched: always, if the
  idea suggests it, or only with `--sources` or `--add`.
- `--completions` supports bash, fish, powershell and zsh, but not elvish.
- Help and argument errors are worded by cobra rather than clap, with the
  same flags and exit codes.
- A date that an offset moves out of the years 0000–9999 is dropped; patent
  panics on it.
- The first-run notice and `--keyword-only`'s help say the model is about
  90 MB, as it is; patent says 80.
