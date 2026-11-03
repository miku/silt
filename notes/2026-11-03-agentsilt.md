# Agent-driven prior-art sweep with silt (2026-11-03)

Task: use `silt --json` as an agent to find *rare* dev-tool ideas that would be
useful to many people, and record the process. Everything lives in
[2026-11-03-agentsilt/](2026-11-03-agentsilt/):

* `run.sh IDEAS OUT` runs `silt --json --limit 20` once per line (2 s apart, to
  stay under GitHub's search limit) and skips results that already exist, so a
  run can be resumed
* `summarize.py OUT` sorts a round least-crowded first, with the top 3 matches
* `roundN.txt` holds the queries (with comments on why), `rN/` the raw JSON and
  stderr, `rN-summary.txt` the sorted summary

Verdict rule (internal/verdict): saturated = 5+ matches >= 0.60; crowded =
2+ >= 0.55 or 1 >= 0.70; otherwise open.

## Process

| round | queries | open / crowded / saturated | what it did |
|---|---|---|---|
| 1 | 39 | 18 / 18 / 3 | broad sweep of everyday pain points; 2 controls (kill port, unused deps) came out saturated/crowded as expected |
| 2 | 25 | 17 / 7 / 1 | reworded round-1 survivors (does "open" survive rewording?) plus 12 fresh ideas. Interrupted at #14; resumed with the same `run.sh` |
| 3 | 28 | 19 / 7 / 2 | **jargon re-checks** of opens I knew to be wrong, plus 15 fresh ideas |
| 4 | 28 | 20 / 7 / 1 | two wordings (plain + jargon) for each of the 14 best survivors |
| — | — | — | web search for the survivors: silt only sees registries, GitHub and HN |
| 5 | 22 | 14 / 8 / 0 | 11 ideas *generated from the pattern of the survivors*, each with a plain and a jargon wording up front |

142 silt runs, 88 of them "open". After rewording and the web check, about
seven ideas are left. An open verdict is therefore cheap: it means "nothing
close in the registries under this wording", which is much weaker than "rare".

The working definition of rare that came out of this:

1. open under at least two wordings, one plain and one in the jargon the
   tool's authors would use, and
2. on the web, only *project-specific* or research implementations turn up,
   no reusable tool.

Two patterns produced most of the survivors, so round 5 used them to generate
ideas:

* **P1: every project rebuilds it by hand in CI.** Many projects have the same
  ad hoc harness and nobody has packaged it. This is itself a strong gap
  signal.
* **P2: join two data sources nobody joins**, e.g. issue tracker x lockfile,
  dependents x their code, changelog x my call sites.

## Shortlist

### 1. Generic release-upgrade / file-compatibility harness (P1)

Install release N-1 (and N-2, ...), let it write data/config/state with a
user-provided script, upgrade to HEAD, check it can read everything; optionally
the downgrade direction too. Fixtures per release are cached.

* silt: open under 5 wordings, max 0.39 to 0.56 (r2#22, r4#17, r4#18, r5#3,
  r5#4; the 0.56 was noise like `automated-testing`, which has no description)
* web: only project-specific harnesses, which is pattern P1:
  [Grenade](https://wiki.openstack.org/wiki/Grenade) (OpenStack),
  [Knative upgrade tests](https://pkg.go.dev/knative.dev/serving@v0.6.1/test/upgrade),
  Erlang `ct_release_test`,
  [WiredTiger](https://www.github.com/wiredtiger/wiredtiger/wiki/Testing-forward-backward-compatibility),
  [Vortex RFC 0023](https://vortex-data.github.io/rfcs/rfc/0023.html),
  [Paimon](https://github.com/apache/paimon/pull/9504), Neon, SecureDrop
* who benefits: anything that persists state, such as CLIs with config and
  caches, desktop apps, databases and file-format libraries

### 2. Release-aware "this workaround can go now" tracker (P2)

Find comments that link to upstream issues, resolve each issue to its closing
PR or commit, find the first release tag that contains it, compare that with
the version in the lockfile, and report "fixed in 4.2.0, you are on 4.3.1:
remove the workaround".

* silt: open under 4 wordings, max 0.40 to 0.57 (r3#14, r3#15, r4#1, r4#2)
* web: [krank](https://github.com/guibou/krank) reports open/closed status of
  linked issues, but not releases; eslint-plugin-unicorn's
  [expiring-todo-comments](https://github.com/sindresorhus/eslint-plugin-unicorn/blob/master/docs/rules/expiring-todo-comments.md)
  needs hand-written version conditions; todocheck only covers the project's own
  tracker. The issue -> release -> lockfile step is missing.
* who benefits: almost every codebase that depends on third-party code

### 3. Reverse-dependency symbol search (P2)

`who-calls mypkg.old_function` lists the dependents (deps.dev or ecosyste.ms)
that call it, fetches their source, does an AST search, and ranks by downloads.
Use it before deprecating or removing a function.

* silt: open under 4 of 5 wordings (r1#10, r2#1, r2#3, r4#16); r4#15 was
  "crowded" only by PyPI name-search tools
* web: [py-code.org](https://py-code.org/) has all PyPI code as git
  (~120 GB, ripgrep over everything in ~5 min); RustPräzi is a research call
  graph of crates.io; crater, revdepcheck and dont-break run dependents' tests
  rather than searching for a symbol; warn-dependents emails dependents. No
  ready-made CLI.
* who benefits: library maintainers. They are few, but the leverage is large.

### 4. Changelog filtered by my call sites (P2)

For a dependency bump, show only the release-note entries between the locked
version and the target version that mention symbols my code imports or calls.

* silt: open, 0.49 and 0.56 (r5#7, r5#8). Adjacent in silt's results:
  `willitbreak` (r2#2, 0.53) and `upgrade-impact-analyzer` (r4#16, 0.54)
  compare the *API* of two versions against usage, not the changelog
* web: only manual guidance, and Endor Labs reachability at the dependency
  level
* rare only in part: the API-diff side exists, the release-notes side does not

### 5. Expiry discovery from a repository

Scan a repo and its infrastructure config for things that expire, such as
PEM/keystore certificates, JWT `exp` claims, GPG subkeys, Apple provisioning
profiles, domains in config (via RDAP) and forge tokens, and emit a single ICS
calendar.

* silt: open (r3#25 0.45, r4#8 0.60 on HN posts); r4#7 was crowded by network
  certificate scanners
* web: [TokenTimer](https://peerlist.io/tokentimerio/project/tokentimer) is
  a register-it-yourself platform; certificate monitors scan networks. Nothing
  *discovers* expiring items from project files.

### 6. Deprecation warnings grouped by originating dependency

A pytest plugin (or an equivalent for `node --trace-deprecation`) that groups
the warnings a test run triggers by the package that issued them, with removal
versions where known: an upgrade-readiness report.

* silt: open (r5#9, 0.52); r5#10 was crowded by deprecation helper libraries
* web: pytest's summary is keyed by test location; no per-package plugin found
* small and cheap to build, but every Python project could use it

### 7. Hostile-environment harness for CLIs

Run a CLI's own test commands under: read-only or missing HOME, no network,
`LANG=C`, `TERM=dumb`, not a TTY, `NO_COLOR`, paths with spaces, unicode or
leading dashes, and SIGTERM in the middle of a run.

* silt: r5#1 open (0.55); r5#2 crowded by general CLI test frameworks (scrut,
  testy-command-line), not by tools that vary the environment. r2#7 (NO_COLOR,
  non-TTY) was crowded by color-detection libraries.
* web: the checks exist piecemeal inside individual test suites (jj, CPython,
  zsh); no harness found

Weaker or niche: unique error messages
([wrapdupes](https://beta.pkg.go.dev/github.com/mitioshi/wrapdupes) covers Go
`fmt.Errorf` only); timeout budgets across service chains (no tool, but hard
to extract the values); CLI interface diffs between releases
([usage diff](https://usage.jdx.dev/cli/reference/diff) needs a usage spec;
the program's own shell-completion output could serve as the spec).

## Open in silt, but taken

"(known)" means from memory, not re-checked today.

* outbound network during builds (r1#8, r2#4, r2#5): StepSecurity
  harden-runner (known); crowded only in jargon (r3#2)
* which test covers a line (r1#5, r2#10, r3#6): coverage.py dynamic contexts,
  `pytest-cov --cov-context=test` (known)
* test pollution (r1#15): detect-test-pollution, pytest-bisect-tests (silt
  found them at 0.49 and 0.42)
* minimum dependency versions (r1#6): pessimist (0.53), `uv --resolution
  lowest-direct`, cargo minimal-versions (known)
* new install scripts on update (r1#7, r2#12): Socket, LavaMoat allow-scripts,
  pnpm's build allowlist (known)
* REST API breaking changes (r1#27): oasdiff, openapi-diff (known)
* why this transitive version (r1#18): `npm explain`, `pnpm why`,
  `cargo tree -i`, `uv tree --invert` (known); pip-blame (r3#13, 0.62)
* TODO -> closed issue (r1#31, r3#7): todocheck (known), krank
* shrink failing input (r1#35, r3#9): C-Reduce, shrinkray (0.54)
* docs drift (r1#22): readme-drift (0.47)
* GNU vs BSD shell flags (r1#34, r3#11, r4#3, r4#4): shellport (0.60),
  portsh (0.41, exact description), opsscript-gate
* single-maintainer dependencies (r2#24, r3#1, r4#5, r4#6): ossuary-risk,
  bus-factor-index, pkg-health (r4#6)
* full disk / ENOSPC (r2#17, r3#3): libfiu, unreliablefs, `/dev/full` (known)
* CSV column drift (r2#23, r3#4): schema-drift-guard, pipedog
* case collisions / Windows-invalid names (r3#17): casecrash, portacheck
* JS-disabled comparison (r2#25, r3#12, r4#11, r4#12): open under all four
  wordings, but SEO tools have it:
  [js-differencer](https://defaced.dev/tools/js-differencer),
  [WWJD](https://onely.com/tools/wwjd), JetOctopus, Page-oscope
* hard-coded hosts (r3#23, r4#13, r4#14): open under three wordings, but these
  exist as rules inside bigger linters:
  [Revet](https://umitkavala.github.io/revet/docs/analyzers/hardcoded-endpoints),
  [Shoulder](https://shoulder.dev/rules/python-hardcoded-dev-urls), Repotoire,
  PMD
* time spent waiting on builds (r2#21, r4#27, r4#28): open under three
  wordings; Uber LDA -> DevZero ODA,
  [ProcessLens](https://github.com/ProcessLens/processlens-vscode), Gradle
  Develocity
* doc example coverage (r3#19, r4#25, r4#26): ansys-tools-example-coverage
  (0.64)
* runtime requirements by tracing (r5#17, r5#18): ReproZip, SlimToolkit
  (known)
* vendored library copies (r5#19, r5#20): osv-scanner's vendored C/C++
  detection, syft (known)
* migration rollback (r5#5, r5#6): django-test-migrations, alembic fixtures
* lint rules that already pass (r5#11, r5#12): adjacent are lint-to-the-future
  and legacy-lint-manager

## What this says about silt

* **Wording decides the verdict.** Plain wordings often miss the tool and
  return "open": egress monitoring (r2#4 0.45 -> r3#2 crowded), fault
  injection (r2#17 -> r3#3), schema drift (r2#23 0.50 -> r3#4 0.75).
* **Exact matches score below 0.55**, so the verdict stays open: portsh 0.41
  (its description is literally the idea), krank 0.44, readme-drift 0.47,
  detect-test-pollution 0.49, pessimist 0.53, shrinkray 0.54. Long queries
  with specifics (`sed -i, readlink -f, date -d`) seem to dilute the scores.
* **Noise inflates crowded.** Generic names with empty or placeholder
  descriptions count as close matches: `testing-testing-1-2-3` (r3#5,
  saturated), `repo-generator-20250729-test-12` (r3#22), `semantic-versioning`
  (r4#19, saturated), `micro`/`microservices` (r5#22). So a "crowded" verdict
  also needs reading.
* **Automatic source selection misses language-agnostic tools in niche
  registries.** krank is on Hackage, but no default-source run searched
  Hackage; `--sources hackage` finds it at once (0.44, still under the
  threshold).
* **Blind spots by design:** features inside big tools (coverage contexts,
  `npm explain`, pytest's warning summary), web/SaaS tools (the SEO JS
  checkers), and implementations living inside other projects' test suites.
  The last one is useful: when the web shows many in-repo copies and silt
  shows no package, that is pattern P1.
* One run (r5#18) lost GitHub and PyPI and still said open; the JSON lists the
  failed sources, so a batch consumer must check `sources_failed`.

Ideas for silt (no commercial LLMs): on an open verdict, suggest or run a
couple of rewordings (a local synonym/jargon table); down-weight matches with
an empty or name-only description; show the best match even when it is under
the threshold, since it was often exactly right; for ideas that name no
language, also search the cheap niche registries (Hackage, crates).
