package verdict

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/miku/silt"
	"github.com/miku/silt/internal/patenttest"
	"github.com/miku/silt/model"
)

// TestPromptMatchesPatentByteForByte compares the prompt with patent's own
// build_prompt for the same input, apart from the program name.
func TestPromptMatchesPatentByteForByte(t *testing.T) {
	f := patenttest.Load(t)
	want := strings.ReplaceAll(string(patenttest.Read(t, "prompt.txt")),
		"idea — patent searches", "idea — silt searches")
	got := buildPrompt(f.Query(), f.Matches, f.SourcesChecked, patenttest.Now)
	if got == want {
		return
	}
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range max(len(gotLines), len(wantLines)) {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			t.Fatalf("line %d differs:\n got: %q\nwant: %q", i+1, g, w)
		}
	}
}

// TestFromDataMatchesPatent compares FromData with patent's --fast verdict.
// silt words the headline as a belief; patent's wording, which the guards on
// the LLM's verdict still use, is compared in its place.
func TestFromDataMatchesPatent(t *testing.T) {
	f := patenttest.Load(t)
	v := FromData(f.Matches, f.SourcesChecked, f.SourcesFailed)
	v.Headline = dataHeadline(v.Level, f.Matches)
	patenttest.JSONEqual(t, "from-data.json", v)
}

// canned is an LLM that answers every prompt with the same response.
type canned string

func (c canned) Generate(context.Context, string) (string, error) { return string(c), nil }
func (canned) Label() string                                      { return "canned" }

// TestAssessMatchesPatent runs Assess on the model responses in
// assess-cases.json and compares each verdict, or error, with what patent's
// verdict::assess returned for it. serde_json and encoding/json word syntax
// errors differently, so for those (patent's end in "at line N column M")
// only the kind of error is compared.
func TestAssessMatchesPatent(t *testing.T) {
	var (
		in struct {
			SourcesChecked []model.Source           `json:"sources_checked"`
			SourcesFailed  []model.Source           `json:"sources_failed"`
			MatchSets      map[string][]model.Match `json:"match_sets"`
			Cases          []struct {
				Name, Matches, Response string
			}
		}
		want []struct {
			Name    string
			Verdict json.RawMessage
			Error   *string
		}
	)
	patenttest.Decode(t, "assess-cases.json", &in)
	patenttest.Decode(t, "assess.json", &want)
	if len(want) != len(in.Cases) {
		t.Fatalf("%d cases, %d outcomes recorded", len(in.Cases), len(want))
	}
	serdeSyntax := regexp.MustCompile(` at line \d+ column \d+$`)
	for i, c := range in.Cases {
		w := want[i]
		v, err := Assess(t.Context(), canned(c.Response), query(), in.MatchSets[c.Matches], in.SourcesChecked, in.SourcesFailed)
		switch {
		case w.Error != nil && serdeSyntax.MatchString(*w.Error):
			if !errors.As(err, new(*silt.ParseError)) {
				t.Errorf("%s: got %+v, %v; want a parse error like patent's %q", c.Name, v, err, *w.Error)
			}
		case w.Error != nil:
			if err == nil || err.Error() != *w.Error {
				t.Errorf("%s: got %+v, %v; want error %q", c.Name, v, err, *w.Error)
			}
		case err != nil:
			t.Errorf("%s: %v; want verdict %s", c.Name, err, w.Verdict)
		default:
			var have, wantV any
			b, _ := json.Marshal(v)
			if json.Unmarshal(b, &have) != nil || json.Unmarshal(w.Verdict, &wantV) != nil || !reflect.DeepEqual(have, wantV) {
				t.Errorf("%s:\n got: %s\nwant: %s", c.Name, b, w.Verdict)
			}
		}
	}
}
