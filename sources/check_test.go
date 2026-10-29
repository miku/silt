package sources

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/miku/silt"
	"github.com/miku/silt/model"
)

// checkClient answers as the live services might: crates.io answers, npm is
// over its rate limit for an hour, PyPI's search is walled but its index and
// JSON API answer, and GitHub fails.
func checkClient(t *testing.T) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Request: req}
		body := ""
		switch req.URL.Host + req.URL.Path {
		case "crates.io/api/v1/crates":
			body = twoCrateBody
		case "registry.npmjs.org/-/v1/search":
			resp.StatusCode = http.StatusTooManyRequests
			resp.Header.Set("Retry-After", "3600")
		case "pypi.org/search/":
			body = pypiChallenge
		case "pypi.org/simple/":
			body = `{"projects": [{"name": "json-tools"}]}`
		case "pypi.org/pypi/json-tools/json":
			body = project(`"Tools for JSON"`)
		case "api.github.com/search/repositories":
			resp.StatusCode = http.StatusInternalServerError
		default:
			t.Errorf("unexpected request %s", req.URL)
			resp.StatusCode = http.StatusNotFound
		}
		resp.Body = io.NopCloser(strings.NewReader(body))
		return resp, nil
	})}
}

func TestCheckSaysHowEachSourceAnswered(t *testing.T) {
	include := []model.Source{model.PyPI, model.GitHub, model.Npm, model.CratesIO}
	checks := Searcher{Client: checkClient(t)}.Check(t.Context(), CheckQuery, include, nil)
	want := []struct {
		source  model.Source
		status  CheckStatus
		matches int
	}{
		{model.CratesIO, CheckReached, 2},
		{model.GitHub, CheckFailed, 0},
		{model.Npm, CheckRateLimited, 0},
		{model.PyPI, CheckWalled, 0},
	}
	if len(checks) != len(want) {
		t.Fatalf("%d checks: %+v", len(checks), checks)
	}
	for i, w := range want {
		c := checks[i]
		if c.Source != w.source || c.Status != w.status || c.Matches != w.matches || c.Reached() != (w.status == CheckReached) {
			t.Errorf("check %d = %+v, want %v %v with %d matches", i, c, w.source, w.status, w.matches)
		}
		if c.Elapsed <= 0 {
			t.Errorf("%v: elapsed %v", c.Source, c.Elapsed)
		}
	}
	if npm := checks[2]; time.Until(npm.RateLimit.Reset) < 59*time.Minute || npm.Err == nil {
		t.Errorf("npm: %+v; want the reset an hour from now, and the error", npm)
	}
}

func TestCheckUsesTheBrowserAndTheIndexButNoCache(t *testing.T) {
	c := testCache(t)
	s := Searcher{Client: checkClient(t), Cache: c, Catalogs: testCatalogs(t), PyPIIndex: true}
	pypi := []model.Source{model.PyPI}
	if checks := s.Check(t.Context(), CheckQuery, pypi, nil); checks[0].Status != CheckByName || checks[0].Matches != 1 {
		t.Errorf("with the index: %+v", checks[0])
	}
	s.Browser = &fakeBrowser{dom: pypiHTML}
	if checks := s.Check(t.Context(), CheckQuery, pypi, nil); checks[0].Status != CheckBrowsed || checks[0].Matches != 2 {
		t.Errorf("with the browser: %+v", checks[0])
	}
	if entries, _ := os.ReadDir(c.Dir); len(entries) != 0 {
		t.Errorf("a check wrote %d cache entries", len(entries))
	}
	if entries, _ := os.ReadDir(s.Catalogs.Dir); len(entries) != 0 {
		t.Errorf("a check kept %d catalogs", len(entries))
	}
}

func TestCheckExcludesAndKeepsTheSourceOrder(t *testing.T) {
	checks := Searcher{Client: checkClient(t)}.Check(t.Context(), CheckQuery, []model.Source{model.Npm, model.CratesIO, model.PyPI}, []model.Source{model.PyPI})
	if len(checks) != 2 || checks[0].Source != model.CratesIO || checks[1].Source != model.Npm {
		t.Errorf("checks %+v", checks)
	}
}

func TestNewSourceCheckTellsATimeoutFromAFailure(t *testing.T) {
	timedOut := newSourceCheck(model.Go, result{err: &timeoutError{&silt.ParseError{Msg: "Go timed out"}}}, 15*time.Second)
	failed := newSourceCheck(model.Go, result{err: &silt.ParseError{Msg: "drift"}}, time.Second)
	if timedOut.Status != CheckTimedOut || failed.Status != CheckFailed || timedOut.Reached() || failed.Reached() {
		t.Errorf("timed out %v, failed %v", timedOut.Status, failed.Status)
	}
	if !errors.As(timedOut.Err, new(*silt.ParseError)) || timedOut.Err.Error() != "failed to parse response: Go timed out" {
		t.Errorf("the timeout should still be patent's error: %v", timedOut.Err)
	}
}

func TestCheckLeavesTheProcessCatalogsAlone(t *testing.T) {
	Searcher{Client: checkClient(t), PyPIIndex: true}.Check(t.Context(), CheckQuery, []model.Source{model.PyPI}, nil)
	if sharedPyPIIndex.fetched {
		t.Error("a check filled the process's PyPI index")
	}
}
