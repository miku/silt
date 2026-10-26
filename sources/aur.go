package sources

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/miku/silt"
	"github.com/miku/silt/freshness"
	"github.com/miku/silt/internal/textutil"
	"github.com/miku/silt/model"
)

const (
	aurBaseURL = "https://aur.archlinux.org"
	// aurByNameDesc matches the argument against package names and
	// descriptions: the RPC's default, and by far the better recall —
	// "process" finds 1503 packages this way against 78 by name alone — but
	// broad terms can exceed the server's result ceiling.
	aurByNameDesc = "name-desc"
	// aurByName matches package names only. Narrow enough to stay under the
	// ceiling where aurByNameDesc is refused ("port" and "file" are both
	// refused there, and return 636 and 716 hits here), so it serves as a
	// fallback and never as the primary mode.
	aurByName = "name"
	// aurMaxHits is the most packages one narrowing step contributes. A
	// single broad keyword matches hundreds of AUR packages; they arrive
	// unranked, so the tail past the most voted-for handful is noise.
	aurMaxHits = 20
)

// AUR searches the Arch User Repository through its RPC: GET /rpc/?v=5.
type AUR struct {
	Client  *http.Client
	BaseURL string
}

// NewAUR returns an adapter for the live AUR.
func NewAUR(client *http.Client) *AUR { return &AUR{Client: client, BaseURL: aurBaseURL} }

// ID implements Adapter.
func (*AUR) ID() model.Source { return model.AUR }

type aurPackage struct {
	Name        *string `json:"Name"`
	Description string  `json:"Description"`
	NumVotes    *uint64 `json:"NumVotes"`
	// Last change to the package's AUR entry, in epoch seconds.
	LastModified freshness.Lenient[int64] `json:"LastModified"`
}

// rpc makes one RPC round trip. refused means the RPC refused arg as
// matching too much to return, so the caller can retry it in a narrower
// match mode. Any other RPC-level error is an UnavailableError, so that the
// source is reported as not reached instead of passing an empty result off
// as a clean search.
func (a *AUR) rpc(ctx context.Context, arg, by string) (hits []aurPackage, refused bool, err error) {
	var body struct {
		Type    string       `json:"type"`
		Error   *string      `json:"error"`
		Results []aurPackage `json:"results"`
	}
	params := queryParams{{"v", "5"}, {"type", "search"}, {"by", by}, {"arg", arg}}
	if err := getJSON(ctx, a.Client, a.BaseURL+"/rpc/", params, &body); err != nil {
		return nil, false, err
	}
	for _, p := range body.Results {
		if p.Name == nil {
			return nil, false, missingField("Name")
		}
	}
	if body.Type == "error" {
		reason := "AUR RPC returned an error"
		if body.Error != nil {
			reason = *body.Error
		}
		if strings.Contains(textutil.ToLower(reason), "too many") {
			return nil, true, nil
		}
		return nil, false, &silt.UnavailableError{Msg: "AUR search: " + reason}
	}
	return body.Results, false, nil
}

// Search implements Adapter.
func (a *AUR) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// The RPC matches arg as one string against a single package, so a
	// multi-term idea matches nothing: measured live, a five-term idea,
	// "spell syntax mistakes" and "syntax mistakes" all return zero while
	// "mistakes" returns 13. Like Maven and NuGet this one narrows all the
	// way down to a single keyword; the noise that lets in is dropped by
	// similarity ranking.
	var matches []model.Match
	for _, term := range narrowingCandidates(q, 1) {
		// Narrowing and the result ceiling pull against each other — each
		// step is broader than the last, and broad is what the RPC refuses —
		// so the fallback to name-only matching belongs inside the step.
		hits, refused, err := a.rpc(ctx, term, aurByNameDesc)
		if err == nil && refused {
			hits, refused, err = a.rpc(ctx, term, aurByName)
		}
		switch {
		case err != nil:
			return nil, err
		case refused:
			return nil, &silt.UnavailableError{Msg: "AUR search: query matched too many packages to return"}
		}
		matches = a.toMatches(hits)
		if len(matches) > 0 {
			break
		}
	}
	return matches, nil
}

// toMatches keeps the aurMaxHits most voted-for packages.
func (a *AUR) toMatches(hits []aurPackage) []model.Match {
	votes := func(p aurPackage) uint64 {
		if p.NumVotes == nil {
			return 0
		}
		return *p.NumVotes
	}
	slices.SortStableFunc(hits, func(x, y aurPackage) int { return cmp.Compare(votes(y), votes(x)) })
	var matches []model.Match
	for _, p := range hits[:min(aurMaxHits, len(hits))] {
		m := model.Match{
			Name:        *p.Name,
			Source:      model.AUR,
			URL:         a.BaseURL + "/packages/" + *p.Name,
			Description: p.Description,
		}
		// NumVotes is the AUR's "how established is this" signal; no votes
		// carry no signal.
		if votes(p) > 0 {
			m.Popularity = p.NumVotes
		}
		if p.LastModified.OK {
			if s, ok := freshness.FromUnixSecs(p.LastModified.V); ok {
				m.LastUpdated = &s
			}
		}
		matches = append(matches, m)
	}
	return matches
}
