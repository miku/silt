# Changelog

## 0.1.0 (unreleased)

The first release: a Go port of [patent](https://github.com/r14dd/patent)
0.14.1 (`8d741bd`). Given the same idea, silt selects the same sources, sends
each registry the same queries, ranks with the same model and thresholds,
and gives the same verdict, JSON and exit code as patent; the side-by-side
check is `go run ./scripts/parity`.

- Searches the 19 sources patent searches, picked from the idea or set with
  `--sources` and `--exclude`.
- Ranks matches by semantic similarity with all-MiniLM-L6-v2, run in pure Go
  (within 1e-6 of patent's ONNX Runtime), or by keyword overlap with
  `--keyword-only`.
- Gets the verdict from a local Ollama or an OpenAI-compatible API, with
  patent's integrity guards, or from the similarity data alone with `--fast`.
  `--print-prompt` prints the prompt instead.
- Shows the results in a TUI on a terminal, and as JSON otherwise.
- Static binaries for Linux, macOS and Windows, on amd64 and arm64.

Where silt differs from patent:

- It is called `silt`: `SILT_MODEL`, `SILT_API_BASE` and `SILT_API_KEY`,
  `<config>/silt/config.toml`, `<cache>/silt/`, and its own User-Agent.
- It reads `OLLAMA_HOST` as the `ollama` CLI does, so the verdict can come
  from an Ollama on another machine; patent always uses `localhost:11434`.
- The TUI is silt's own, with patent's layout, keys and integrity rules.
- Duplicate matches are resolved in source order; in patent, which duplicate
  wins changes from run to run.
- Each source's results are cached for an hour, so the same idea run again
  doesn't send the same requests again (`--cache-ttl`, `SILT_CACHE_TTL`; `0`
  turns it off). Failed searches are never cached. patent sends every
  request on every run.
- Homebrew's catalogs (51 MB) are downloaded once a day rather than on
  every run that searches Homebrew; silt keeps the 2 MB it searches
  (`--catalog-ttl`, `SILT_CATALOG_TTL`; `0` downloads them every run).
- With CloakBrowser installed, PyPI is reached: when its search page walls
  off silt's request, silt loads the page in the browser, which passes the
  challenge. `SILT_BROWSER` names the browser, or turns it off with `off`.
  patent reports PyPI as not reached.
- `--completions` supports bash, fish, powershell and zsh, but not elvish.
- Help and argument errors are worded by cobra rather than clap, with the
  same flags and exit codes.
- A date that an offset moves out of the years 0000–9999 is dropped; patent
  panics on it.
