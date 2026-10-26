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
about 90 MB) or by keyword overlap, and asks an LLM for the verdict: a local
Ollama (`qwen3.5` by default) or any OpenAI-compatible API. The verdict's
level never drops below what the similarity data shows, and a headline or
gap that claims something doesn't exist is replaced or dropped. If the model
fails, or with `--fast`, the verdict comes from the similarity data alone.
The TUI is still to come.

```shell
$ go install github.com/miku/silt/cmd/silt@latest
$ silt --json "cli tool that kills a process on a given port"            # verdict via local Ollama
$ silt --json --api-base https://api.openai.com/v1 --model gpt-4o-mini "embedded vector database for mobile apps"
$ silt --fast --json "cli tool that kills a process on a given port"     # no LLM
$ silt --keyword-only --fast --json "react component for infinite scroll"   # no model at all
$ silt --print-prompt "kubernetes log viewer with live filtering"   # the verdict prompt, for your own agent
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
