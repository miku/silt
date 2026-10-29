# Reachability: plan

2026-10-29. How silt could reach more of the sources it selects, and ask
them less often. All seven steps are done: the result cache, PyPI through
CloakBrowser, Homebrew's catalogs kept for a day, rate limits, PyPI by name
without a browser, stale-if-error and `--check-sources`.

## Where coverage is lost today

- **PyPI, always.** Its search page answers every non-browser client with a
  Fastly "Client Challenge", a JavaScript page that sets a cookie and reloads.
  Today it came back as HTTP 200 with 3 KB and no results; the adapter reports
  it as `Unavailable`. In the 22 recorded runs (`differential.jsonl`), PyPI
  was selected 7 times and failed 7 times. It is selected for every Python,
  AI, API and CLI idea, and for the fallback.
- **Transient failures**: GitHub without a token (10 searches a minute,
  partial results), the 15 s per-source timeout, one of Hackage's 20 parallel
  `.cabal` fetches failing.
- **Load we cause**: the Homebrew adapter downloads both full catalogs on
  every run that selects it: `formula.json` is 32 MB, `cask.json` 19 MB
  (uncompressed; both say `max-age=600`).

## Measured: what gets through PyPI's challenge

Query `https://pypi.org/search/?q=kill+port`, once per client, on this
machine:

| client | result |
|---|---|
| curl with silt's User-Agent | 200, "Client Challenge", 0 results |
| `/usr/bin/chromium` and `google-chrome`, `--headless=new --virtual-time-budget=15000 --dump-dom` | "Client Challenge", 0 results |
| CloakBrowser 146 (free build), `--dump-dom` | "Client Challenge": dumped before the challenge ran |
| CloakBrowser 146, `--virtual-time-budget=15000 --dump-dom` | "Search results · PyPI", **20 results in 2.9 s** |

The command, with the stealth flags the Python wrapper adds
(`cloakbrowser/config.py`, `get_default_stealth_args`):

```shell
~/.cloakbrowser/chromium-146.0.7680.177.5/chrome --headless=new --no-sandbox \
  --fingerprint=42424 --fingerprint-platform=windows --user-data-dir=DIR \
  --virtual-time-budget=15000 --dump-dom 'https://pypi.org/search/?q=kill+port'
```

The output is the rendered HTML, so the existing goquery parser
(`sources/pypi.go`) reads it unchanged.

## CloakBrowser from Go

There is no Go API: the wrappers are Python, JavaScript and .NET, all driving
the binary through Playwright. Three ways to use it from Go:

1. **Run the binary with `--dump-dom`** (proposed). No new dependency and no
   CDP: `exec.CommandContext`, read stdout. Enough for search pages, which
   only need to be loaded and rendered.
2. **CDP from Go** (chromedp, or a small WebSocket client) on
   `--remote-debugging-port`. Only needed to click, scroll or wait for a
   selector, and no source needs that today.
3. **`cloakserve`**, the wrapper's CDP server (Docker). For a long-running
   harvester, not for a CLI.

Bundling the browser is ruled out: the install is 697 MB, and the binary's
license forbids redistribution. Listing it as an optional dependency in the
docs is allowed (`BINARY-LICENSE.md`, "Dependency listing"). Users install it
themselves (`uv tool install cloakbrowser`, `cloakbrowser install`), and silt
only looks for it.

## Proposal: an optional browser fetcher

- **Finding it**, first match wins:
  1. `SILT_BROWSER`, a path to the binary, or `off`;
  2. `CLOAKBROWSER_BINARY_PATH`, which the wrapper also honours;
  3. the newest `chromium-*` under `CLOAKBROWSER_CACHE_DIR` or
     `~/.cloakbrowser`. The executable is `chrome` on Linux,
     `Chromium.app/Contents/MacOS/Chromium` on macOS, `chrome.exe` on Windows.

  No browser, no change: everything works as it does today.
- **Only as a fallback for a wall.** An adapter first sends its plain HTTP
  request, with silt's User-Agent. Only when that comes back `Unavailable`
  does it fetch the same URL through the browser. Today that means only
  PyPI, later perhaps pkg.go.dev. Each search is still cached for an hour,
  so a repeated idea starts no browser at all.
- **One browser at a time.** The free build allows one concurrent session,
  so a process-wide semaphore of 1 is needed; a busy browser waits within
  the source's 15 s.
- **A persistent profile** under `<cache>/silt/browser`, with a fixed
  `--fingerprint` seed stored there (as the wrapper stores
  `.cloakbrowser-seed`). The challenge cookie survives between runs, and
  later fetches may skip the challenge.
- **Honest reporting.** If the browser gets results, the source counts as
  reached, and the progress line says how: "PyPI (through CloakBrowser)".
  If the browser also hits the wall, the source stays "not reached", as now.
- **Tests without the browser.** `internal/browser` takes the binary path
  as a parameter, so tests use a fake "chrome" script that prints canned
  HTML: it can check the flags, the timeout and the semaphore. A live test
  (`-tags live`) runs the real binary when it is installed.
- **Decide: on when found, or opt-in.** Through the browser, the registry no
  longer sees who is calling (it gets a browser's User-Agent, not silt's,
  against the deviations-table rule that registries should see the caller),
  and PyPI's wall is deliberate. The proposal is on when found, as asked,
  limited to sources whose plain request was walled, one request per search,
  cached, and named in the output. `SILT_BROWSER=off` (or a `--no-browser`
  flag) turns it off. The README should say this plainly.

## Other reachability work

- **PyPI without a browser**: the simple index (PEP 691 JSON, all project
  names), downloaded at most once a day with `If-None-Match`, matched
  locally against the keywords; then `pypi.org/pypi/<name>/json` for the
  summaries of the best ~20. Names only, so it finds less than PyPI's search,
  but it never meets the wall. Use it when no browser is found.
- **Keep Homebrew's catalogs for a day** (`<cache>/silt/http`), instead of
  51 MB per run. Conditional requests barely help, since the catalogs change
  every few minutes (`last-modified` was minutes old); a day-old catalog is
  fine for discovery.
- **Rate limits and Retry-After.**
  - Honour `Retry-After` and GitHub's `X-RateLimit-Reset`: retry only if the
    wait fits the source's deadline, otherwise report "rate-limited until
    HH:MM".
  - Cap Hackage's parallel `.cabal` fetches at ~4 instead of 20.
  - Add jitter to the 800 ms retry.
- **Stale if error** (RFC 5861's idea): when a source fails now, use its
  last result if it is younger than a day. Label it ("crates.io: from the
  cache, 5h old; not reached now") rather than counting it as a fresh
  search. This needs prune to keep entries for max(TTL, 24h).
- **`silt --check-sources`**: search each source once with a fixed idea and
  print reached / walled / rate-limited / timed out, with the latency.
  Useful to see whether a network or IP is walled before blaming the
  verdict.
- **GitHub token from `gh`**: if `GITHUB_TOKEN` is unset, ask
  `gh auth token`. Opt-in, since it uses a credential the user didn't hand
  to silt.

## Steps

| # | step | state |
|---|---|---|
| 1 | result cache per source and idea, 1 h, failures never cached (`sources/cache.go`, `--cache-ttl`, `SILT_CACHE_TTL`) | done |
| 2 | `internal/browser`: find the binary, `--dump-dom`, semaphore, profile, fake-binary tests | done |
| 3 | PyPI through the browser after a wall; progress line names it | done |
| 4 | Homebrew catalogs cached for a day (`--catalog-ttl`, `SILT_CATALOG_TTL`) | done |
| 5 | Retry-After / rate-limit handling, Hackage concurrency cap, retry jitter | done |
| 6 | PyPI simple-index fallback when no browser is found | done |
| 7 | stale-if-error, `--check-sources` | done |

### Step 1, as built

- A result is cached per source and query (idea + keywords). For GitHub, a
  hash of the token is part of the key, since a token widens the search to
  private repositories; the token itself is never stored.
- Entries are JSON files under `<UserCacheDir>/silt/results`: directory 0700,
  files 0600. Each is written to a temporary file and renamed, so concurrent
  runs never read half an entry.
- An entry expires after the TTL, judged by its file's modification time.
  Expired entries and leftover temporary files are pruned at the start of
  each search.
- Only successes are cached; a genuinely empty result counts as one. A
  failed source is searched again on the next run.
- The progress output adds a line naming the sources the cache answered,
  e.g. "from the local cache (less than 1h old): crates.io, Hacker News".
  The JSON output is unchanged.
- The CLI tests and `scripts/parity` run with `SILT_CACHE_TTL=0`, so the
  parity harness still asks both tools' registries at the same moment.
- Live check: the same `--keyword-only --fast` search twice, against
  crates.io and Hacker News, took 2.8 s, then 0.0 s with identical output.

### Steps 2 and 3, as built

- `internal/browser`: `Find` (the order above; a path set in `SILT_BROWSER`
  or `CLOAKBROWSER_BINARY_PATH` that is no file is an error, as an invalid
  `SILT_CACHE_TTL` is) and `Browser.DumpDOM`, which runs the binary with the
  wrappers' flags plus `--virtual-time-budget=15000 --dump-dom URL`.
- One browser at a time per process (a semaphore). Across processes Chromium
  guards the profile itself: a second browser on the same profile exits at
  once with code 21 ("Failed to create a ProcessSingleton … Aborting now to
  avoid profile corruption"), and `DumpDOM` retries every 250 ms within the
  deadline. Measured: two at once, one got 20 results, the other exited 21.
- The profile is `<UserCacheDir>/silt/browser` (0700), with the seed in
  `.cloakbrowser-seed`, the wrappers' file and format.
- Killed at the source's deadline (`exec.CommandContext`, `WaitDelay` 1 s).
  Measured: a SIGKILLed browser leaves no child processes behind.
- PyPI tries the browser only after an `UnavailableError` from the plain
  request (the 403 or the challenge page), never after a 429 or 5xx. Through
  the browser, only a page with package snippets or PyPI's "There were no
  results for" callout counts; anything else (the challenge again, the blank
  DOM a failed load dumps, a captive portal) leaves PyPI unavailable, so a
  failed load is never cached as an empty result.
- Found while testing: a real empty result is a 296 KB page, which the old
  "over 2000 bytes and no snippets means the wall" rule misread. The callout
  now marks it as empty on both paths.
- `Outcome.Browsed` lists the sources reached through the browser; the
  progress line marks them: "crates.io, PyPI (through CloakBrowser)". The
  JSON output is unchanged.
- Tests run the test binary as the fake browser (`TestMain`), so they need
  no shell and run on Windows too: flags, seed, the semaphore (it fails if
  two fake browsers overlap; checked by widening the semaphore to 4), a hang
  killed at the deadline, a failure's last stderr line, profile in use.
  `TestLivePyPIThroughTheBrowser` (`-tags live`) runs the real binary with a
  fresh profile: 20 matches in 3.3 s.
- Live check of the CLI, `--keyword-only --fast --sources pypi,crates-io`:
  5.5 s with PyPI through CloakBrowser (20 PyPI matches); the rerun 0.0 s
  from the cache; with `SILT_BROWSER=off`, PyPI not reached as before.

### Step 4, as built

- `sources.CatalogCache` (Dir, TTL), separate from the result `Cache`: a
  catalog answers any query, and lives in `<UserCacheDir>/silt/catalogs`,
  not `<cache>/silt/http` as planned above and not next to `results`, whose
  prune would delete it after an hour.
- What is kept is not the HTTP responses but the fields the search reads:
  name, desc and homepage of every formula, then every cask, in the order
  downloaded, with a format version and the base URL. A file of another
  version or host, an unreadable one, or one older than the TTL (by
  modification time) is a miss and is replaced by the next download. Live:
  16,500 packages, 1.9 MB, against 32.2 + 19.3 MB downloaded.
- Below it, the process-wide in-memory catalog stays as it was, so a TUI
  session reads the disk at most once.
- Only a successful download of both catalogs is kept; written to a
  temporary file and renamed (`writeAtomic`, now shared with the result
  cache), directory 0700.
- Default 24 h; `--catalog-ttl` > `SILT_CATALOG_TTL` > default, parsed as
  `--cache-ttl` is (`resolveTTL`, generalised from `resolveCacheTTL`); `0`
  downloads on every run. The parity harness and the CLI tests set
  `SILT_CATALOG_TTL=0`, so silt searches the same catalog as patent.
- Not labelled in the progress output: a day-old catalog is still
  Homebrew's data, and the results cache line already covers a repeated
  idea.
- Live check, `--keyword-only --fast --cache-ttl 0 --sources homebrew`:
  1.7 s downloading; another idea 0.02 s from disk; the first idea again
  0.02 s with the same matches as the download; `--catalog-ttl 0` 1.7 s.

### Step 5, as built

- `rateLimitError` (in `sources`, unexported) wraps the error an adapter
  reported before, so its kind and wording, and `IsRetryable`, are as they
  were; it adds the reset and a hint. `checkStatus` makes every 429 one,
  with the reset from `Retry-After` (seconds or an HTTP date). GitHub makes
  its 403 and 429 one, with `Retry-After` (secondary limits) or else
  `X-RateLimit-Reset` when `X-RateLimit-Remaining` is 0, and the hint "set
  GITHUB_TOKEN for a higher limit" when it has no token.
  `X-RateLimit-Reset` is read for GitHub only: elsewhere it is sometimes
  seconds from now rather than an epoch.
- `fanout.retryWait` decides the one retry. A known reset: wait for it if
  that leaves `minRetryTime` (2 s) before the deadline (the earlier of the
  15 s and the caller's), else no retry. A rate limit without a reset, and
  any other retryable failure: retry after 800 ms give or take half
  (400–1200 ms). Pure function of (err, now, deadline), tested without
  sleeping.
- `Outcome.RateLimited` ([]RateLimit: source, reset, hint, in Failed
  order); the pipeline logs one line per source, the reset in local time
  rounded up to the minute so it is never before the reset: "GitHub:
  rate-limited until 21:39; set GITHUB_TOKEN for a higher limit". JSON
  unchanged; the TUI's "Not reached" list unchanged.
- Hackage: `maxCabalFetches` = 4 (a semaphore around the 20 fetches).
  Live, three ideas: 0.9–1.1 s at 20 at once, 1.7–2.1 s at 4, the same
  descriptions.
- Tests can fail: dropping the reset branch fails `TestRetryWait`; raising
  the cap to 20 fails the Hackage test (which checks 4, not the constant —
  its first version compared against the constant and passed the mutation).
- Live, GitHub without a token, limit used up: 0.3 s, "GitHub:
  rate-limited until 21:39; set GITHUB_TOKEN …" (reset 21:38:53); before,
  the 403 was retried after 800 ms. With the token, 49 matches.
- Testing note: GitHub counted curl's and Go's requests from this machine
  separately (curl at 0 remaining, Go's client at 9 in the same minute), so
  the limit had to be used up with Go's client to test silt.

### Step 6, as built

- Checked first, with silt's User-Agent, no browser: `/simple/` as PEP 691
  JSON is 200 (10 MB gzip, 45 MB, 926,592 projects, 1.7 s);
  `/pypi/<name>/json` is 200 (requests 43 KB gzip; boto3 713 KB gzip, 3.3 MB,
  for its list of releases), 404 for a name that isn't a project, and takes
  display names as listed (`Simple-TOML-Configurator`, `zope.interface`)
  without a redirect. There is no `/pypi/<name>/latest/json` (404), and the
  per-version endpoint (3.4 KB for boto3) needs the version first.
- Used only after the plain request is walled and no browser got through:
  none, or one that failed. `PyPI.Index`, `Searcher.PyPIIndex`; the zero
  Searcher leaves it off (patent has none); the CLI turns it on unless
  `SILT_PYPI_INDEX=off`. The parity harness and the CLI tests set it off.
- The index is a catalog: `<cache>/silt/catalogs/pypi-names.json` (15 MB,
  names only), `--catalog-ttl`, and in memory for the process (the generic
  `memCatalog`, now shared with Homebrew). No `If-None-Match`: the ETag had
  changed after 40 minutes (800 serials later), so after a day it never
  matches.
- Matching (`matchPyPINames`), chosen by prototyping on the real index with
  8 ideas: names split at `-_.`; a keyword matches a part equal to it (a
  final "s" ignored on both) or, from 4 letters, a part it starts
  ("port_killer"); each matched keyword counts its IDF over all names (not
  below 0), each unmatched part counts -1; ties to the shorter name, then
  in order. Generic words (python, library, package, tool, util, …) are
  dropped first. Rejected on the prototype: exact parts only (missed
  port_killer, file-renamer, markdown-pdf-converter); at least two keywords
  (dropped one-word names); no generic-word list (python-library topped
  the fuzzy idea; "whatever", in 9 names, ranked as the rarest keyword).
- Description: the 20 best names, 4 at a time, from `/pypi/<name>/json`:
  `info.summary`, and the latest `upload_time_iso_8601` of `urls` as
  LastUpdated (a date the search path doesn't have). A 404 (no such
  project, or one without releases) drops the name, so some ideas get
  17–18 matches; another failure keeps it without a description. URL
  `/project/<name>/`, as the search path.
- `detour` (noDetour, throughBrowser, byName) replaces the browsed bool;
  `Outcome.ByName`; the progress line says "PyPI (names only)".
- Live, no browser, fresh cache, `--keyword-only --fast --sources pypi`:
  first idea 3.1 s (index downloaded), then 1.1–1.6 s. kill-port →
  port_killer ("It kills process by specify port number."),
  kill-this-port, kill-process-tree; toml → toml-config, toml-file,
  Simple-TOML-Configurator; markdown → markdown-pdf-converter,
  markdown-pdf, markdown_convert. Weak for "fuzzy string matching"
  (matchingsplit, matchingtools): "matching" is rarer among names than
  "fuzzy". The ranker orders the 20 by description afterwards.
- Peak RSS (`--keyword-only`, PyPI only): 22 MB without the index, 103 MB
  reading it from disk, 171 MB downloading it (decoding 45 MB of JSON). A
  text format on disk would roughly halve the disk case; not done.

### Step 7, as built

- Stale-if-error: `Cache.StaleIfError` (the CLI sets
  `DefaultStaleIfError`, 24 h; no flag). Prune keeps entries for
  max(TTL, StaleIfError). After a live search fails (retry included), an
  entry younger than StaleIfError stands in, unless the search was
  cancelled. The source then counts as reached (its matches are in, and
  the verdict's sources_checked lists it, as for a fresh cache hit), and
  `Outcome.Stale` lists it with the age; the progress line says "not
  reached now, from the local cache instead: crates.io (3h old)". A
  rate-limited source that stands in still gets its rate-limit line, so
  `Outcome.RateLimited` is now in adapter order, failed or stale. The JSON
  output is unchanged. `--cache-ttl 0` writes no entries, so nothing
  stands in, as in the parity harness.
- `--check-sources`: `Searcher.Check` searches each source once for
  `CheckQuery` ("json", which every source has packages for), all at once,
  with the normal retry, browser and PyPI index, but with no result cache,
  no catalogs on disk, and fresh in-memory catalogs (a first version
  reused the process's, which the tests caught as a leak between tests).
  Each source is reached / through CloakBrowser / by names only / walled /
  rate-limited (with `RateLimit.String`, moved from the pipeline) / timed
  out / failed, with the time. Timeouts are told from other failures by
  `timeoutError`, which wraps patent's "X timed out" error. Printed to
  stdout; exit 0 whatever the result, since 1 and 2 are verdict levels.
  `--sources` and `--exclude` choose the sources; nil means all 19.
- Tests can fail: dropping the cancellation guard, or pruning at TTL
  rather than max(TTL, StaleIfError), each fails its test.
- Live, `--check-sources`: 19 of 19 reached in 3.4 s (PyPI through
  CloakBrowser, 3.4 s; Hackage 1.7 s; GitHub 2.4 s); with
  `SILT_BROWSER=off`, PyPI by names only (2.8 s). No result or catalog
  files written.
- Live, stale: one search with the cache on; entries backdated 3 h; the
  same search with `HTTPS_PROXY` pointing at a closed port, so every
  request fails: "40 matches from 2 sources … not reached now, from the
  local cache instead: crates.io (3h old), npm (3h old)". Backdated 25 h:
  not used, and pruned. With the network back: fresh results, new entries.
