package rank

import (
	"testing"

	"github.com/miku/silt/internal/patenttest"
)

func TestByKeywordsMatchesPatent(t *testing.T) {
	f := patenttest.Load(t)
	patenttest.JSONEqual(t, "rank-keywords.json", ByKeywords(f.Query(), f.Matches, DefaultLimit))
}
