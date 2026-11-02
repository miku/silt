# silt — simplifications

2026-11-02. A read-through looking for code to delete, collapse or reshape.
Nothing here is a bug: `go build ./...` and `go test ./...` are both green,
and the parity rules in [todo.md](todo.md) stay in force. Each item says what
to do and what it buys, so that the ones worth their risk can be picked out
of the ones that are not.

Ordered by value per effort. §1 is free, §2 is mechanical, §3 needs a decision,
§4 is outside the Go code, §5 is left as found.

Line numbers are as of `8c5003c`.

Update, 2026-11-02: the LLM verdict is gone (D8 in
[decisions.md](decisions.md)), so the items on `llm/`, `verdict/assess.go`
(`stripHedges`, `isWholeWord`, `parseVerdict`), `verdict.sourceList`, the
backend precedence chain in `cmd/silt` and `ollamaEndpoint` are moot.

Update, 2026-11-03: §1 is done (`HTTPClientError` dropped as well; see D6),
and the packages moved under `internal/` (D1), so a path such as
`sources/sources.go` below is now `internal/sources/sources.go`. In §4 the
`dbis` media are gone from the tree and the history, and the verdict
experiments from the tree, though still in the history.

## 1. Dead code — delete outright

Found by listing every exported name in the non-test files and counting its
references everywhere else. That check is about 20 lines of Python and is
worth committing as `scripts/deadapi` next to `scripts/paritytests`.

| Item | Where | Evidence |
|---|---|---|
| `sources.SearchAll` | `sources/sources.go:414` | no call sites, not even in tests |
| `sources.SearchFilteredWithClient` | `sources/sources.go:434` | no call sites; its doc points at `internal/httprec`, which was never wired in |
| `rank.Rank` | `rank/ranker.go:134` | only its own two tests; the pipeline uses `NewRanker` + `RankWith` |
| `minilm.Dim` | `internal/minilm/model.go:34` | unused; everything reads the unexported `hidden` |
| `silt.HTTPClientError` | `errors.go:20` | never constructed — its own doc says "silt never returns this today"; only read by `IsRetryable` |
| `internal/httprec`, whole package | `internal/httprec/` (186 + 184 test lines) | imported by nothing. [todo.md](todo.md) M9 still lists the replay hook as to-do, so the package is a promise, not a used thing |
| `rank.Cosine` | `rank/ranker.go:17` | used only inside `rank` → unexport |
| `sources.SearchSources` | `sources/sources.go:524` | used only by `sources` tests → unexport |

About 450 lines, most of it in the one `httprec` move. If the replay hook is
still wanted, keep `httprec` and wire it under the pipeline instead of leaving
it unreferenced — an unused fixture package rots silently.

`HTTPClientError` is the one judgement call: it exists so that `IsRetryable`
carries patent's full error taxonomy. Dropping it is a listed deviation, so
either drop it and add the row, or leave it and drop the other five.

## 2. Duplication to collapse

**Two identical HTTP clients.** `sources.NewHTTPClient`
(`sources/http.go:27`) and `llm.NewHTTPClient` (`llm/llm.go:25`) differ only
in `Timeout` and in whether the User-Agent transport wraps the base. One
`newClient(timeout time.Duration, rt http.RoundTripper)`.

**Three "join the source names" helpers.** `pipeline.sourceNames`
(`internal/pipeline/pipeline.go:101`), `tui.sourceNames`
(`internal/tui/view.go:144`) and `verdict.sourceList`
(`verdict/verdict.go:27`). `model.Names([]Source) string` and
`model.NameSlice([]Source) []string` cover all three.

**Adapter boilerplate ×19.** Every adapter repeats the `Client`/`BaseURL`
fields, a `NewX(client)` and an `ID()`. An embedded unexported
`adapterBase{Client, BaseURL}` plus a `sources.New(id, client)` table —
replacing the 40-line `Searcher.newAdapter` switch at `sources/sources.go` —
removes ~80 lines and leaves one place to forget a source instead of two.
The exported `Client`/`BaseURL` fields stay, so the mock-server tests and
patent's `with_base_url` equivalent are unaffected.

**The narrowing loop ×8.** `artifacthub, cratesio, hackernews, homebrew,
maven, nuget, packagist, rubygems` all spell

```go
for _, term := range narrowingCandidates(q, N) {
        …fetch…
        matches = matches[:0]
        …map…
        if len(matches) > 0 { break }
}
```

`narrowAndSearch(ctx, q, minTerms, func(term string) ([]model.Match, error))`
puts the `matches[:0]` reset — subtle and easy to get wrong, since a stale
slice from the previous candidate is a silent wrong-answer — in one place.

**Two whole-word scanners.** `ideaContains` + `isASCIIAlphanumeric`
(`sources/sources.go:134,141`) and `isWholeWord` (`verdict/assess.go:175`) are
the same scan with different boundary rules (ASCII vs Unicode). One
parameterized helper in `internal/textutil`, with the two rule sets as
callers.

**Two precedence chains in the CLI.** `flagOrEnv` + `or` + `resolveBackend`
(`cmd/silt/main.go:157–222`) and `resolveTTL` (`cmd/silt/main.go:224`) both
implement flag > env > config > default. One generic
`resolve[T](flag, env, cfg, def)` with a parse function for the TTL case.

**The `raceEnabled` build-tag pair** is duplicated verbatim in `rank/` and
`internal/minilm/` (four files) → one `internal/raceflag`.

**`memCatalog` + `sync.Once` + a shared package var** is written twice, once
in `Homebrew` (`sources/homebrew.go:28,33`) and once in `PyPI`
(`sources/pypi.go:56`, `sources/pypiindex.go:37`), along with the identical
"fresh on disk from the same host, else fetch and store" shape of
`loadCatalog` and `loadIndex`. A `catalogSource[T]` with the version, the
disk name and a fetch func carries both.

**`Cache` and `CatalogCache`** (`sources/cache.go`) both do `enabled()`, a
stat-and-TTL check, a version-stamp check and `writeAtomic`. A small
`diskStore{name, version, ttl}` would carry both, and would make the
`StaleIfError` asymmetry between them explicit instead of incidental.

**`stripHedges` already lowercases** (`verdict/assess.go:108`), but
`containsAbsencePhrase` (`verdict/assess.go:118`) passes it text it has
already lowered and keeps a second copy of the lowered text around. One pass.

**`strings.ToLower` vs `textutil.ToLower` are mixed.** `model`,
`sources/pypiindex.go`, `freshness` and `internal/tui/list.go` use the
standard one; `verdict`, `rank`, `sources`, `llm/openai` and
`internal/pipeline` use `textutil`. The distinction is real for the integrity
guards (U+0130, final sigma) and meaningless for a TUI filter, so either
standardize on `textutil` or say in `internal/textutil`'s doc which side of
the line each caller is on. Right now it reads as drift.

**`internal/tui`'s own text primitives.** `padRight`, `padLeft`, `cell`
(`internal/tui/view.go:526–536`) and `fit` (`view.go:162`) reimplement what
`lipgloss.Style.Width(...)` and `.MaxHeight(...)` do. `pack` (`view.go:122`)
also mixes `len([]rune(sep))` with `ansi.StringWidth(item)` for the same
comparison, which happens to agree only because the separator is
single-cell.

## 3. Structural changes, each needing a decision

**`sources.Outcome` carries six parallel slices** — `Matches, Reached,
Failed, Cached, Browsed, ByName, Stale, RateLimit`
(`sources/sources.go:353`) — documented as staying "in the same order", and
the consumer rejoins them with `slices.Contains(out.Browsed, s)`
(`internal/pipeline/pipeline.go:121`). The only consumer is
`pipeline.logSearch`. A `[]SourceResult{Source, Matches, Status, Age,
RateLimit}` deletes the ordering invariant, the `switch r.detour` in the
fan-out and the three `Contains` scans, and `sources/check.go`
(`newSourceCheck`, which re-derives the same classification) reads from the
same shape. This is the single biggest simplification in the tree.

**`cmd/silt/main.go:258` `flags.run` is ~160 lines.** Split into
`resolveSources`, `resolveCaches`, `emitJSON` and `openTUI`; the flag
resolution and the output choice are two different jobs that currently
interleave.

**`run()`'s exit-code closure** (`cmd/silt/main.go:90`) has `RunE` write to an
`exitCode` variable captured from `run`. A typed `exitError{code int,
err error}` returned from `RunE` removes the shared mutable state and makes
the "usage error exits 2, runtime error exits 1, verdict exits its level"
rule one switch instead of three places.

**`internal/pipeline`'s test seams are package-level vars** —
`searchFiltered`, `newRanker`, `ollamaEndpoint`
(`internal/pipeline/pipeline.go:74–88`). They are mutable globals, so the
package cannot use `t.Parallel()`, and a test that forgets to restore one
leaks into the next. They are already nil-able settings, which is exactly
`Config`'s stated convention; make them fields.

**`sources/sources.go` is 730 lines** and opens with a 60-line package doc
about which sources publish a `LastUpdated`. Split into `doc.go`,
`select.go` (the rule table and `detectSources`), `narrow.go`, `fanout.go`
and `dedup.go`. The file currently mixes the selection table, the fan-out,
the cache glue, `Outcome`, `RateLimit.String` and `Dedup`, and the useful
part of the package doc is buried under the date-coverage table.

**`verdict.parseVerdict`** (`verdict/assess.go:227`) applies four sequential,
order-dependent overrides to `headline` (lines 272–294: floor raised,
absence phrase, claims-no-match with a close match present, Open with nothing
close). Each is right, the order is load-bearing, and nothing says so.
Extract `resolveHeadline(raw, modelLevel, level, matches) string` so the
precedence is one function with one return and one doc comment.

**`defaultFanout` is a package global that gets copied and then mutated**
(`f.cache = s.Cache`, `sources/sources.go:473`). Build the `fanout` value per
search; the global then holds only the three timing constants.

**`model.All()` allocates on every call** (`model/model.go:97`) and is looped
twice by `ParseSource` and once per value by `UnmarshalText`. Precompute it
once; `ParseSource` can use a `map[string]Source` built in `init` instead of
two linear scans plus rebuilding the valid-names list on every error. The
"caller may modify the returned slice" contract is not used anywhere.

**`sources/check.go:newSourceCheck`** builds
`map[detour]CheckStatus{…}` on every call → a package-level var or a switch.
Same for `tui.sortKey.String()` (`internal/tui/list.go:24`), which builds an
array literal per call.

**`pipeline.ShortDuration`** (`internal/pipeline/pipeline.go:90`) is a display
helper used by `cmd/silt/flags.go:40`, which therefore imports the pipeline
for one function. Move it to `internal/textutil`.

**Two different `errNoIdea`s** with different wording
(`cmd/silt/main.go:65`, `internal/pipeline/query.go:18`). Both are reachable.
Either share one or rename them so they don't read as a mistake.

## 4. Outside the Go code

**`notes/dbis.gif` (16 MB) and `notes/dbis.webm` (23 MB) are tracked**, and
are about 39 MB of the 43 MB `.git`. Move them to LFS or drop them; every
clone pays that forever. `notes/2026-10-31-verdict-experiments/` (4.4 MB, its
own `Makefile` and Python) is a second project inside this one — a sibling
repo, or at least its own `.gitignore` beyond the one it already has.

**`make test` runs the whole suite twice**, plain and then `-race`
(`Makefile:19`), about 26 s instead of 13 s. The `raceEnabled` split already
skips the heavy numeric tests under `-race`, so the plain pass adds little
coverage; run `-race` only, or keep the plain pass for `sources` and `cmd`
alone.

**No CI config is tracked at all** — no `.github/`, no Woodpecker or Drone
file — though [todo.md](todo.md) M0 records "CI green" as done. Whatever runs
`make lint && make test` locally should be committed.

**`.golangci.yml` enables `standard` plus six linters.** Adding `unused`,
`unparam`, `dupl`, `goconst`, `funlen`/`gocyclo` and `errgroup` would have
caught §1 and most of §2 on its own. Related: the `golangci-lint` and
`staticcheck` binaries on `PATH` are too old for Go 1.27 and fail with
"export data version 5 is greater than maximum supported", so a local run
that bypasses the Makefile reports nothing at all rather than an error worth
noticing. Only the Makefile's `go run …@v2.15.0` pin works.

**`make clean`** does not remove `scripts/patent-harness/Cargo.lock`, which
`.gitignore` lists.

## 5. Left as found

- `flags.json` shadows the imported `encoding/json` in `cmd/silt/main.go`;
  `jsonOut` would not.
- `main.go`'s `downloadProgress` and `scripts/fetchmodel` both hand-roll a
  percentage printer.
- `sources.go`'s `narrowed` (`sources/sources.go:86`) sorts an index slice to
  imitate Rust's stable sort; `slices.SortStableFunc` over the keywords
  themselves gives the same order for less code.
- `internal/tui/system.go:openURL` starts the opener with
  `context.Background()` and reaps it in a goroutine. Documented, but the
  detached-process idiom would say the same thing in code.
- `verdict.plural(n)` (`verdict/verdict.go:150`) is fine; noted only because
  `fmt` has no built-in for it and it will be written again if it is not here.
