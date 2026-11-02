package verdict

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/miku/silt/internal/model"
	"github.com/miku/silt/internal/patenttest"
)

// TestFromDataMatchesPatent compares FromData with patent's from_data, the
// verdict of its --fast mode, for the same matches: the level, the sources,
// the caveat and the number of closely-related tools are patent's. silt
// words the headline as an estimate, and has no gaps, which only patent's
// LLM writes.
func TestFromDataMatchesPatent(t *testing.T) {
	f := patenttest.Load(t)
	var want struct {
		Level          model.Saturation `json:"level"`
		Headline       string           `json:"headline"`
		SourcesChecked []model.Source   `json:"sources_checked"`
		SourcesFailed  []model.Source   `json:"sources_failed"`
		Caveat         string           `json:"caveat"`
	}
	patenttest.Decode(t, "from-data.json", &want)
	got := FromData(f.Matches, f.SourcesChecked, f.SourcesFailed)
	if got.Level != want.Level || got.Caveat != want.Caveat ||
		!reflect.DeepEqual(got.SourcesChecked, want.SourcesChecked) || !reflect.DeepEqual(got.SourcesFailed, want.SourcesFailed) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	count := regexp.MustCompile(`\d+ closely-related tools?`).FindString(want.Headline)
	if count == "" || !strings.Contains(got.Headline, count) {
		t.Errorf("headline %q doesn't count %q, as patent's %q does", got.Headline, count, want.Headline)
	}
}
