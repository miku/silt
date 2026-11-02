package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"github.com/miku/silt/internal/freshness"
	"github.com/miku/silt/internal/model"
	"github.com/miku/silt/internal/silt"
)

const vsCodeBaseURL = "https://marketplace.visualstudio.com"

// VSCodeMarketplace searches the VS Code Marketplace: POST
// /_apis/public/gallery/extensionquery.
type VSCodeMarketplace struct {
	Client  *http.Client
	BaseURL string
}

// NewVSCodeMarketplace returns an adapter for the live marketplace.
func NewVSCodeMarketplace(client *http.Client) *VSCodeMarketplace {
	return &VSCodeMarketplace{Client: client, BaseURL: vsCodeBaseURL}
}

// ID implements Adapter.
func (*VSCodeMarketplace) ID() model.Source { return model.VSCodeMarketplace }

// saturatingUint64 converts like Rust's f as u64: truncated toward zero,
// with NaN and negative values 0 and overflow the maximum.
func saturatingUint64(f float64) uint64 {
	switch {
	case !(f > 0):
		return 0
	case f >= math.MaxUint64:
		return math.MaxUint64
	}
	return uint64(f)
}

// Search implements Adapter.
func (a *VSCodeMarketplace) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	type criterion struct {
		FilterType int    `json:"filterType"`
		Value      string `json:"value"`
	}
	query, err := json.Marshal(map[string]any{
		"filters": []map[string]any{{
			"criteria": []criterion{
				{8, "Microsoft.VisualStudio.Code"},
				{10, strings.Join(q.Keywords, " ")},
			},
			"pageSize":   20,
			"pageNumber": 1,
		}},
		// Without 914, a narrower flag set silently drops lastUpdated.
		"flags": 914,
	})
	if err != nil {
		return nil, &silt.HTTPError{Err: err}
	}
	req, err := newRequest(ctx, http.MethodPost, a.BaseURL+"/_apis/public/gallery/extensionquery", nil, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json;api-version=7.1-preview.1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := send(a.Client, req)
	if err != nil {
		return nil, err
	}
	var body struct {
		Results *[]struct {
			Extensions *[]struct {
				Publisher *struct {
					PublisherName *string `json:"publisherName"`
				} `json:"publisher"`
				ExtensionName    *string `json:"extensionName"`
				DisplayName      *string `json:"displayName"`
				ShortDescription string  `json:"shortDescription"`
				Statistics       []struct {
					StatisticName *string  `json:"statisticName"`
					Value         *float64 `json:"value"`
				} `json:"statistics"`
				LastUpdated freshness.Lenient[string] `json:"lastUpdated"`
			} `json:"extensions"`
		} `json:"results"`
	}
	if err := decodeJSON(resp, &body); err != nil {
		return nil, err
	}
	if body.Results == nil {
		return nil, missingField("results")
	}
	var matches []model.Match
	for i, r := range *body.Results {
		if r.Extensions == nil {
			return nil, missingField("extensions")
		}
		for _, e := range *r.Extensions {
			switch {
			case e.Publisher == nil:
				return nil, missingField("publisher")
			case e.Publisher.PublisherName == nil:
				return nil, missingField("publisherName")
			case e.ExtensionName == nil:
				return nil, missingField("extensionName")
			}
			var installs *uint64
			for _, s := range e.Statistics {
				switch {
				case s.StatisticName == nil:
					return nil, missingField("statisticName")
				case s.Value == nil:
					return nil, missingField("value")
				}
				if installs == nil && *s.StatisticName == "install" {
					n := saturatingUint64(*s.Value)
					installs = &n
				}
			}
			// Only the first result's extensions count, but like serde, every
			// result is checked.
			if i > 0 || e.ShortDescription == "" {
				continue
			}
			name := *e.ExtensionName
			if e.DisplayName != nil {
				name = *e.DisplayName
			}
			matches = append(matches, model.Match{
				Name:        name,
				Source:      model.VSCodeMarketplace,
				URL:         "https://marketplace.visualstudio.com/items?itemName=" + *e.Publisher.PublisherName + "." + *e.ExtensionName,
				Description: e.ShortDescription,
				Popularity:  installs,
				LastUpdated: rfc3339Date(e.LastUpdated),
			})
		}
	}
	return matches, nil
}
