package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/miku/silt"
	"github.com/miku/silt/model"
)

const (
	nixpkgsBaseURL    = "https://search.nixos.org"
	nixpkgsChannel    = "unstable"
	maxNixpkgsResults = 20

	// The backend is not open to anonymous clients — it rejects
	// unauthenticated requests with a 401. These are the read-only
	// credentials that search.nixos.org itself ships to every browser that
	// loads the page (they are checked into the public NixOS/nixos-search
	// repository); they are not a secret and grant nothing beyond the same
	// public query access.
	nixpkgsUser     = "aWVSALXpZv"
	nixpkgsPassword = "X8gPHnzL52wFEekuxsfQ9cSh"

	// nixpkgsIndexGeneration is part of the index name, which upstream bumps
	// whenever the document mapping changes; the old index is then deleted
	// and requests for it 404. It has to be updated now and then, and the
	// live smoke test is what catches the rot.
	nixpkgsIndexGeneration = 50
)

// Nixpkgs searches the NixOS package index: the Elasticsearch backend behind
// search.nixos.org.
type Nixpkgs struct {
	Client  *http.Client
	BaseURL string
}

// NewNixpkgs returns an adapter for the live search.nixos.org.
func NewNixpkgs(client *http.Client) *Nixpkgs { return &Nixpkgs{Client: client, BaseURL: nixpkgsBaseURL} }

// ID implements Adapter.
func (*Nixpkgs) ID() model.Source { return model.Nixpkgs }

// nixpkgsQuery is the query DSL of the search.nixos.org frontend, which feeds
// it a one- or two-word search box. silt feeds it a whole keyword list, and
// the frontend's "operator": "and" — every term must match one package —
// made realistic multi-word ideas return nothing at all. It is "or" here
// deliberately: casting wide and letting ranking sink the loose hits is the
// same trade every other source makes. A reached source that is always empty
// is worse than a failing one: it feeds a falsely clean verdict instead of
// being surfaced as not reached.
func nixpkgsQuery(terms string, size int) map[string]any {
	return map[string]any{
		"from": 0,
		"size": size,
		"sort": []any{map[string]any{"_score": "desc", "package_attr_name": "desc"}},
		"query": map[string]any{
			"dis_max": map[string]any{
				"tie_breaker": 0.27,
				"queries": []any{map[string]any{
					"multi_match": map[string]any{
						"type":                                "cross_fields",
						"query":                               terms,
						"analyzer":                            "whitespace",
						"auto_generate_synonyms_phrase_query": false,
						"operator":                            "or",
						"fields": []string{
							"package_attr_name^9",
							"package_pname^6",
							"package_description^1.3",
							"package_longDescription^1",
						},
					},
				}},
			},
		},
	}
}

// Search implements Adapter.
func (a *Nixpkgs) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	query, err := json.Marshal(nixpkgsQuery(strings.Join(q.Keywords, " "), maxNixpkgsResults))
	if err != nil {
		return nil, &silt.HTTPError{Err: err}
	}
	url := fmt.Sprintf("%s/backend/latest-%d-nixos-%s/_search", a.BaseURL, nixpkgsIndexGeneration, nixpkgsChannel)
	req, err := newRequest(ctx, http.MethodPost, url, nil, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(nixpkgsUser, nixpkgsPassword)
	req.Header.Set("Content-Type", "application/json")
	resp, err := send(a.Client, req)
	if err != nil {
		return nil, err
	}
	var body struct {
		Hits *struct {
			Hits *[]struct {
				Source *struct {
					AttrName    *string  `json:"package_attr_name"`
					PName       *string  `json:"package_pname"`
					Description string   `json:"package_description"`
					Homepage    []string `json:"package_homepage"`
					PVersion    string   `json:"package_pversion"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := decodeJSON(resp, &body); err != nil {
		return nil, err
	}
	switch {
	case body.Hits == nil:
		return nil, missingField("hits")
	case body.Hits.Hits == nil:
		return nil, missingField("hits.hits")
	}
	var matches []model.Match
	for _, hit := range *body.Hits.Hits {
		switch {
		case hit.Source == nil:
			return nil, missingField("_source")
		case hit.Source.AttrName == nil:
			return nil, missingField("package_attr_name")
		}
		pkg := hit.Source
		name := *pkg.AttrName
		if pkg.PName != nil {
			name = *pkg.PName
		}
		url := fmt.Sprintf("https://search.nixos.org/packages?channel=%s&show=%s&query=%s", nixpkgsChannel, *pkg.AttrName, name)
		if len(pkg.Homepage) > 0 && pkg.Homepage[0] != "" {
			url = pkg.Homepage[0]
		}
		if pkg.PVersion != "" {
			name += " (v" + pkg.PVersion + ")"
		}
		matches = append(matches, model.Match{Name: name, Source: model.Nixpkgs, URL: url, Description: pkg.Description})
	}
	return matches, nil
}
