package sources

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miku/silt"
	"github.com/miku/silt/model"
)

var now = time.Date(2026, 10, 29, 20, 20, 47, 0, time.UTC)

func header(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestRetryAfter(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  time.Time
	}{
		{"", time.Time{}},
		{"0", now},
		{"30", now.Add(30 * time.Second)},
		{" 120 ", now.Add(2 * time.Minute)},
		{"Thu, 29 Oct 2026 20:22:00 GMT", time.Date(2026, 10, 29, 20, 22, 0, 0, time.UTC)},
		{"-5", time.Time{}},
		{"1.5", time.Time{}},
		{"soon", time.Time{}},
		{"99999999999", time.Time{}}, // overflows uint32: no sane wait
	} {
		if got := retryAfter(header("Retry-After", tt.value), now); !got.Equal(tt.want) {
			t.Errorf("Retry-After %q = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestGitHubReset(t *testing.T) {
	reset := strconv.FormatInt(now.Add(time.Minute).Unix(), 10)
	for _, tt := range []struct {
		name string
		h    http.Header
		want time.Time
	}{
		{"primary limit", header("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", reset), now.Add(time.Minute)},
		{"secondary limit", header("Retry-After", "45"), now.Add(45 * time.Second)},
		{"Retry-After wins", header("Retry-After", "5", "X-RateLimit-Remaining", "0", "X-RateLimit-Reset", reset), now.Add(5 * time.Second)},
		{"requests left: not the primary limit", header("X-RateLimit-Remaining", "3", "X-RateLimit-Reset", reset), time.Time{}},
		{"no reset", header("X-RateLimit-Remaining", "0"), time.Time{}},
		{"bad reset", header("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", "tomorrow"), time.Time{}},
		{"no headers", http.Header{}, time.Time{}},
	} {
		if got := gitHubReset(tt.h, now); !got.Equal(tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestA429IsARateLimitThatStaysAnHTTPError(t *testing.T) {
	srv := newMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	resp, err := get(t.Context(), NewHTTPClient(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = decodeJSON(resp, new(any))
	var limited *rateLimitError
	var httpErr *silt.HTTPError
	if !errors.As(err, &limited) || !errors.As(err, &httpErr) || httpErr.StatusCode != 429 || !silt.IsRetryable(err) {
		t.Fatalf("err = %#v; want a retryable HTTPError 429 that is also a rateLimitError", err)
	}
	if d := limited.reset.Sub(start); d < 29*time.Second || d > 31*time.Second {
		t.Errorf("reset in %v, want 30s", d)
	}
}

func TestGitHubRateLimitCarriesTheResetAndAHint(t *testing.T) {
	reset := time.Now().Add(time.Minute).Truncate(time.Second)
	for _, status := range []int{403, 429} {
		srv := newMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
			w.WriteHeader(status)
		}))
		for _, token := range []string{"", "ghp_test"} {
			a := gitHubFor(srv)
			a.Token = token
			_, err := a.Search(t.Context(), query())
			var limited *rateLimitError
			if !errors.As(err, &limited) || !limited.reset.Equal(reset) {
				t.Fatalf("%d, token %q: err = %#v; want a rate limit until %v", status, token, err, reset)
			}
			if wantHint := token == ""; (limited.hint != "") != wantHint {
				t.Errorf("%d, token %q: hint %q", status, token, limited.hint)
			}
		}
	}
}

func TestRetryWait(t *testing.T) {
	f := fanout{retryDelay: 800 * time.Millisecond, minRetryTime: 2 * time.Second}
	deadline := now.Add(15 * time.Second)
	limitedUntil := func(d time.Duration) error {
		return &rateLimitError{err: &silt.HTTPError{StatusCode: 429}, reset: now.Add(d)}
	}
	for _, tt := range []struct {
		name     string
		err      error
		want     time.Duration
		wantWait bool
	}{
		{"no error", nil, 0, false},
		{"a wall", &silt.UnavailableError{Msg: "walled"}, 0, false},
		{"reset soon", limitedUntil(3 * time.Second), 3 * time.Second, true},
		{"reset already passed", limitedUntil(-time.Second), 0, true},
		{"reset leaves exactly minRetryTime", limitedUntil(13 * time.Second), 13 * time.Second, true},
		{"reset leaves less than minRetryTime", limitedUntil(13*time.Second + time.Millisecond), 0, false},
		{"reset after the deadline", limitedUntil(time.Minute), 0, false},
	} {
		got, ok := f.retryWait(tt.err, now, deadline)
		if ok != tt.wantWait || ok && got != tt.want {
			t.Errorf("%s: retryWait = %v, %v; want %v, %v", tt.name, got, ok, tt.want, tt.wantWait)
		}
	}

	// Any other retryable failure, a rate limit without a reset among them,
	// waits retryDelay give or take half.
	seen := map[time.Duration]bool{}
	for _, err := range []error{&silt.ParseError{Msg: "drift"}, &rateLimitError{err: &silt.HTTPError{StatusCode: 429}}} {
		for range 200 {
			got, ok := f.retryWait(err, now, deadline)
			if !ok || got < 400*time.Millisecond || got >= 1200*time.Millisecond {
				t.Fatalf("%v: retryWait = %v, %v; want 400ms to 1.2s", err, got, ok)
			}
			seen[got] = true
		}
	}
	if len(seen) < 10 {
		t.Errorf("%d distinct delays in 400 retries; the delay isn't jittered", len(seen))
	}
}

// rateLimited answers 429 with retryAfter, or without the header if it is
// empty, for the first n requests, then h.
func rateLimited(n int, retryAfter string, h http.Handler) http.HandlerFunc {
	var mu sync.Mutex
	served := 0
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		served++
		limited := served <= n
		mu.Unlock()
		if !limited {
			h.ServeHTTP(w, r)
			return
		}
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}
}

func TestFanOutWaitsForAResetThatFitsTheDeadline(t *testing.T) {
	srv := newMockServer(t, onPath("/api/v1/crates", rateLimited(1, "0", respond(200, twoCrateBody))))
	out := defaultFanout.search(t.Context(), []Adapter{cratesIOFor(srv)}, query())
	if len(out.Reached) != 1 || len(out.Matches) != 2 || len(out.RateLimited) != 0 || len(srv.Requests()) != 2 {
		t.Errorf("reached %v, %d matches, rate-limited %v after %d requests; want the retry to get through",
			out.Reached, len(out.Matches), out.RateLimited, len(srv.Requests()))
	}
}

func TestFanOutDoesNotRetryBeforeAFarReset(t *testing.T) {
	srv := newMockServer(t, onPath("/api/v1/crates", rateLimited(1, "3600", respond(200, twoCrateBody))))
	start := time.Now()
	out := defaultFanout.search(t.Context(), []Adapter{cratesIOFor(srv)}, query())
	if n := len(srv.Requests()); n != 1 {
		t.Errorf("%d requests; a retry before the reset would be refused again", n)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %v; it should give up at once", time.Since(start))
	}
	if len(out.RateLimited) != 1 || out.RateLimited[0].Source != model.CratesIO ||
		out.RateLimited[0].Reset.Sub(start) < 59*time.Minute || len(out.Failed) != 1 {
		t.Errorf("failed %v, rate-limited %+v; want crates.io until an hour from now", out.Failed, out.RateLimited)
	}
}

func TestFanOutRetriesARateLimitWithoutAResetOnce(t *testing.T) {
	srv := newMockServer(t, onPath("/api/v1/crates", rateLimited(2, "", respond(200, twoCrateBody))))
	out := fastFanout.search(t.Context(), []Adapter{cratesIOFor(srv)}, query())
	if n := len(srv.Requests()); n != 2 {
		t.Errorf("%d requests, want 2", n)
	}
	if len(out.RateLimited) != 1 || !out.RateLimited[0].Reset.IsZero() {
		t.Errorf("rate-limited %+v; want crates.io, reset unknown", out.RateLimited)
	}
}

func TestFanOutReportsGitHubsRateLimitWithTheHint(t *testing.T) {
	reset := time.Now().Add(50 * time.Second)
	srv := newMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		w.WriteHeader(http.StatusForbidden)
	}))
	other := &staticSource{id: model.CratesIO}
	out := defaultFanout.search(t.Context(), []Adapter{other, gitHubFor(srv)}, query())
	if n := len(srv.Requests()); n != 1 {
		t.Errorf("%d requests to GitHub, want 1: its reset is past the deadline", n)
	}
	want := []RateLimit{{Source: model.GitHub, Reset: time.Unix(reset.Unix(), 0), Hint: "set GITHUB_TOKEN for a higher limit"}}
	if len(out.RateLimited) != 1 || out.RateLimited[0] != want[0] || len(out.Reached) != 1 {
		t.Errorf("rate-limited %+v, reached %v; want %+v", out.RateLimited, out.Reached, want)
	}
}

func TestHackageFetchesFewCabalFilesAtOnce(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	var names []string
	for i := range 20 {
		names = append(names, `{"name": "p`+strconv.Itoa(i)+`"}`)
	}
	search := "[" + strings.Join(names, ",") + "]"
	srv := newMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/packages/search" {
			respond(200, search)(w, r)
			return
		}
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		w.Write([]byte("synopsis: package " + r.URL.Path + "\n"))
	}))
	m := mustSearch(t, hackageFor(srv), query())
	if len(m) != 20 || m[7].Name != "p7" || m[7].Description != "package /package/p7/p7.cabal" {
		t.Fatalf("got %d matches, the eighth %+v", len(m), m[7])
	}
	// 4, not maxCabalFetches: the test is of the cap, not of the constant.
	if peak > 4 || peak < 2 {
		t.Errorf("peak of %d .cabal fetches at once; want at most 4, and more than one", peak)
	}
}

// The reset has to fit the caller's deadline too, when it is earlier than the
// fan-out's.
func TestTheResetMustFitTheCallersDeadline(t *testing.T) {
	srv := newMockServer(t, onPath("/api/v1/crates", rateLimited(1, "10", respond(200, twoCrateBody))))
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	out := defaultFanout.search(ctx, []Adapter{cratesIOFor(srv)}, query())
	if time.Since(start) > 2*time.Second || len(out.Failed) != 1 || len(srv.Requests()) != 1 {
		t.Errorf("took %v, failed %v, %d requests", time.Since(start), out.Failed, len(srv.Requests()))
	}
}
