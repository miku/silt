// Command parity runs patent and silt side by side on the reference ideas and
// reports where they differ: the verdict level and headline, the sources
// selected, reached and failed, the match URLs, and the similarities of the
// matches both found. The two run at the same time against the live
// registries, so that they see the same data as far as the registries allow.
//
//	go run ./scripts/parity                 # --fast --keyword-only
//	go run ./scripts/parity -embeddings     # --fast, ranked with the model
//
// Run it from the repository root. It builds silt from the working tree, and
// keeps both tools' output for each idea in -o for a closer look. It exits 1
// if any idea differs.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/miku/silt/model"
)

// patentVersion is the reference the port is checked against.
const patentVersion = "patent 0.14.1"

// limit makes both tools keep every ranked match. In keyword mode many
// matches tie, and which of them make a top 50 depends on the dedup order,
// which in patent changes from run to run.
const limit = "10000"

// tolerance is how far the similarity of a match both tools found may
// differ (D2).
const tolerance = 1e-3

func main() {
	log.SetFlags(0)
	var (
		patent     = flag.String("patent", "patent", "the patent binary, at "+patentVersion)
		ideasFile  = flag.String("ideas", "scripts/parity/ideas.txt", "the reference ideas, one per line")
		outDir     = flag.String("o", "", "where to keep both tools' output (default: a new temporary directory)")
		embeddings = flag.Bool("embeddings", false, "rank with the embedding model instead of --keyword-only")
		pause      = flag.Duration("pause", defaultPause(), "pause between ideas, for GitHub's search rate limit")
	)
	flag.Parse()

	ctx := context.Background()
	if v := version(ctx, *patent); v != patentVersion {
		log.Fatalf("%s is %q, want %q", *patent, v, patentVersion)
	}
	ideas, err := readIdeas(*ideasFile)
	if err != nil {
		log.Fatal(err)
	}
	dir := *outDir
	if dir == "" {
		if dir, err = os.MkdirTemp("", "silt-parity-"); err != nil {
			log.Fatal(err)
		}
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	silt := filepath.Join(dir, "silt")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", silt, "./cmd/silt").CombinedOutput(); err != nil {
		log.Fatalf("building silt: %v\n%s", err, out)
	}

	args := []string{"--json", "--fast", "--limit", limit}
	if !*embeddings {
		args = append(args, "--keyword-only")
	}
	fmt.Printf("    %-9s %-9s %-6s %-9s %s\n", "patent", "silt", "URLs", "max Δsim", "idea")
	differing := 0
	for i, idea := range ideas {
		if i > 0 {
			time.Sleep(*pause)
		}
		var p, s run
		var wg sync.WaitGroup
		wg.Go(func() { p = execute(ctx, *patent, args, idea) })
		wg.Go(func() { s = execute(ctx, silt, args, idea) })
		wg.Wait()
		for tool, r := range map[string]run{"patent": p, "silt": s} {
			name := filepath.Join(dir, fmt.Sprintf("%02d-%s", i+1, tool))
			if err := errors.Join(
				os.WriteFile(name+".json", r.stdout, 0o644),
				os.WriteFile(name+".log", r.stderr, 0o644),
			); err != nil {
				log.Fatal(err)
			}
		}

		c := compare(p, s)
		fmt.Printf("%2d  %-9s %-9s %-6s %-9s %s\n", i+1, p.level(), s.level(), c.jaccard, c.maxDelta, idea)
		for _, d := range c.diffs {
			fmt.Printf("      %s\n", strings.ReplaceAll(d, "\n", "\n      "))
		}
		if len(c.diffs) > 0 {
			differing++
		}
	}
	fmt.Printf("\n%d of %d ideas differ; the outputs are in %s\n", differing, len(ideas), dir)
	if differing > 0 {
		os.Exit(1)
	}
}

// defaultPause keeps the two GitHub searches per idea (one per tool) under
// GitHub's limit of 30 a minute with a token, or 10 without one.
func defaultPause() time.Duration {
	if os.Getenv("GITHUB_TOKEN") != "" {
		return 4 * time.Second
	}
	return 12 * time.Second
}

func version(ctx context.Context, bin string) string {
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		log.Fatalf("%s --version: %v", bin, err)
	}
	return strings.TrimSpace(string(out))
}

// readIdeas reads one idea per line, skipping blank lines and # comments.
func readIdeas(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var ideas []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && !strings.HasPrefix(line, "#") {
			ideas = append(ideas, line)
		}
	}
	return ideas, sc.Err()
}

// run is what one tool did for one idea.
type run struct {
	exit           int
	stdout, stderr []byte
	out            *output // nil if stdout isn't the JSON document
}

// output is the part of the --json document that is compared.
type output struct {
	Verdict model.Verdict `json:"verdict"`
	Matches []model.Match `json:"matches"`
}

func (r run) level() string {
	if r.out == nil {
		return fmt.Sprintf("exit %d", r.exit)
	}
	return strings.ToLower(r.out.Verdict.Level.String())
}

// lastLine is the last line r wrote to stderr: for a rejected idea, the
// error.
func (r run) lastLine() string {
	lines := strings.Split(strings.TrimSpace(string(r.stderr)), "\n")
	return lines[len(lines)-1]
}

func execute(ctx context.Context, bin string, args []string, idea string) run {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, append(slices.Clone(args), "--", idea)...)
	// Both tools must ask the registries at the same moment, and the same
	// way: silt's result cache would answer a rerun from an earlier one, its
	// catalog cache would search a Homebrew catalog up to a day old, and its
	// browser and its PyPI index would reach PyPI, which patent can't.
	// patent ignores all four.
	cmd.Env = append(os.Environ(), "SILT_CACHE_TTL=0", "SILT_CATALOG_TTL=0", "SILT_BROWSER=off", "SILT_PYPI_INDEX=off")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	r := run{}
	// The exit code is the verdict level, so a non-zero one is no failure.
	var exitErr *exec.ExitError
	if err := cmd.Run(); errors.As(err, &exitErr) {
		r.exit = exitErr.ExitCode()
	} else if err != nil {
		log.Fatalf("running %s: %v", bin, err)
	}
	r.stdout, r.stderr = stdout.Bytes(), stderr.Bytes()
	var out output
	if json.Unmarshal(r.stdout, &out) == nil {
		r.out = &out
	}
	return r
}

// comparison is how silt's run of an idea differs from patent's.
type comparison struct {
	jaccard  string // of the match URLs
	maxDelta string // of the similarities of the matches both found
	diffs    []string
}

func compare(p, s run) comparison {
	if p.out == nil || s.out == nil {
		c := comparison{jaccard: "-", maxDelta: "-"}
		// The messages name the tool.
		pErr := strings.ReplaceAll(p.lastLine(), "patent", "silt")
		if p.out != nil || s.out != nil || p.exit != s.exit || pErr != s.lastLine() {
			c.diffs = append(c.diffs, fmt.Sprintf("patent: %s\nsilt:   %s", p.lastLine(), s.lastLine()))
		}
		return c
	}
	pv, sv := p.out.Verdict, s.out.Verdict
	var diffs []string
	if pv.Level != sv.Level {
		diffs = append(diffs, fmt.Sprintf("level: patent %v, silt %v", pv.Level, sv.Level))
	}
	if pv.Headline != sv.Headline {
		diffs = append(diffs, fmt.Sprintf("headline:\n  patent %q\n  silt   %q", pv.Headline, sv.Headline))
	}
	diffs = append(diffs, sourceDiff("selected", selected(pv), selected(sv))...)
	diffs = append(diffs, sourceDiff("reached", pv.SourcesChecked, sv.SourcesChecked)...)
	diffs = append(diffs, sourceDiff("failed", pv.SourcesFailed, sv.SourcesFailed)...)

	pm, sm := byKey(p.out.Matches), byKey(s.out.Matches)
	var onlyPatent, onlySilt, otherSource []model.Match
	var maxDelta float64
	for k, m := range pm {
		other, ok := sm[k]
		if !ok {
			onlyPatent = append(onlyPatent, m)
			continue
		}
		// Which duplicate wins dedup, and so its source, is unspecified
		// in patent.
		if m.Source != other.Source {
			otherSource = append(otherSource, m)
		}
		maxDelta = max(maxDelta, math.Abs(float64(m.Similarity)-float64(other.Similarity)))
	}
	for k, m := range sm {
		if _, ok := pm[k]; !ok {
			onlySilt = append(onlySilt, m)
		}
	}
	if len(onlyPatent) > 0 {
		diffs = append(diffs, fmt.Sprintf("only patent found %d: %s", len(onlyPatent), countBySource(onlyPatent)))
	}
	if len(onlySilt) > 0 {
		diffs = append(diffs, fmt.Sprintf("only silt found %d: %s", len(onlySilt), countBySource(onlySilt)))
	}
	if len(otherSource) > 0 {
		diffs = append(diffs, fmt.Sprintf("found by both, from another source in patent: %d (%s)",
			len(otherSource), countBySource(otherSource)))
	}
	if maxDelta > tolerance {
		diffs = append(diffs, fmt.Sprintf("similarities differ by up to %.1e", maxDelta))
	}

	common := len(pm) - len(onlyPatent)
	jaccard := 1.0
	if union := len(pm) + len(onlySilt); union > 0 {
		jaccard = float64(common) / float64(union)
	}
	return comparison{
		jaccard:  fmt.Sprintf("%.2f", jaccard),
		maxDelta: fmt.Sprintf("%.1e", maxDelta),
		diffs:    diffs,
	}
}

// selected are the sources a run searched: those it reached and those that
// failed.
func selected(v model.Verdict) []model.Source {
	return append(slices.Clone(v.SourcesChecked), v.SourcesFailed...)
}

// sourceDiff compares two lists of sources as sets.
func sourceDiff(what string, p, s []model.Source) []string {
	p, s = slices.Sorted(slices.Values(p)), slices.Sorted(slices.Values(s))
	if slices.Equal(p, s) {
		return nil
	}
	return []string{fmt.Sprintf("%s: patent [%s], silt [%s]", what, kebabNames(p), kebabNames(s))}
}

func kebabNames(ss []model.Source) string {
	names := make([]string, len(ss))
	for i, s := range ss {
		names[i] = s.KebabName()
	}
	return strings.Join(names, " ")
}

// byKey indexes matches by their dedup key: the URL, or the source and name
// for a match without one.
func byKey(matches []model.Match) map[string]model.Match {
	m := make(map[string]model.Match, len(matches))
	for _, match := range matches {
		k := strings.TrimSpace(match.URL)
		if k == "" {
			k = match.Source.KebabName() + " " + match.Name
		}
		m[k] = match
	}
	return m
}

// countBySource summarises matches as "docker-hub 3, npm 1", in source order.
func countBySource(matches []model.Match) string {
	n := make(map[model.Source]int)
	for _, m := range matches {
		n[m.Source]++
	}
	var parts []string
	for _, s := range model.All() {
		if n[s] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", s.KebabName(), n[s]))
		}
	}
	return strings.Join(parts, ", ")
}
