# Reachability: plan

2026-10-29. How silt could reach more of the sources it selects, and ask
them less often. Steps 1–3 (the result cache, and PyPI through CloakBrowser)
are done; the rest is a plan.

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
| 4 | Homebrew catalogs cached for a day | |
| 5 | Retry-After / rate-limit handling, Hackage concurrency cap | |
| 6 | PyPI simple-index fallback when no browser is found | |
| 7 | stale-if-error, `--check-sources` | |

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
