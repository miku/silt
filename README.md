# silt

A prior-art search for your code ideas: has this dev tool already been
shipped? silt is an idiomatic Go port of [patent](https://github.com/r14dd/patent),
and the first step towards a tool for exploring code projects.

> silt can prove that something **exists**, but never that it **doesn't** — it
> only searched some sources. Every verdict is scoped to "found in the sources
> checked", and sources that were selected but failed are reported as such.

**Status:** work in progress; see [notes/todo.md](notes/todo.md). Today silt
searches all 19 of patent's sources (crates.io, npm, PyPI, GitHub, Go, Maven,
NuGet, RubyGems, Docker Hub, Homebrew, Packagist, Hex, Artifact Hub, AUR,
Hackage, Nixpkgs, the VS Code and JetBrains marketplaces, Hacker News), ranks
matches by semantic similarity (all-MiniLM-L6-v2 in pure Go, downloaded once,
about 90 MB) or by keyword overlap, and gives a verdict from the similarity
data. LLM verdicts and the TUI are still to come.

```shell
$ go install github.com/miku/silt/cmd/silt@latest
$ silt --fast --json "cli tool that kills a process on a given port"
$ silt --keyword-only --fast --json "react component for infinite scroll"   # no model
$ silt --list-sources
```

The exit code is the verdict, so scripts can gate on it: 0 Open, 1 Crowded,
2 Saturated. Errors exit 1, argument errors 2.

## Development

```shell
$ go run ./scripts/fetchmodel   # once: the model for the embedding tests
$ make test                     # hermetic, no network
$ make test-live                # smoke tests against the real registries
$ make lint
```

## License

MIT. silt is a derivative of patent, © Riad Mukhtarov, used under the MIT
license; see [NOTICE](NOTICE).
