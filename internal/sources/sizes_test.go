package sources

import (
	"testing"

	"github.com/miku/silt/internal/model"
)

// TestEverySourceButTheForgesAndHackerNewsHasASize keeps the table complete:
// a package source added without a size would be left out of the progress
// screen's count without a word.
func TestEverySourceButTheForgesAndHackerNewsHasASize(t *testing.T) {
	unsized := map[model.Source]bool{model.GitHub: true, model.HackerNews: true, model.Codeberg: true}
	for _, s := range model.All() {
		n, ok := Size(s)
		if ok == unsized[s] {
			t.Errorf("Size(%v) = %d, %v", s, n, ok)
		}
		if ok && n <= 0 {
			t.Errorf("Size(%v) = %d", s, n)
		}
	}
}
