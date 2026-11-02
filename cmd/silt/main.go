// Command silt is a prior-art search for your code ideas: has this dev tool
// already been shipped?
//
//	silt "interactive cli to kill whatever's on a port"
//	silt --keyword-only --json "react component for infinite scroll"
//
// The verdict is estimated from how similar the matches are to the idea; see
// the verdict package.
//
// The exit code reflects the verdict — 0 Open, 1 Crowded, 2 Saturated — so
// that scripts can gate on it. Errors exit 1, argument errors 2.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/miku/silt/internal/browser"
	"github.com/miku/silt/internal/model"
	"github.com/miku/silt/internal/pipeline"
	"github.com/miku/silt/internal/rank"
	"github.com/miku/silt/internal/silt"
	"github.com/miku/silt/internal/sources"
	"github.com/miku/silt/internal/tui"
)

func main() {
	env := environment{
		stdout:           os.Stdout,
		stderr:           os.Stderr,
		isTerminal:       term.IsTerminal(int(os.Stdout.Fd())),
		stderrIsTerminal: term.IsTerminal(int(os.Stderr.Fd())),
		lookupEnv:        os.LookupEnv,
		runTUI:           tui.Run,
	}
	os.Exit(run(os.Args[1:], env))
}

// environment is what silt reads from and writes to besides its arguments.
type environment struct {
	stdout, stderr io.Writer
	// isTerminal reports whether stdout is a terminal, stderrIsTerminal the
	// same of stderr.
	isTerminal, stderrIsTerminal bool
	lookupEnv                    func(string) (string, bool)
	runTUI                       func(context.Context, tui.Options) error
}

// Errors shown to users verbatim, as patent words them.
//
//nolint:staticcheck // ST1005: capitalized and punctuated on purpose.
var (
	errJSONNeedsIdea = errors.New("--json requires an idea argument.")
	errNoIdea        = errors.New(`No idea provided. Usage: silt "your dev-tool idea here"`)
)

// usageError is a problem with the arguments themselves. As with clap, these
// exit 2, while errors at run time exit 1.
type usageError struct{ error }

// flags holds the parsed command line.
type flags struct {
	limit                     limitValue
	keywordOnly               bool
	listSources, checkSources bool
	json                      bool
	sources, add, exclude     listValue
	cacheTTL, catalogTTL      ttlValue
	completions               string
}

// run runs silt with args and returns its exit code.
func run(args []string, env environment) int {
	f := flags{limit: rank.DefaultLimit, cacheTTL: ttlValue(sources.DefaultCacheTTL), catalogTTL: ttlValue(sources.DefaultCatalogTTL)}
	exitCode := 0
	cmd := &cobra.Command{
		Use:   "silt [IDEA]",
		Short: "A prior-art search for your code ideas",
		Long: "A prior-art search for your code ideas.\n\n" +
			"IDEA is the dev-tool idea to search for, e.g.\n" +
			"\"interactive cli to kill whatever's on a port\".\n" +
			"Omit it to launch the interactive search TUI.",
		Version: silt.Version(),
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageError{fmt.Errorf("unexpected argument '%s' found", args[1])}
			}
			return nil
		},
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			exitCode, err = f.run(cmd, args, env)
			return err
		},
	}
	cmd.CompletionOptions.DisableDefaultCmd = true // it would shadow an idea starting with "completion"
	cmd.SetOut(env.stdout)
	cmd.SetErr(env.stderr)
	cmd.SetArgs(args)
	cmd.SetVersionTemplate("silt {{.Version}}\n")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })
	f.register(cmd)
	registerCompletions(cmd)

	if err := cmd.Execute(); err != nil {
		if errors.As(err, new(usageError)) {
			fmt.Fprintf(env.stderr, "error: %v\n\nFor more information, try '--help'.\n", err)
			return 2
		}
		fmt.Fprintf(env.stderr, "Error: %v\n", err)
		return 1
	}
	return exitCode
}

func (f *flags) register(cmd *cobra.Command) {
	fs := cmd.Flags()
	fs.SortFlags = false
	fs.Var(&f.limit, "limit", "Max number of matches to keep after ranking (must be at least 1)")
	fs.BoolVar(&f.keywordOnly, "keyword-only", false, "Skip the embedding model and rank by keyword overlap instead.\nUseful offline or to avoid the ~90 MB model download")
	fs.Var(&f.sources, "sources", "Only search these sources (comma-separated, e.g. \"npm,crates-io,github\").\nOverrides the automatic source detection. See --list-sources for valid names")
	fs.Var(&f.add, "add", "Also search these sources, on top of the ones picked from the idea\n(comma-separated). Codeberg is searched only when asked for, here or with\n--sources: its search takes 5-15 s")
	fs.Var(&f.exclude, "exclude", "Exclude these sources from the search (comma-separated)")
	fs.Var(&f.cacheTTL, "cache-ttl", "How long a source's results are reused for the same idea, e.g. 30m or 24h;\n0 turns the cache off. Failed searches are never cached. Can also be set\nvia SILT_CACHE_TTL")
	fs.Var(&f.catalogTTL, "catalog-ttl", "How long downloaded catalogs (Homebrew's, PyPI's project names) are kept,\ne.g. 6h or 168h; 0 downloads them on every run. Can also be set via\nSILT_CATALOG_TTL")
	fs.BoolVar(&f.listSources, "list-sources", false, "List all available source names, and when each is searched, and exit")
	fs.BoolVar(&f.checkSources, "check-sources", false, "Search each source once, without the caches, print whether it was reached,\nwalled, rate-limited or timed out, and how long it took, and exit.\n--sources, --add and --exclude choose the sources")
	fs.BoolVar(&f.json, "json", false, "Print structured JSON instead of launching the TUI")
	fs.StringVar(&f.completions, "completions", "", "Generate shell completions and exit ("+strings.Join(shells, ", ")+")")
	fs.BoolP("help", "h", false, "Print help")
	fs.BoolP("version", "V", false, "Print version")
}

// resolveTTL applies the precedence of a TTL flag, such as --cache-ttl, over
// its environment variable env, such as SILT_CACHE_TTL, over the default.
func resolveTTL(flagSet bool, flag time.Duration, env string, def time.Duration, lookupEnv func(string) (string, bool)) (time.Duration, error) {
	if flagSet {
		return flag, nil
	}
	if v, ok := lookupEnv(env); ok {
		d, err := parseTTL(v)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", env, err)
		}
		return d, nil
	}
	return def, nil
}

func parseSources(names []string) ([]model.Source, error) {
	var ss []model.Source
	for _, name := range names {
		s, err := model.ParseSource(name)
		if err != nil {
			return nil, err
		}
		ss = append(ss, s)
	}
	return ss, nil
}

// schemaVersion is that of the --json document: 2 is patent's 1 without the
// verdict's gaps, which only patent's LLM wrote.
const schemaVersion = 2

// output is the --json document.
type output struct {
	SchemaVersion int           `json:"schema_version"`
	Query         string        `json:"query"`
	Verdict       model.Verdict `json:"verdict"`
	Matches       []model.Match `json:"matches"`
}

func (f *flags) run(cmd *cobra.Command, args []string, env environment) (int, error) {
	stdout, stderr := env.stdout, env.stderr

	if f.completions != "" {
		return 0, generateCompletions(cmd, f.completions, stdout)
	}
	if f.listSources {
		for _, s := range model.All() {
			fmt.Fprintf(stdout, "%-22s %-13s %s\n", s.KebabName(), s, whenSearched(sources.SelectionOf(s)))
		}
		return 0, nil
	}

	var include []model.Source
	if len(f.sources) > 0 {
		ss, err := parseSources(f.sources)
		if err != nil {
			return 0, err
		}
		include = ss
	}
	add, err := parseSources(f.add)
	if err != nil {
		return 0, err
	}
	exclude, err := parseSources(f.exclude)
	if err != nil {
		return 0, err
	}

	ttl, err := resolveTTL(cmd.Flags().Changed("cache-ttl"), time.Duration(f.cacheTTL), "SILT_CACHE_TTL", sources.DefaultCacheTTL, env.lookupEnv)
	if err != nil {
		return 0, err
	}
	catalogTTL, err := resolveTTL(cmd.Flags().Changed("catalog-ttl"), time.Duration(f.catalogTTL), "SILT_CATALOG_TTL", sources.DefaultCatalogTTL, env.lookupEnv)
	if err != nil {
		return 0, err
	}
	browserPath, err := browser.Find(env.lookupEnv)
	if err != nil {
		return 0, err
	}

	ctx := context.Background()
	pcfg := pipeline.Config{
		KeywordOnly: f.keywordOnly,
		Limit:       int(f.limit),
		Include:     include,
		Add:         add,
		Exclude:     exclude,
	}
	if ttl > 0 {
		// Without a cache directory (no home directory), search uncached.
		if dir, err := sources.DefaultCacheDir(); err == nil {
			pcfg.Cache = &sources.Cache{Dir: dir, TTL: ttl, StaleIfError: sources.DefaultStaleIfError}
		}
	}
	if catalogTTL > 0 {
		if dir, err := sources.DefaultCatalogDir(); err == nil {
			pcfg.Catalogs = &sources.CatalogCache{Dir: dir, TTL: catalogTTL}
		}
	}
	// The names fallback is plain HTTP to PyPI's own index and JSON API, so
	// it is on unless turned off.
	if v, _ := env.lookupEnv("SILT_PYPI_INDEX"); v != "off" {
		pcfg.PyPIIndex = true
	}
	if browserPath != "" {
		// The browser needs a profile; without a cache directory, go without.
		if dir, err := browser.DefaultProfileDir(); err == nil {
			pcfg.Browser = &browser.Browser{Path: browserPath, ProfileDir: dir}
		}
	}
	if f.checkSources {
		checkSources(ctx, pcfg, stdout)
		return 0, nil
	}
	tuiOpts := tui.Options{Search: searcher(pcfg), Sources: pcfg.Sources}

	if len(args) == 0 {
		if f.json {
			return 0, errJSONNeedsIdea
		}
		if !env.isTerminal {
			return 0, errNoIdea
		}
		return 0, env.runTUI(ctx, tuiOpts)
	}
	idea := args[0]
	if err := pipeline.ValidateIdea(idea); err != nil {
		return 0, err
	}

	fmt.Fprintf(stderr, "Searching for prior art: \"%s\"\n", idea)
	fmt.Fprintf(stderr, "   keywords: %s\n", strings.Join(pipeline.BuildQuery(idea).Keywords, ", "))

	if !f.keywordOnly && !rank.ModelIsCached() {
		fmt.Fprintln(stderr, "silt: downloading the embedding model for local semantic search (~90 MB, one-time)...")
	}

	if env.stderrIsTerminal {
		pcfg.Progress = downloadProgress(stderr)
	}
	start := time.Now()
	res, err := pipeline.Run(ctx, idea, pcfg, func(msg string) { fmt.Fprintln(stderr, msg) })
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(stderr, "total: %.1fs\n", time.Since(start).Seconds())

	// The TUI needs a real terminal; when stdout is piped or redirected, fall
	// back to JSON.
	switch {
	case f.json:
	case !env.isTerminal:
		fmt.Fprintln(stderr, "note: stdout is not a terminal — emitting JSON (pass --json to silence this).")
	default:
		// The exit code is that of this verdict, whatever is searched for
		// in the TUI afterwards.
		tuiOpts.Idea, tuiOpts.Result = idea, &res
		return res.Verdict.Level.ExitCode(), env.runTUI(ctx, tuiOpts)
	}
	matches := res.Matches
	if matches == nil {
		matches = []model.Match{}
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // serde doesn't escape <, > and &
	if err := enc.Encode(output{SchemaVersion: schemaVersion, Query: idea, Verdict: res.Verdict, Matches: matches}); err != nil {
		return 0, err
	}
	return res.Verdict.Level.ExitCode(), nil
}

// whenSearched words a source's selection for --list-sources.
func whenSearched(sel sources.Selection) string {
	switch sel {
	case sources.Always:
		return "always"
	case sources.ByIdea:
		return "if the idea suggests it"
	case sources.ByIdeaOrFallback:
		return "if the idea suggests it, or suggests none"
	}
	return "only with --sources or --add"
}

// searcher runs the searches started in the TUI with the settings of the
// command line, --limit included.
func searcher(cfg pipeline.Config) tui.SearchFunc {
	return func(ctx context.Context, idea string, log func(string), progress rank.Progress) (pipeline.Result, error) {
		cfg := cfg
		cfg.Progress = progress
		return pipeline.Run(ctx, idea, cfg, log)
	}
}

// downloadProgress draws the download of each model file as one line on a
// terminal, updated in place whenever the percentage changes.
func downloadProgress(w io.Writer) rank.Progress {
	lastFile, lastPct := "", -1
	return func(file string, done, total int64) {
		pct := int(done * 100 / max(total, 1))
		if file == lastFile && pct == lastPct {
			return
		}
		lastFile, lastPct = file, pct
		fmt.Fprintf(w, "\r   %s: %3d%% of %.1f MB", file, pct, float64(total)/1e6)
		if done >= total {
			fmt.Fprintln(w)
		}
	}
}
