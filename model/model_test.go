package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSourceAllReturnsEveryVariant(t *testing.T) {
	if got := len(All()); got != 19 {
		t.Fatalf("len(All()) = %d, want 19", got)
	}
}

func TestExitCodeGraduated(t *testing.T) {
	for _, tc := range []struct {
		s    Saturation
		want int
	}{{Open, 0}, {Crowded, 1}, {Saturated, 2}} {
		if got := tc.s.ExitCode(); got != tc.want {
			t.Errorf("%v.ExitCode() = %d, want %d", tc.s, got, tc.want)
		}
	}
}

func TestSaturationIsOrdered(t *testing.T) {
	if !(Open < Crowded && Crowded < Saturated) {
		t.Fatal("want Open < Crowded < Saturated")
	}
	if got := max(Crowded, Open); got != Crowded {
		t.Fatalf("max(Crowded, Open) = %v", got)
	}
}

func TestSourceNamesInPatentOrder(t *testing.T) {
	var kebab, display []string
	for _, s := range All() {
		kebab = append(kebab, s.KebabName())
		display = append(display, s.String())
	}
	wantKebab := []string{
		"crates-io", "github", "npm", "pypi", "hacker-news", "go", "maven",
		"rubygems", "docker-hub", "vscode-marketplace", "jetbrains", "nuget",
		"homebrew", "packagist", "hex", "artifact-hub", "aur", "hackage", "nixpkgs",
	}
	wantDisplay := []string{
		"crates.io", "GitHub", "npm", "PyPI", "Hacker News", "Go", "Maven",
		"RubyGems", "Docker Hub", "VS Code", "JetBrains", "NuGet", "Homebrew",
		"Packagist", "Hex", "Artifact Hub", "AUR", "Hackage", "Nixpkgs",
	}
	if !reflect.DeepEqual(kebab, wantKebab) {
		t.Errorf("kebab names = %v", kebab)
	}
	if !reflect.DeepEqual(display, wantDisplay) {
		t.Errorf("display names = %v", display)
	}
}

func TestParseSource(t *testing.T) {
	for in, want := range map[string]Source{
		"npm":                Npm,
		"crates-io":          CratesIO,
		"crates_io":          CratesIO,
		"crates.io":          CratesIO,
		"CRATES-IO":          CratesIO,
		"hacker_news":        HackerNews,
		"hackernews":         HackerNews,
		"docker":             DockerHub,
		"dockerhub":          DockerHub,
		"vscode":             VSCodeMarketplace,
		"vscode_marketplace": VSCodeMarketplace,
		"nix":                Nixpkgs,
		"jetbrains":          JetBrains,
		"intellij":           JetBrains,
		"brew":               Homebrew,
		"artifacthub":        ArtifactHub,
		"AUR":                AUR,
	} {
		got, err := ParseSource(in)
		if err != nil || got != want {
			t.Errorf("ParseSource(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}

func TestParseSourceErrorListsValidNames(t *testing.T) {
	_, err := ParseSource("unknown")
	if err == nil {
		t.Fatal("an unrecognised source name must return an error")
	}
	want := "unknown source `unknown`. Valid: crates-io, github, npm, pypi, hacker-news, go, " +
		"maven, rubygems, docker-hub, vscode-marketplace, jetbrains, nuget, homebrew, " +
		"packagist, hex, artifact-hub, aur, hackage, nixpkgs"
	if err.Error() != want {
		t.Errorf("error = %q\nwant    %q", err, want)
	}
}

func TestSourceJSON(t *testing.T) {
	b, err := json.Marshal([]Source{CratesIO, VSCodeMarketplace, HackerNews})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `["crates-io","vscode-marketplace","hacker-news"]`; got != want {
		t.Errorf("marshal = %s, want %s", got, want)
	}
	// The old PascalCase names are still accepted on input.
	var got []Source
	if err := json.Unmarshal([]byte(`["crates-io","CratesIo","VsCodeMarketplace","Aur"]`), &got); err != nil {
		t.Fatal(err)
	}
	if want := []Source{CratesIO, CratesIO, VSCodeMarketplace, AUR}; !reflect.DeepEqual(got, want) {
		t.Errorf("unmarshal = %v, want %v", got, want)
	}
	// JSON is not the CLI: the loose aliases are not part of the format.
	for _, bad := range []string{`"docker"`, `"crates.io"`, `"Crates-Io"`} {
		var s Source
		if err := json.Unmarshal([]byte(bad), &s); err == nil {
			t.Errorf("unmarshal(%s) succeeded, want error", bad)
		}
	}
	if _, err := json.Marshal(Source(0)); err == nil {
		t.Error("the zero Source must not marshal")
	}
}

func TestSaturationJSON(t *testing.T) {
	b, err := json.Marshal([]Saturation{Open, Crowded, Saturated})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `["open","crowded","saturated"]`; got != want {
		t.Errorf("marshal = %s, want %s", got, want)
	}
	var got []Saturation
	if err := json.Unmarshal([]byte(`["open","Crowded","Saturated"]`), &got); err != nil {
		t.Fatal(err)
	}
	if want := []Saturation{Open, Crowded, Saturated}; !reflect.DeepEqual(got, want) {
		t.Errorf("unmarshal = %v, want %v", got, want)
	}
	var s Saturation
	if err := json.Unmarshal([]byte(`"OPEN"`), &s); err == nil {
		t.Error(`unmarshal("OPEN") succeeded, want error`)
	}
}

func TestMatchJSONWritesNullForMissingFields(t *testing.T) {
	b, err := json.Marshal(Match{Name: "tokio", Source: CratesIO, URL: "https://crates.io/crates/tokio"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"tokio","source":"crates-io","url":"https://crates.io/crates/tokio",` +
		`"description":"","popularity":null,"similarity":0,"last_updated":null}`
	if string(b) != want {
		t.Errorf("marshal = %s\nwant      %s", b, want)
	}
}

func TestMatchJSONRoundTrip(t *testing.T) {
	pop := uint64(950_000_000)
	ts := "2026-05-15T06:13:41Z"
	in := Match{
		Name:        "tokio",
		Source:      CratesIO,
		URL:         "https://crates.io/crates/tokio",
		Description: "An event-driven, non-blocking I/O platform.",
		Popularity:  &pop,
		Similarity:  0.85,
		LastUpdated: &ts,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"similarity":0.85`) {
		t.Errorf("float32 similarity must keep its short form: %s", b)
	}
	var out Match
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

func TestVerdictJSONWritesEmptyListsNotNull(t *testing.T) {
	b, err := json.Marshal(Verdict{Level: Open, Headline: "h", Caveat: "c"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"level":"open","headline":"h","gaps":[],"sources_checked":[],"sources_failed":[],"caveat":"c"}`
	if string(b) != want {
		t.Errorf("marshal = %s\nwant      %s", b, want)
	}
}

func TestVerdictJSONRoundTrip(t *testing.T) {
	in := Verdict{
		Level:          Crowded,
		Headline:       "Crowded — 2 closely-related tools turned up in the sources checked.",
		Gaps:           []string{"a TUI"},
		SourcesChecked: []Source{Npm, CratesIO},
		SourcesFailed:  []Source{PyPI},
		Caveat:         "caveat",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Verdict
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

func TestVerdictJSONMissingSourcesFailedDecodesAsEmpty(t *testing.T) {
	// Output written before sources_failed existed must still decode.
	var v Verdict
	err := json.Unmarshal([]byte(`{"level":"Saturated","headline":"h","gaps":[],"sources_checked":["npm"],"caveat":"c"}`), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v.Level != Saturated || len(v.SourcesFailed) != 0 {
		t.Errorf("decoded %+v", v)
	}
}
