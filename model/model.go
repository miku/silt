// Package model holds the core domain types shared across the pipeline.
//
// They are deliberately small and JSON-serializable so that the --json path
// and the TUI render from the same data.
package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Query is a user's idea, plus keywords derived from it for keyword-based
// source APIs.
//
// The full Idea string is what the embedder ranks against; Keywords is the
// cleaned-up query handed to registry search endpoints.
type Query struct {
	Idea     string
	Keywords []string
}

// Source is where a [Match] came from. It is always surfaced to the user for
// transparency.
//
// It marshals to a stable kebab-case identifier (e.g. "crates-io"). The old
// PascalCase names (e.g. "CratesIo") are accepted when unmarshaling, for
// backward compatibility. The zero value is not a valid source.
type Source int

const (
	CratesIO Source = iota + 1
	GitHub
	Npm
	PyPI
	HackerNews
	Go
	Maven
	RubyGems
	DockerHub
	VSCodeMarketplace
	JetBrains
	NuGet
	Homebrew
	Packagist
	Hex
	ArtifactHub
	AUR
	Hackage
	Nixpkgs
)

// sourceNames is indexed by Source.
var sourceNames = [...]struct{ kebab, display, pascal string }{
	CratesIO:          {"crates-io", "crates.io", "CratesIo"},
	GitHub:            {"github", "GitHub", "GitHub"},
	Npm:               {"npm", "npm", "Npm"},
	PyPI:              {"pypi", "PyPI", "PyPI"},
	HackerNews:        {"hacker-news", "Hacker News", "HackerNews"},
	Go:                {"go", "Go", "Go"},
	Maven:             {"maven", "Maven", "Maven"},
	RubyGems:          {"rubygems", "RubyGems", "RubyGems"},
	DockerHub:         {"docker-hub", "Docker Hub", "DockerHub"},
	VSCodeMarketplace: {"vscode-marketplace", "VS Code", "VsCodeMarketplace"},
	JetBrains:         {"jetbrains", "JetBrains", "JetBrains"},
	NuGet:             {"nuget", "NuGet", "NuGet"},
	Homebrew:          {"homebrew", "Homebrew", "Homebrew"},
	Packagist:         {"packagist", "Packagist", "Packagist"},
	Hex:               {"hex", "Hex", "Hex"},
	ArtifactHub:       {"artifact-hub", "Artifact Hub", "ArtifactHub"},
	AUR:               {"aur", "AUR", "Aur"},
	Hackage:           {"hackage", "Hackage", "Hackage"},
	Nixpkgs:           {"nixpkgs", "Nixpkgs", "Nixpkgs"},
}

// sourceAliases are the extra spellings [ParseSource] accepts on top of each
// kebab-case name.
var sourceAliases = map[string]Source{
	"crates_io":          CratesIO,
	"crates.io":          CratesIO,
	"hacker_news":        HackerNews,
	"hackernews":         HackerNews,
	"docker_hub":         DockerHub,
	"dockerhub":          DockerHub,
	"docker":             DockerHub,
	"vscode_marketplace": VSCodeMarketplace,
	"vscode":             VSCodeMarketplace,
	"intellij":           JetBrains,
	"brew":               Homebrew,
	"artifact_hub":       ArtifactHub,
	"artifacthub":        ArtifactHub,
	"nix":                Nixpkgs,
}

// All returns every known source, in a stable order. The caller may modify
// the returned slice.
func All() []Source {
	all := make([]Source, 0, len(sourceNames)-1)
	for s := CratesIO; s <= Nixpkgs; s++ {
		all = append(all, s)
	}
	return all
}

func (s Source) valid() bool { return s >= CratesIO && s <= Nixpkgs }

// KebabName returns the name used in --sources / --exclude and in JSON.
func (s Source) KebabName() string {
	if !s.valid() {
		return fmt.Sprintf("Source(%d)", int(s))
	}
	return sourceNames[s].kebab
}

// String returns the display name, e.g. "crates.io" or "VS Code".
func (s Source) String() string {
	if !s.valid() {
		return fmt.Sprintf("Source(%d)", int(s))
	}
	return sourceNames[s].display
}

// ParseSource parses a source name as given on the command line: any
// kebab-case name or alias, in any case. The error lists every valid name.
func ParseSource(name string) (Source, error) {
	lower := strings.ToLower(name)
	for _, s := range All() {
		if lower == s.KebabName() {
			return s, nil
		}
	}
	if s, ok := sourceAliases[lower]; ok {
		return s, nil
	}
	valid := make([]string, 0, len(sourceNames))
	for _, s := range All() {
		valid = append(valid, s.KebabName())
	}
	return 0, fmt.Errorf("unknown source `%s`. Valid: %s", name, strings.Join(valid, ", "))
}

// MarshalText implements encoding.TextMarshaler.
func (s Source) MarshalText() ([]byte, error) {
	if !s.valid() {
		return nil, fmt.Errorf("invalid source %d", int(s))
	}
	return []byte(s.KebabName()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. It accepts exactly the
// kebab-case name or the old PascalCase name, as patent's serde
// representation does — not the looser aliases of [ParseSource].
func (s *Source) UnmarshalText(text []byte) error {
	name := string(text)
	for _, v := range All() {
		if name == sourceNames[v].kebab || name == sourceNames[v].pascal {
			*s = v
			return nil
		}
	}
	return fmt.Errorf("unknown source %q", name)
}

// Match is a single piece of prior art found in a [Source].
type Match struct {
	Name        string `json:"name"`
	Source      Source `json:"source"`
	URL         string `json:"url"`
	Description string `json:"description"`
	// Popularity is a source-specific popularity signal (downloads, stars,
	// points…), or nil because not every source exposes one.
	Popularity *uint64 `json:"popularity"`
	// Similarity to the idea, in [0, 1]. Filled in by ranking.
	Similarity float32 `json:"similarity"`
	// LastUpdated is when the project was last updated, as a whole-second
	// RFC 3339 UTC timestamp (e.g. "2026-05-15T06:13:41Z").
	//
	// It is nil whenever the source's search response carries no such date —
	// fetching one per result would mean an extra request per match, which
	// the per-source timeout does not have room for. Also nil when a date was
	// present but unparseable; see the freshness package, which owns the
	// normalisation and the staleness classification built on top of it.
	LastUpdated *string `json:"last_updated"`
}

// Saturation says how crowded the space looks, based on what was found in
// the sources checked.
//
// The values are ordered Open < Crowded < Saturated so that the verdict level
// can be floored against the similarity data with max: the model is never
// allowed to under-rate a space that the embeddings show is clearly
// populated.
//
// It marshals to lowercase ("open", "crowded", "saturated"). The old
// PascalCase names are accepted when unmarshaling, for backward
// compatibility.
type Saturation int

const (
	// Open means nothing close was found in the sources checked. 🟢
	Open Saturation = iota
	// Crowded means a few adjacent things exist. 🟡
	Crowded
	// Saturated means the space is densely populated. 🔴
	Saturated
)

var saturationNames = [...]string{Open: "Open", Crowded: "Crowded", Saturated: "Saturated"}

func (s Saturation) valid() bool { return s >= Open && s <= Saturated }

// ExitCode is the process exit code for this level, so that scripts can gate
// on it: 0 for Open, 1 for Crowded, 2 for Saturated.
func (s Saturation) ExitCode() int { return int(s) }

// String returns the PascalCase name, e.g. "Open".
func (s Saturation) String() string {
	if !s.valid() {
		return fmt.Sprintf("Saturation(%d)", int(s))
	}
	return saturationNames[s]
}

// MarshalText implements encoding.TextMarshaler.
func (s Saturation) MarshalText() ([]byte, error) {
	if !s.valid() {
		return nil, fmt.Errorf("invalid saturation %d", int(s))
	}
	return []byte(strings.ToLower(saturationNames[s])), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (s *Saturation) UnmarshalText(text []byte) error {
	name := string(text)
	for v, pascal := range saturationNames {
		if name == pascal || name == strings.ToLower(pascal) {
			*s = Saturation(v)
			return nil
		}
	}
	return fmt.Errorf("unknown saturation %q", name)
}

// Verdict is the model-written, integrity-scoped verdict.
//
// Invariant: copy is always phrased as "found in the sources checked" and
// never asserts that something does not exist anywhere.
type Verdict struct {
	Level    Saturation `json:"level"`
	Headline string     `json:"headline"`
	Gaps     []string   `json:"gaps"`
	// SourcesChecked are the sources that were searched successfully —
	// always surfaced for transparency.
	SourcesChecked []Source `json:"sources_checked"`
	// SourcesFailed are the sources that were selected but failed to respond
	// (e.g. a network error or rate limit). Surfaced so that a thin or empty
	// result isn't mistaken for "nothing out there" when coverage was
	// actually reduced.
	SourcesFailed []Source `json:"sources_failed"`
	Caveat        string   `json:"caveat"`
}

// MarshalJSON writes nil slices as [] rather than null, matching patent's
// output.
func (v Verdict) MarshalJSON() ([]byte, error) {
	type plain Verdict
	p := plain(v)
	if p.Gaps == nil {
		p.Gaps = []string{}
	}
	if p.SourcesChecked == nil {
		p.SourcesChecked = []Source{}
	}
	if p.SourcesFailed == nil {
		p.SourcesFailed = []Source{}
	}
	return json.Marshal(p)
}
