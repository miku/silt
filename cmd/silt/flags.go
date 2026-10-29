package main

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/miku/silt/internal/pipeline"
	"github.com/miku/silt/model"
)

// limitValue is --limit: a uint32 of at least 1, as clap's
// value_parser!(u32).range(1..) checks it.
type limitValue uint32

func (l *limitValue) String() string { return strconv.FormatUint(uint64(*l), 10) }
func (l *limitValue) Type() string   { return "LIMIT" }

func (l *limitValue) Set(s string) error {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid digit found in string")
	}
	if n < 1 {
		return fmt.Errorf("%d is not in 1..=%d", n, uint32(math.MaxUint32))
	}
	*l = limitValue(n)
	return nil
}

// ttlValue is --cache-ttl: a duration such as 30m or 24h; 0 turns the cache
// off.
type ttlValue time.Duration

func (d *ttlValue) String() string { return pipeline.ShortDuration(time.Duration(*d)) }
func (d *ttlValue) Type() string   { return "DURATION" }

func (d *ttlValue) Set(s string) error {
	v, err := parseTTL(s)
	if err != nil {
		return err
	}
	*d = ttlValue(v)
	return nil
}

// parseTTL parses a cache TTL: a Go duration that isn't negative.
func parseTTL(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("%s is negative; 0 turns the cache off", s)
	}
	return d, nil
}

// listValue is a comma-separated list flag that may be repeated. Unlike
// pflag's StringSlice it splits on every comma and keeps empty items, as
// clap's value_delimiter does, so --sources "" is an unknown source rather
// than no sources.
type listValue []string

func (l *listValue) String() string { return strings.Join(*l, ",") }
func (l *listValue) Type() string   { return "LIST" }

func (l *listValue) Set(s string) error {
	*l = append(*l, strings.Split(s, ",")...)
	return nil
}

// shells are the shells --completions supports. clap also generates elvish;
// cobra can't.
var shells = []string{"bash", "fish", "powershell", "zsh"}

func generateCompletions(cmd *cobra.Command, shell string, w io.Writer) error {
	root := cmd.Root()
	switch shell {
	case "bash":
		return root.GenBashCompletionV2(w, true)
	case "fish":
		return root.GenFishCompletion(w, true)
	case "powershell":
		return root.GenPowerShellCompletionWithDesc(w)
	case "zsh":
		return root.GenZshCompletion(w)
	}
	return usageError{fmt.Errorf("invalid value '%s' for '--completions <SHELL>'\n  [possible values: %s]",
		shell, strings.Join(shells, ", "))}
}

// registerCompletions completes the values of the flags that take a fixed
// set: shells, and source names.
func registerCompletions(cmd *cobra.Command) {
	sourceNames := func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		var names []string
		for _, s := range model.All() {
			names = append(names, s.KebabName()+"\t"+s.String())
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
	_ = cmd.RegisterFlagCompletionFunc("sources", sourceNames)
	_ = cmd.RegisterFlagCompletionFunc("exclude", sourceNames)
	_ = cmd.RegisterFlagCompletionFunc("completions", cobra.FixedCompletions(shells, cobra.ShellCompDirectiveNoFileComp))
}
