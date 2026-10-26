package verdict

import (
	"strings"
	"testing"

	"github.com/miku/silt/internal/patenttest"
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

func TestFromDataMatchesPatent(t *testing.T) {
	f := patenttest.Load(t)
	patenttest.JSONEqual(t, "from-data.json", FromData(f.Matches, f.SourcesChecked, f.SourcesFailed))
}
