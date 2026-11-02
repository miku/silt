// Package silt is a prior-art search for code ideas, ported from patent
// (https://github.com/r14dd/patent).
//
// Given a plain-English dev-tool idea, silt fans one query out across the
// open-source ecosystem — crates.io, npm, PyPI, GitHub, Go, Maven, NuGet,
// RubyGems, Docker Hub, Homebrew, Packagist, Hex, Artifact Hub, the Arch Linux
// repositories and the AUR, Hackage, Nixpkgs, the VS Code Marketplace, the
// JetBrains Marketplace, Hacker News and, on request, Codeberg — ranks what it
// finds, and gives an honest, scoped verdict on whether the
// idea has already been built. The exact set searched is chosen per query;
// whichever sources actually responded are always surfaced.
//
// Integrity principle: this tool can prove something exists, but never that
// it doesn't — it only searched some sources. All output is scoped to "what
// was found in the sources checked".
//
// The root package holds the error types shared by the library packages:
// [sources] fans out to the registries, rank orders matches, and [verdict]
// turns them into an integrity-scoped [model.Verdict] through any [llm.LLM]
// backend (a local Ollama or an OpenAI-compatible API).
//
// [sources]: https://pkg.go.dev/github.com/miku/silt/sources
// [verdict]: https://pkg.go.dev/github.com/miku/silt/verdict
// [model.Verdict]: https://pkg.go.dev/github.com/miku/silt/model#Verdict
// [llm.LLM]: https://pkg.go.dev/github.com/miku/silt/llm#LLM
package silt
