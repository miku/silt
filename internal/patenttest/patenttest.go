// Package patenttest loads the reference data in testdata/patent: a fixture
// and what patent's own library functions return for it, recorded with a
// small Rust harness that links the patent crate (0.14.1 @ 8d741bd).
//
//	fixture.json        the input: an idea, keywords, matches, sources
//	from-data.json      verdict::from_data
//	rank-keywords.json  rank::rank_by_keywords with limit 50
package patenttest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/miku/silt/internal/model"
)

// Fixture is the recorded input.
type Fixture struct {
	Idea           string         `json:"idea"`
	Keywords       []string       `json:"keywords"`
	Matches        []model.Match  `json:"matches"`
	SourcesChecked []model.Source `json:"sources_checked"`
	SourcesFailed  []model.Source `json:"sources_failed"`
}

// Query returns the fixture's query.
func (f Fixture) Query() model.Query { return model.Query{Idea: f.Idea, Keywords: f.Keywords} }

// Path returns the path of a file in testdata/patent.
func Path(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "patent", name)
}

// Read returns the contents of a file in testdata/patent.
func Read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(Path(name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Decode unmarshals the JSON file name in testdata/patent into v.
func Decode(t *testing.T, name string, v any) {
	t.Helper()
	if err := json.Unmarshal(Read(t, name), v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// Load decodes fixture.json.
func Load(t *testing.T) Fixture {
	t.Helper()
	var f Fixture
	Decode(t, "fixture.json", &f)
	return f
}

// JSONEqual reports whether got, once marshaled, is semantically equal to the
// recorded JSON in the file name. Bytes differ for equal data: serde writes
// 0.0 where encoding/json writes 0.
func JSONEqual(t *testing.T, name string, got any) {
	t.Helper()
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var have, want any
	if err := json.Unmarshal(b, &have); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(Read(t, name), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(have, want) {
		t.Errorf("JSON differs from patent's %s:\n got: %s\nwant: %s", name, b, Read(t, name))
	}
}
