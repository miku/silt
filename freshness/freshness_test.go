package freshness

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func at(t *testing.T, ts string) time.Time {
	t.Helper()
	tt, ok := Parse(ts)
	if !ok {
		t.Fatalf("test timestamp %q must parse", ts)
	}
	return tt
}

func check(t *testing.T, name, got string, ok bool, want string) {
	t.Helper()
	if !ok || got != want {
		t.Errorf("%s = %q, %v; want %q", name, got, ok, want)
	}
}

func TestNormalisesEveryRegistryFormatToTheSameShape(t *testing.T) {
	// The real values probed from each live API, so this pins the actual
	// formats in play rather than an idealised version of them.
	s, ok := FromRFC3339("2026-05-15T06:13:41.215606Z") // crates.io
	check(t, "crates.io", s, ok, "2026-05-15T06:13:41Z")
	s, ok = FromRFC3339("2026-07-21T15:41:28.716Z") // npm
	check(t, "npm", s, ok, "2026-07-21T15:41:28Z")
	s, ok = FromRFC3339("2026-07-31T18:49:43Z") // GitHub
	check(t, "GitHub", s, ok, "2026-07-31T18:49:43Z")
	s, ok = FromUnixMillis(1_750_337_811_233) // Maven
	check(t, "Maven", s, ok, "2025-06-19T12:56:51Z")
	s, ok = FromUnixSecs(1_778_477_360) // AUR
	check(t, "AUR", s, ok, "2026-05-11T05:29:20Z")
	s, ok = FromGoDate("Feb 28, 2026") // pkg.go.dev
	check(t, "pkg.go.dev", s, ok, "2026-02-28T00:00:00Z")
	// The VS Code Marketplace is the only source seen writing a single-digit
	// fraction and a +00:00 offset instead of Z.
	s, ok = FromRFC3339("2026-08-27T09:58:55.6+00:00")
	check(t, "VS Code", s, ok, "2026-08-27T09:58:55Z")
}

func TestNonUTCOffsetsAreConvertedRatherThanTruncated(t *testing.T) {
	s, ok := FromRFC3339("2026-05-15T06:13:41+02:00")
	check(t, "+02:00", s, ok, "2026-05-15T04:13:41Z")
}

func TestMalformedInputYieldsNoneAndNeverPanics(t *testing.T) {
	for _, junk := range []string{
		"",
		"   ",
		"not a date",
		"2026-13-45T99:99:99Z",
		"1750337811233",
		"Feb 2026",
		"Smarch 3, 2026",
		"Feb 30, 2026",
	} {
		if s, ok := FromRFC3339(junk); ok {
			t.Errorf("FromRFC3339(%q) = %q", junk, s)
		}
		if s, ok := FromGoDate(junk); ok {
			t.Errorf("FromGoDate(%q) = %q", junk, s)
		}
		if a, ok := AgeOf(junk, Now()); ok {
			t.Errorf("AgeOf(%q) = %+v", junk, a)
		}
	}
	// Out-of-range epochs are rejected, not wrapped.
	if s, ok := FromUnixSecs(math.MaxInt64); ok {
		t.Errorf("FromUnixSecs(MaxInt64) = %q", s)
	}
	if s, ok := FromUnixMillis(math.MaxInt64); ok {
		t.Errorf("FromUnixMillis(MaxInt64) = %q", s)
	}
}

func TestEpochRangeEdges(t *testing.T) {
	// Rust's time crate renders RFC 3339 for years 0000 through 9999 only.
	s, ok := FromUnixSecs(253402300799)
	check(t, "last second of 9999", s, ok, "9999-12-31T23:59:59Z")
	if s, ok := FromUnixSecs(253402300800); ok {
		t.Errorf("year 10000 = %q", s)
	}
	s, ok = FromUnixSecs(-62167219200)
	check(t, "first second of 0000", s, ok, "0000-01-01T00:00:00Z")
	if s, ok := FromUnixSecs(-62167219201); ok {
		t.Errorf("year -1 = %q", s)
	}
	if s, ok := FromUnixSecs(math.MinInt64); ok {
		t.Errorf("FromUnixSecs(MinInt64) = %q", s)
	}
	s, ok = FromUnixMillis(253402300799_999)
	check(t, "last millisecond of 9999", s, ok, "9999-12-31T23:59:59Z")
	if s, ok := FromUnixMillis(math.MinInt64); ok {
		t.Errorf("FromUnixMillis(MinInt64) = %q", s)
	}
}

func TestOffsetsThatLeaveTheRenderableRangeYieldNone(t *testing.T) {
	// patent returns None for the first and panics on the second (a
	// documented deviation): both leave the years 0000–9999 once in UTC.
	for _, ts := range []string{"0000-01-01T00:30:00+01:00", "9999-12-31T23:00:00-02:00"} {
		if s, ok := FromRFC3339(ts); ok {
			t.Errorf("FromRFC3339(%q) = %q", ts, s)
		}
	}
}

func TestGoDateParsingIsAsStrictAsPatent(t *testing.T) {
	for in, want := range map[string]string{
		"  Feb 28, 2026  ":       "2026-02-28T00:00:00Z",
		"FEB 1, 2026":            "2026-02-01T00:00:00Z",
		"Feb 29, 2024":           "2024-02-29T00:00:00Z",
		"Feb +5, 2026":           "2026-02-05T00:00:00Z", // Rust's u8 parser takes a '+'
		"Feb 05 2026 extra text": "2026-02-05T00:00:00Z", // trailing tokens are ignored
	} {
		s, ok := FromGoDate(in)
		check(t, in, s, ok, want)
	}
	for _, bad := range []string{"February 28, 2026", "Feb 29, 2026", "Feb 0, 2026", "Feb 256, 2026", "Feb ++5, 2026", "Feb 5, -3"} {
		if s, ok := FromGoDate(bad); ok {
			t.Errorf("FromGoDate(%q) = %q", bad, s)
		}
	}
}

func TestLabelsReadNaturallyAcrossTheScale(t *testing.T) {
	now := at(t, "2026-08-02T00:00:00Z")
	for _, tc := range []struct{ ts, want string }{
		{"2026-08-02T00:00:00Z", "today"},
		{"2026-08-01T00:00:00Z", "yesterday"},
		{"2026-07-20T00:00:00Z", "13 days ago"},
		{"2026-06-02T00:00:00Z", "2 months ago"},
		{"2026-06-25T00:00:00Z", "1 month ago"},
		{"2025-09-02T00:00:00Z", "11 months ago"},
		{"2025-08-02T00:00:00Z", "1 year ago"},
		{"2021-08-02T00:00:00Z", "5 years ago"},
		// Boundaries that have bitten: 365 days is a hair under one mean
		// Gregorian year and floored to "0 years ago"; 360-364 days landed in
		// the months branch and read "12 months ago".
		{"2025-08-07T00:00:00Z", "11 months ago"},
		{"2025-08-03T00:00:00Z", "11 months ago"},
		{"2025-08-01T00:00:00Z", "1 year ago"},
		{"2024-08-03T00:00:00Z", "1 year ago"},
	} {
		a, ok := AgeOf(tc.ts, now)
		if !ok || a.Label != tc.want {
			t.Errorf("AgeOf(%s) = %q, want %q", tc.ts, a.Label, tc.want)
		}
	}
}

func TestStaleBoundarySitsExactlyAtTheTwoYearAnniversary(t *testing.T) {
	now := at(t, "2026-08-02T00:00:00Z")
	stale := func(ts string) bool {
		a, ok := AgeOf(ts, now)
		if !ok {
			t.Fatalf("AgeOf(%s) failed", ts)
		}
		return a.Stale
	}
	// One day short of the second anniversary.
	if stale("2024-08-03T00:00:00Z") {
		t.Error("one day short of two years must not be stale")
	}
	// The anniversary itself already counts.
	if !stale("2024-08-02T00:00:00Z") || !stale("2024-08-01T00:00:00Z") {
		t.Error("two years and more must be stale")
	}
}

func TestWholeCalendarYearsAreNotLostToLeapDays(t *testing.T) {
	// Five calendar years across the 2024 leap day is 1826 days; dividing by
	// a mean year floors that to four and reads "4 years ago".
	now := at(t, "2026-08-02T00:00:00Z")
	for ts, want := range map[string]string{
		"2021-08-02T00:00:00Z": "5 years ago",
		// Same shape one day either side of the anniversary.
		"2021-08-01T00:00:00Z": "5 years ago",
		"2021-08-03T00:00:00Z": "4 years ago",
	} {
		if a, _ := AgeOf(ts, now); a.Label != want {
			t.Errorf("AgeOf(%s) = %q, want %q", ts, a.Label, want)
		}
	}
}

func TestFutureTimestampsAreNotStale(t *testing.T) {
	now := at(t, "2026-08-02T00:00:00Z")
	a, ok := AgeOf("2027-01-01T00:00:00Z", now)
	if !ok || a.Label != "today" {
		t.Errorf("label = %q, want today", a.Label)
	}
	if a.Stale {
		t.Error("clock skew must never read as abandonment")
	}
}

func TestLenientDegradesTypeChangesToNotPresent(t *testing.T) {
	type hit struct {
		TS      Lenient[int64]  `json:"ts"`
		Updated Lenient[string] `json:"updated_at"`
	}
	for _, tc := range []struct {
		body      string
		ts        int64
		tsOK      bool
		updated   string
		updatedOK bool
	}{
		{`{"ts": 1778477360, "updated_at": "2026-05-15T06:13:41Z"}`, 1778477360, true, "2026-05-15T06:13:41Z", true},
		{`{}`, 0, false, "", false},
		{`{"ts": null, "updated_at": null}`, 0, false, "", false},
		{`{"ts": "1778477360", "updated_at": 5}`, 0, false, "", false},
		{`{"ts": {"secs": 1}, "updated_at": ["x"]}`, 0, false, "", false},
		{`{"ts": 1.5e9, "updated_at": true}`, 0, false, "", false},
	} {
		var h hit
		if err := json.Unmarshal([]byte(tc.body), &h); err != nil {
			t.Errorf("%s: a date field's type change must not fail the response: %v", tc.body, err)
			continue
		}
		if h.TS.OK != tc.tsOK || h.TS.V != tc.ts || h.Updated.OK != tc.updatedOK || h.Updated.V != tc.updated {
			t.Errorf("%s: decoded %+v", tc.body, h)
		}
	}
}
