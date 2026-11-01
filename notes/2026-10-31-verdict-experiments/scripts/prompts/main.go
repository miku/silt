// Command prompts writes silt's verdict prompt for every recorded idea, so
// that the LLM baseline of E2 is asked exactly what silt would ask.
//
//	go run ./scripts/prompts < data/runs.jsonl > data/prompts.jsonl
//
// Run from the experiment directory (`make prompts`). The prompt is
// verdict.BuildPrompt over the matches and sources of the embedding run.
// BuildPrompt renders the age of a match relative to the current time, so
// the prompts are written once and kept: every LLM run reads the same text.
// Rejected ideas have no matches and get no prompt.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/miku/silt/model"
	"github.com/miku/silt/verdict"
)

type record struct {
	ID         string `json:"id"`
	Idea       string `json:"idea"`
	Error      string `json:"error"`
	Embeddings *struct {
		Verdict struct {
			SourcesChecked []model.Source `json:"sources_checked"`
		} `json:"verdict"`
		Matches []model.Match `json:"matches"`
	} `json:"embeddings"`
}

type prompt struct {
	ID      string `json:"id"`
	Built   string `json:"built"`
	Matches int    `json:"matches"`
	Prompt  string `json:"prompt"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "prompts:", err)
		os.Exit(1)
	}
}

func run() error {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	built := time.Now().UTC().Format(time.RFC3339)
	for sc.Scan() {
		var r record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return err
		}
		if r.Error != "" || r.Embeddings == nil {
			continue
		}
		q := model.Query{Idea: r.Idea}
		p := verdict.BuildPrompt(q, r.Embeddings.Matches, r.Embeddings.Verdict.SourcesChecked)
		if err := enc.Encode(prompt{ID: r.ID, Built: built, Matches: len(r.Embeddings.Matches), Prompt: p}); err != nil {
			return err
		}
	}
	return sc.Err()
}
