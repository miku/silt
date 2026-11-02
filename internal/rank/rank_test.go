package rank

import (
	"testing"

	"github.com/miku/silt/internal/model"
)

func m(name, desc string) model.Match {
	return model.Match{Name: name, Source: model.CratesIO, URL: "https://example.com/" + name, Description: desc}
}

func names(ms []model.Match) []string {
	var out []string
	for _, x := range ms {
		out = append(out, x.Name)
	}
	return out
}

func TestByKeywordsScoresTheFractionOfKeywordsFound(t *testing.T) {
	q := model.Query{Idea: "kill process on port", Keywords: []string{"kill", "process", "port"}}
	ranked := ByKeywords(q, []model.Match{
		m("recipes", "baking bread"),
		m("fkill", "Fabulously kill processes"),
		m("kill-port", "Kill the process on a PORT"),
	}, 10)
	if got := names(ranked); len(got) != 2 || got[0] != "kill-port" || got[1] != "fkill" {
		t.Fatalf("ranked = %v; want kill-port, fkill (zero scores dropped)", got)
	}
	if ranked[0].Similarity != 1 {
		t.Errorf("kill-port similarity = %v, want 1", ranked[0].Similarity)
	}
	// Substring matching: "process" is found in "processes".
	if want := float32(2) / 3; ranked[1].Similarity != want {
		t.Errorf("fkill similarity = %v, want %v", ranked[1].Similarity, want)
	}
}

func TestByKeywordsSortsStably(t *testing.T) {
	q := model.Query{Keywords: []string{"cli"}}
	ranked := ByKeywords(q, []model.Match{m("a", "cli"), m("b", "cli"), m("c", "cli")}, 10)
	if got := names(ranked); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("equal scores must keep their order: %v", got)
	}
}

func TestByKeywordsTruncatesToLimit(t *testing.T) {
	q := model.Query{Keywords: []string{"cli"}}
	if got := ByKeywords(q, []model.Match{m("a", "cli"), m("b", "cli"), m("c", "cli")}, 2); len(got) != 2 {
		t.Errorf("got %d matches, want 2", len(got))
	}
}

func TestByKeywordsWithoutKeywordsKeepsOrderUnscored(t *testing.T) {
	ranked := ByKeywords(model.Query{Idea: "x"}, []model.Match{m("b", ""), m("a", ""), m("c", "")}, 2)
	if got := names(ranked); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Errorf("ranked = %v", got)
	}
	if ranked[0].Similarity != 0 {
		t.Errorf("similarity = %v, want 0", ranked[0].Similarity)
	}
}

func TestByKeywordsOfNothingIsEmpty(t *testing.T) {
	if got := ByKeywords(model.Query{Keywords: []string{"x"}}, nil, 10); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}
