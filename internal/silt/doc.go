// Package silt holds what silt's other packages share: the error types that
// the sources and the ranker return, and the version.
//
// silt is a prior-art search for code ideas, ported from patent
// (https://github.com/r14dd/patent). Given a plain-English dev-tool idea, it
// fans one query out across the open-source ecosystem — crates.io, npm, PyPI,
// GitHub, Go, Maven, NuGet, RubyGems, Docker Hub, Homebrew, Packagist, Hex,
// Artifact Hub, the Arch Linux repositories and the AUR, Hackage, Nixpkgs, the
// VS Code Marketplace, the JetBrains Marketplace, Hacker News and, on request,
// Codeberg — ranks what it finds by semantic similarity to the idea, and
// estimates from those similarities whether the idea has already been built.
// The exact set searched is chosen per query; whichever sources actually
// responded are always surfaced.
//
// Integrity principle: this tool can prove something exists, but never that
// it doesn't — it only searched some sources. All output is scoped to "what
// was found in the sources checked".
//
// silt is a command, not a library, so all of its packages are internal:
// sources fans out to the registries, rank orders the matches, verdict turns
// their similarities into an integrity-scoped model.Verdict, pipeline runs
// the three in turn, and tui shows the result.
package silt
