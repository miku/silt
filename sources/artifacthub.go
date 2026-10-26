package sources

import (
	"context"
	"net/http"
	"net/url"

	"github.com/miku/silt/freshness"
	"github.com/miku/silt/model"
)

const artifactHubBaseURL = "https://artifacthub.io"

// ArtifactHub searches the Artifact Hub package index: GET
// /api/v1/packages/search.
type ArtifactHub struct {
	Client  *http.Client
	BaseURL string
}

// NewArtifactHub returns an adapter for the live Artifact Hub.
func NewArtifactHub(client *http.Client) *ArtifactHub {
	return &ArtifactHub{Client: client, BaseURL: artifactHubBaseURL}
}

// ID implements Adapter.
func (*ArtifactHub) ID() model.Source { return model.ArtifactHub }

// artifactHubKinds maps Artifact Hub's numeric repository kind to the slug of
// its package pages (…/packages/{slug}/{repo}/{name}).
var artifactHubKinds = []string{
	0: "helm", 1: "falco", 2: "opa", 3: "olm", 4: "tbaction", 5: "krew",
	6: "helm-plugin", 7: "tekton-task", 8: "keda-scaler", 9: "coredns",
	10: "keptn", 11: "tekton-pipeline", 12: "container", 13: "kubewarden",
	14: "gatekeeper", 15: "kyverno", 16: "knative-client-plugin", 17: "backstage",
	18: "argo-template", 19: "kubearmor", 20: "kcl", 21: "headlamp",
	22: "inspektor-gadget", 23: "tekton-stepaction", 24: "meshery",
	25: "opencost", 26: "radius", 27: "bootc",
}

// Search implements Adapter.
func (a *ArtifactHub) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// ts_query_web ANDs every term against a comparatively small index, so
	// even two terms usually return nothing: measured live, a 7-term idea and
	// "command listening" both return zero where "listening" returns charts.
	// Narrow all the way to a single keyword; the noise that lets in is
	// dropped by similarity ranking.
	var matches []model.Match
	for _, term := range narrowingCandidates(q, 1) {
		var body struct {
			Packages []struct {
				Name        *string `json:"name"`
				Description string  `json:"description"`
				Stars       *uint64 `json:"stars"` // stargazers on Artifact Hub itself
				// Publish time of this version, in epoch seconds.
				TS         freshness.Lenient[int64] `json:"ts"`
				Repository *struct {
					Name *string `json:"name"`
					Kind *uint32 `json:"kind"`
					URL  *string `json:"url"`
				} `json:"repository"`
			} `json:"packages"`
		}
		params := url.Values{"ts_query_web": {term}, "limit": {"15"}, "facets": {"false"}}
		if err := getJSON(ctx, a.Client, a.BaseURL+"/api/v1/packages/search", params, &body); err != nil {
			return nil, err
		}
		matches = matches[:0]
		for _, p := range body.Packages {
			switch {
			case p.Name == nil:
				return nil, missingField("name")
			case p.Repository == nil:
				return nil, missingField("repository")
			case p.Repository.Name == nil:
				return nil, missingField("repository.name")
			case p.Repository.Kind == nil:
				return nil, missingField("kind")
			}
			m := model.Match{
				Name:        *p.Name,
				Source:      model.ArtifactHub,
				Description: p.Description,
			}
			// The canonical package page for a known kind; for a kind added
			// since, the repository's own URL rather than a broken link.
			if k := *p.Repository.Kind; k < uint32(len(artifactHubKinds)) {
				m.URL = a.BaseURL + "/packages/" + artifactHubKinds[k] + "/" + *p.Repository.Name + "/" + *p.Name
			} else if p.Repository.URL != nil {
				m.URL = *p.Repository.URL
			}
			// A 0-star package is no popularity signal.
			if p.Stars != nil && *p.Stars > 0 {
				m.Popularity = p.Stars
			}
			if p.TS.OK {
				if s, ok := freshness.FromUnixSecs(p.TS.V); ok {
					m.LastUpdated = &s
				}
			}
			matches = append(matches, m)
		}
		if len(matches) > 0 {
			break
		}
	}
	return matches, nil
}
