// Command silt is a prior-art search for your code ideas: has this dev tool
// already been shipped?
//
//	silt "interactive cli to kill whatever's on a port"
//	silt --keyword-only --json "react component for infinite scroll"
//	silt --llm-verdict "kubernetes log viewer with live filtering"
//
// The verdict comes from the similarity data unless --llm-verdict asks an
// LLM for it.
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

	"github.com/miku/silt"
	"github.com/miku/silt/internal/browser"
	"github.com/miku/silt/internal/config"
	"github.com/miku/silt/internal/pipeline"
	"github.com/miku/silt/internal/tui"
	"github.com/miku/silt/llm/ollama"
	"github.com/miku/silt/model"
	"github.com/miku/silt/rank"
	"github.com/miku/silt/sources"
	"github.com/miku/silt/verdict"
)

func main() {
	env := environment{
		stdout:           os.Stdout,
		stderr:           os.Stderr,
		isTerminal:       term.IsTerminal(int(os.Stdout.Fd())),
		stderrIsTerminal: term.IsTerminal(int(os.Stderr.Fd())),
		lookupEnv:        os.LookupEnv,
		loadConfig:       config.Load,
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
	loadConfig                   func() (config.Config, error)
	runTUI                       func(context.Context, tui.Options) error
}

// Errors shown to users verbatim, as patent words them.
//
//nolint:staticcheck // ST1005: capitalized and punctuated on purpose.
var (
	errJSONNeedsIdea       = errors.New("--json requires an idea argument.")
	errNoIdea              = errors.New(`No idea provided. Usage: silt "your dev-tool idea here"`)
	errAPIBaseWithoutModel = errors.New("api_base is set but no model was specified. " +
		"Pass --model <NAME>, set SILT_MODEL, or add `model = \"...\"` to " +
		"your silt config file (its location is shown in the README).")
	errFastWithLLMVerdict = usageError{errors.New("--fast and --llm-verdict can't be used together")}
)

// usageError is a problem with the arguments themselves. As with clap, these
// exit 2, while errors at run time exit 1.
type usageError struct{ error }

// flags holds the parsed command line.
type flags struct {
	limit                                limitValue
	model, apiBase, apiKey               string
	keywordOnly, llmVerdict, printPrompt bool
	// fast is the default now; the flag is still accepted.
	fast                      bool
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
	fs.BoolVar(&f.llmVerdict, "llm-verdict", false, "Ask an LLM for the verdict, with a headline and gaps in its own words,\ninstead of estimating it from the similarity data. Takes seconds more")
	fs.StringVar(&f.model, "model", "", "LLM model for --llm-verdict. Defaults to qwen3.5 for Ollama; required with\n--api-base. Can also be set via SILT_MODEL or config file")
	fs.StringVar(&f.apiBase, "api-base", "", "Use an OpenAI-compatible API instead of local Ollama for --llm-verdict.\nBase URL ending in /v1, e.g. http://localhost:1234/v1. Can also be set via\nSILT_API_BASE or config file")
	fs.StringVar(&f.apiKey, "api-key", "", "API key for --api-base. Falls back to SILT_API_KEY, then the config file.\nOmit for servers without auth")
	fs.BoolVar(&f.keywordOnly, "keyword-only", false, "Skip the embedding model and rank by keyword overlap instead.\nUseful offline or to avoid the ~80 MB model download")
	fs.BoolVar(&f.fast, "fast", false, "Without an LLM: the default now")
	_ = fs.MarkHidden("fast")
	fs.Var(&f.sources, "sources", "Only search these sources (comma-separated, e.g. \"npm,crates-io,github\").\nOverrides the automatic source detection. See --list-sources for valid names")
	fs.Var(&f.add, "add", "Also search these sources, on top of the ones picked from the idea\n(comma-separated). Codeberg is searched only when asked for, here or with\n--sources: its search takes 5-15 s")
	fs.Var(&f.exclude, "exclude", "Exclude these sources from the search (comma-separated)")
	fs.Var(&f.cacheTTL, "cache-ttl", "How long a source's results are reused for the same idea, e.g. 30m or 24h;\n0 turns the cache off. Failed searches are never cached. Can also be set\nvia SILT_CACHE_TTL")
	fs.Var(&f.catalogTTL, "catalog-ttl", "How long downloaded catalogs (Homebrew's, PyPI's project names) are kept,\ne.g. 6h or 168h; 0 downloads them on every run. Can also be set via\nSILT_CATALOG_TTL")
	fs.BoolVar(&f.printPrompt, "print-prompt", false, "Print the verdict prompt to stdout and exit (for BYO-agent workflows)")
	fs.BoolVar(&f.listSources, "list-sources", false, "List all available source names and exit")
	fs.BoolVar(&f.checkSources, "check-sources", false, "Search each source once, without the caches, print whether it was reached,\nwalled, rate-limited or timed out, and how long it took, and exit.\n--sources, --add and --exclude choose the sources")
	fs.BoolVar(&f.json, "json", false, "Print structured JSON instead of launching the TUI")
	fs.StringVar(&f.completions, "completions", "", "Generate shell completions and exit ("+strings.Join(shells, ", ")+")")
	fs.BoolP("help", "h", false, "Print help")
	fs.BoolP("version", "V", false, "Print version")
}

// flagOrEnv returns the value of the flag name if it was given, else that of
// the environment variable env if it is set (even if empty), else nil.
func flagOrEnv(cmd *cobra.Command, name, value, env string, lookupEnv func(string) (string, bool)) *string {
	if cmd.Flags().Changed(name) {
		return &value
	}
	if v, ok := lookupEnv(env); ok {
		return &v
	}
	return nil
}

// or returns the first non-nil value.
func or(vs ...*string) *string {
	for _, v := range vs {
		if v != nil {
			return v
		}
	}
	return nil
}

// backend is the resolved LLM backend configuration.
type backend struct {
	apiBase, apiKey, model *string
	// ollamaEndpoint is the daemon OLLAMA_HOST names, if it is set.
	ollamaEndpoint *string
}

// resolveBackend applies the precedence CLI flag / env var > config file >
// built-in default. OPENAI_API_KEY is not read: a key meant for OpenAI is
// never sent to whatever server api_base names. The Ollama daemon is the one
// OLLAMA_HOST names, as for the ollama CLI. It returns the warnings and notes
// to print, or an error if the settings can't work. Without llm, no LLM is
// asked, and the settings are neither checked nor warned about: the
// environment and the config file may well hold them for the runs with
// --llm-verdict.
func resolveBackend(flagBase, flagKey, flagModel *string, llm bool, cfg config.Config, lookupEnv func(string) (string, bool)) (backend, []string, error) {
	b := backend{
		apiBase: or(flagBase, cfg.APIBase),
		apiKey:  or(flagKey, cfg.APIKey),
		model:   or(flagModel, cfg.Model),
	}
	if v, ok := lookupEnv("OLLAMA_HOST"); ok {
		endpoint := ollama.Endpoint(v)
		b.ollamaEndpoint = &endpoint
	}
	if !llm {
		return b, nil, nil
	}
	if b.apiBase != nil && b.model == nil {
		return backend{}, nil, errAPIBaseWithoutModel
	}
	var notes []string
	if b.apiKey != nil && b.apiBase == nil {
		notes = append(notes, "warning: api_key has no effect without api_base; using local Ollama.")
	}
	if b.apiBase != nil {
		notes = append(notes, fmt.Sprintf("note: --api-base sends your search query to %s to generate the verdict; "+
			"the default local Ollama backend and the verdict without --llm-verdict keep it on your machine.", *b.apiBase))
	}
	return b, notes, nil
}

// ignoredLLMFlags warns about the LLM flags given on the command line when
// no LLM is asked: likely a run that meant --llm-verdict.
func ignoredLLMFlags(cmd *cobra.Command, llm bool) []string {
	if llm {
		return nil
	}
	var given []string
	for _, name := range []string{"model", "api-base", "api-key"} {
		if cmd.Flags().Changed(name) {
			given = append(given, "--"+name)
		}
	}
	verb := "apply"
	switch len(given) {
	case 0:
		return nil
	case 1:
		verb = "applies"
	}
	return []string{fmt.Sprintf("warning: %s only %s with --llm-verdict; the verdict comes from the similarity data.",
		strings.Join(given, ", "), verb)}
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
			fmt.Fprintf(stdout, "%-22s %s\n", s.KebabName(), s)
		}
		return 0, nil
	}

	if f.fast && f.llmVerdict {
		return 0, errFastWithLLMVerdict
	}
	if f.fast {
		fmt.Fprintln(stderr, "note: --fast is the default now; --llm-verdict asks an LLM for the verdict.")
	}
	// --print-prompt prints the prompt for an LLM rather than asking one.
	llm := f.llmVerdict && !f.printPrompt

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

	// Load the config and resolve the backend before the no-idea check, so
	// that the interactive TUI gets the configured LLM backend too.
	cfg, err := env.loadConfig()
	if err != nil {
		return 0, err
	}
	b, notes, err := resolveBackend(
		flagOrEnv(cmd, "api-base", f.apiBase, "SILT_API_BASE", env.lookupEnv),
		flagOrEnv(cmd, "api-key", f.apiKey, "SILT_API_KEY", env.lookupEnv),
		flagOrEnv(cmd, "model", f.model, "SILT_MODEL", env.lookupEnv),
		llm, cfg, env.lookupEnv)
	if err != nil {
		return 0, err
	}
	if !f.printPrompt {
		notes = append(notes, ignoredLLMFlags(cmd, llm)...)
	}
	for _, n := range notes {
		fmt.Fprintln(stderr, n)
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
		APIBase:        b.apiBase,
		APIKey:         b.apiKey,
		Model:          b.model,
		OllamaEndpoint: b.ollamaEndpoint,
		LLMVerdict:     llm,
		KeywordOnly:    f.keywordOnly,
		Limit:          int(f.limit),
		Include:        include,
		Add:            add,
		Exclude:        exclude,
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
	tuiOpts := tui.Options{Search: searcher(pcfg), Backend: backendNote(b, pcfg.LLMVerdict)}

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

	q := pipeline.BuildQuery(idea)
	fmt.Fprintf(stderr, "Searching for prior art: \"%s\"\n", idea)
	fmt.Fprintf(stderr, "   keywords: %s\n", strings.Join(q.Keywords, ", "))

	if !f.keywordOnly && !rank.ModelIsCached() {
		fmt.Fprintln(stderr, "silt: downloading the embedding model for local semantic search (~80 MB, one-time)...")
	}

	if env.stderrIsTerminal {
		pcfg.Progress = downloadProgress(stderr)
	}
	start := time.Now()
	res, err := pipeline.Run(ctx, idea, pcfg, func(msg string) { fmt.Fprintln(stderr, msg) })
	if err != nil {
		return 0, err
	}

	if f.printPrompt {
		fmt.Fprint(stdout, verdict.BuildPrompt(q, res.Matches, res.Verdict.SourcesChecked))
		return 0, nil
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
	if err := enc.Encode(output{SchemaVersion: 1, Query: idea, Verdict: res.Verdict, Matches: matches}); err != nil {
		return 0, err
	}
	return res.Verdict.Level.ExitCode(), nil
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

// backendNote says how the TUI's searches get their verdict.
func backendNote(b backend, llm bool) string {
	model := ollama.DefaultModel
	if b.model != nil {
		model = *b.model
	}
	switch {
	case !llm:
		return "Verdicts are estimated from the similarity data; --llm-verdict asks an LLM."
	case b.apiBase != nil:
		return fmt.Sprintf("Verdicts by %s, through the API at %s.", model, *b.apiBase)
	case b.ollamaEndpoint != nil:
		return fmt.Sprintf("Verdicts by %s on the Ollama at %s.", model, *b.ollamaEndpoint)
	}
	return fmt.Sprintf("Verdicts by %s on the local Ollama.", model)
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
