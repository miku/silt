package sources

import "testing"

// Each registry gets its query string with the parameters in the order
// patent sends them, not sorted as url.Values would: pkg.go.dev ranks the
// results for "q=…&m=package" differently from "m=package&q=…".
func TestQueryStringsKeepPatentsParameterOrder(t *testing.T) {
	srv := newMockServer(t, respond(500, ""))
	for _, c := range []struct {
		adapter Adapter
		want    string
	}{
		{cratesIOFor(srv), "q=async+runtime&per_page=20"},
		{gitHubFor(srv), "q=async+runtime+in%3Adescription%2Creadme&per_page=50"},
		{npmFor(srv), "text=a+fast+async+runtime+for+rust&size=20"},
		{pypiFor(srv), "q=async+runtime"},
		{hnFor(srv), "query=a+fast+async+runtime+for+rust&hitsPerPage=20&tags=story"},
		{goFor(srv), "q=async+runtime&m=package"},
		{mavenFor(srv), "q=async+runtime&rows=20&wt=json"},
		{rubygemsFor(srv), "query=async+runtime"},
		{dockerFor(srv), "query=async+runtime&page_size=20"},
		{jetbrainsFor(srv), "search=async+runtime&max=20"},
		{nugetFor(srv), "q=async+runtime&take=20"},
		{packagistFor(srv), "q=async+runtime&per_page=15"},
		{hexFor(srv), "search=runtime&page=1"},
		{artifacthubFor(srv), "ts_query_web=async+runtime&limit=15&facets=false"},
		{aurFor(srv), "v=5&type=search&by=name-desc&arg=async+runtime"},
		{hackageFor(srv), "terms=async+runtime"},
	} {
		sent := len(srv.Requests())
		if _, err := c.adapter.Search(t.Context(), query()); err == nil {
			t.Errorf("%v: want an error for the 500", c.adapter.ID())
		}
		requests := srv.Requests()
		if len(requests) == sent {
			t.Errorf("%v sent no request", c.adapter.ID())
			continue
		}
		if got := requests[sent].URL.RawQuery; got != c.want {
			t.Errorf("%v sent %q, want %q", c.adapter.ID(), got, c.want)
		}
	}
}
