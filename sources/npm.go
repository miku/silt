package sources

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"unicode/utf8"

	"github.com/miku/silt/freshness"
	"github.com/miku/silt/model"
)

const npmBaseURL = "https://registry.npmjs.org"

// Npm searches the npm registry: GET /-/v1/search?text=.
type Npm struct {
	Client  *http.Client
	BaseURL string
}

// NewNpm returns an adapter for the live npm registry.
func NewNpm(client *http.Client) *Npm { return &Npm{Client: client, BaseURL: npmBaseURL} }

// ID implements Adapter.
func (*Npm) ID() model.Source { return model.Npm }

// npmMaxText: npm rejects a text outside 2-64 characters with
// 400 ERR_TEXT_LENGTH.
const npmMaxText = 64

// npmSearchText is the idea verbatim when it fits npm's limit; otherwise as
// many of its longest keywords as fit, and as a last resort the idea cut to
// the limit.
func npmSearchText(q model.Query) string {
	fits := func(s string) bool { return utf8.RuneCountInString(s) <= npmMaxText }
	if fits(q.Idea) {
		return q.Idea
	}
	for n := len(q.Keywords); n >= 1; n-- {
		if s := narrowed(q.Keywords, n); s != "" && fits(s) {
			return s
		}
	}
	return string([]rune(q.Idea)[:npmMaxText])
}

// npmPopularity scales score.detail.popularity (a fraction) to an integer as
// patent does: multiplied in float32, cast with Rust's saturating as, and
// dropped when that comes out as 0.
func npmPopularity(p float32) *uint64 {
	v := p * 1_000_000
	if !(v >= 1) { // also NaN
		return nil
	}
	u := uint64(math.MaxUint64)
	if v < math.MaxUint64 {
		u = uint64(v)
	}
	return &u
}

// Search implements Adapter.
func (a *Npm) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	var body struct {
		Objects *[]struct {
			Package *struct {
				Name        *string `json:"name"`
				Description string  `json:"description"`
				// Publish date of the version this hit refers to — RFC 3339,
				// milliseconds.
				Date freshness.Lenient[string] `json:"date"`
			} `json:"package"`
			Score *struct {
				Detail struct {
					Popularity float32 `json:"popularity"`
				} `json:"detail"`
			} `json:"score"`
		} `json:"objects"`
	}
	params := url.Values{"text": {npmSearchText(q)}, "size": {"20"}}
	if err := getJSON(ctx, a.Client, a.BaseURL+"/-/v1/search", params, &body); err != nil {
		return nil, err
	}
	if body.Objects == nil {
		return nil, missingField("objects")
	}
	var matches []model.Match
	for _, o := range *body.Objects {
		switch {
		case o.Package == nil:
			return nil, missingField("package")
		case o.Package.Name == nil:
			return nil, missingField("name")
		}
		m := model.Match{
			Name:        *o.Package.Name,
			Source:      model.Npm,
			URL:         "https://www.npmjs.com/package/" + *o.Package.Name,
			Description: o.Package.Description,
			LastUpdated: rfc3339Date(o.Package.Date),
		}
		if o.Score != nil {
			m.Popularity = npmPopularity(o.Score.Detail.Popularity)
		}
		matches = append(matches, m)
	}
	return matches, nil
}
