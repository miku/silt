package sources

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"unicode"

	"github.com/miku/silt/internal/textutil"
	"github.com/miku/silt/model"
)

const (
	hackageBaseURL    = "https://hackage.haskell.org"
	maxHackageResults = 20
	// maxCabalFetches is how many .cabal files are fetched at once. patent
	// fetches all 20 at once; 4 spares a server run by volunteers, for about
	// a second more per search (1.7–2.1 s instead of 0.9–1.1 s, measured).
	maxCabalFetches = 4
)

// Hackage searches the Hackage package archive (Haskell). Its search,
// /packages/search, returns names only, so the synopsis and homepage of each
// result come from its .cabal file, fetched in parallel, maxCabalFetches at a
// time.
//
// Hackage sends a last-modified header for the .cabal file, but trustee
// bound fixes bump it without a release, so it is not a last-updated date and
// Hackage matches have none.
type Hackage struct {
	Client  *http.Client
	BaseURL string
}

// NewHackage returns an adapter for the live Hackage.
func NewHackage(client *http.Client) *Hackage {
	return &Hackage{Client: client, BaseURL: hackageBaseURL}
}

// ID implements Adapter.
func (*Hackage) ID() model.Source { return model.Hackage }

// Search implements Adapter.
func (a *Hackage) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	req, err := newRequest(ctx, http.MethodGet, a.BaseURL+"/packages/search", queryParams{{"terms", strings.Join(q.Keywords, " ")}}, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := send(a.Client, req)
	if err != nil {
		return nil, err
	}
	var results []struct {
		Name *string `json:"name"`
	}
	if err := decodeJSON(resp, &results); err != nil {
		return nil, err
	}
	if results == nil {
		return nil, missingField("results") // a JSON null, not a list
	}
	var names []string
	for _, r := range results {
		if r.Name == nil {
			return nil, missingField("name")
		}
		if len(names) < maxHackageResults {
			names = append(names, *r.Name)
		}
	}

	matches := make([]model.Match, len(names))
	var wg sync.WaitGroup
	fetching := make(chan struct{}, maxCabalFetches)
	for i, name := range names {
		wg.Go(func() {
			fetching <- struct{}{}
			defer func() { <-fetching }()
			// A .cabal file that can't be fetched only costs the match its
			// description and homepage.
			cabal := a.fetchCabal(ctx, name)
			url := cabalField(cabal, "homepage")
			if url == "" {
				url = a.BaseURL + "/package/" + name
			}
			matches[i] = model.Match{
				Name:        name,
				Source:      model.Hackage,
				URL:         url,
				Description: cabalField(cabal, "synopsis"),
			}
		})
	}
	wg.Wait()
	return matches, nil
}

// fetchCabal returns the package's .cabal file, or "" if it can't be
// fetched. Like patent, it reads the body whatever the status: an error page
// simply has no fields.
func (a *Hackage) fetchCabal(ctx context.Context, name string) string {
	resp, err := get(ctx, a.Client, a.BaseURL+"/package/"+name+"/"+name+".cabal", nil)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return string(b)
}

// cabalField returns the first non-empty value of a line whose name starts
// with field, in any case.
func cabalField(cabal, field string) string {
	for _, line := range strings.Split(cabal, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if !strings.HasPrefix(strings.TrimLeftFunc(textutil.ToLower(line), unicode.IsSpace), field) {
			continue
		}
		if _, value, ok := strings.Cut(line, ":"); ok {
			if v := strings.TrimSpace(value); v != "" {
				return v
			}
		}
	}
	return ""
}
