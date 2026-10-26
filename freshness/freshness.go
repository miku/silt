// Package freshness normalises each registry's "last updated" timestamp and
// turns it into the staleness signal shown in the UI.
//
// Registries disagree on how to express a date: crates.io and GitHub send
// RFC 3339 strings (with wildly varying sub-second precision), Maven sends
// epoch milliseconds, AUR and Artifact Hub send epoch seconds, and pkg.go.dev
// only renders a human date into its HTML. Adapters funnel all of these
// through the From* functions here so that [model.Match].LastUpdated is
// always one shape: whole-second RFC 3339 in UTC, e.g. 2026-05-15T06:13:41Z.
//
// Every constructor reports failure with ok == false and never with an
// error: a registry that changes its timestamp format must degrade to "no
// date known", not take out the whole source. That mirrors the rule that one
// source's trouble never fails the run.
//
// [model.Match]: https://pkg.go.dev/github.com/miku/silt/model#Match
package freshness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// StaleAfterYears is how long a match can go without an update before it is
// called stale.
//
// Two years is deliberately generous: plenty of finished, still-perfectly-good
// tools sit untouched for a year or more, and flagging those would cry wolf.
const StaleAfterYears = 2

// Age is how long ago a match was last updated, ready for display.
type Age struct {
	// Label is human-readable, e.g. "3 years ago", "5 months ago", "today".
	Label string
	// Stale reports whether the match has gone StaleAfterYears without an
	// update.
	Stale bool
}

// The range of instants that render as RFC 3339: years 0000 to 9999.
var (
	minTime = time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC)
	maxTime = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)
)

// Parse parses an RFC 3339 timestamp, at any sub-second precision.
func Parse(ts string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, ts)
	return t, err == nil
}

// canonical renders t in the form stored on Match.LastUpdated: whole-second
// RFC 3339, UTC.
func canonical(t time.Time) (string, bool) {
	t = t.UTC().Truncate(time.Second)
	if t.Before(minTime) || t.After(maxTime) {
		return "", false
	}
	return t.Format(time.RFC3339), true
}

// FromRFC3339 normalises a registry-supplied RFC 3339 string (crates.io, npm,
// GitHub, Hex). Anything unparseable reports ok == false, so a format change
// downgrades the match to "no date known" rather than failing its source.
func FromRFC3339(ts string) (string, bool) {
	t, ok := Parse(ts)
	if !ok {
		return "", false
	}
	return canonical(t)
}

// FromUnixSecs normalises an epoch-seconds timestamp (AUR, Artifact Hub).
// Values outside the years 0000–9999 report ok == false.
func FromUnixSecs(secs int64) (string, bool) {
	if secs < minTime.Unix() || secs > maxTime.Unix() {
		return "", false
	}
	return canonical(time.Unix(secs, 0))
}

// FromUnixMillis normalises an epoch-milliseconds timestamp (Maven, JetBrains).
// Values outside the years 0000–9999 report ok == false.
func FromUnixMillis(millis int64) (string, bool) {
	if millis < minTime.UnixMilli() || millis > maxTime.UnixMilli()+999 {
		return "", false
	}
	return canonical(time.UnixMilli(millis))
}

var months = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}

// FromGoDate normalises pkg.go.dev's rendered publication date, e.g.
// "Feb 28, 2026".
//
// This is the only source whose date is scraped from human-facing HTML rather
// than read from an API field, so it is also the likeliest to drift — hence
// the same no-error contract as everything else here.
func FromGoDate(text string) (string, bool) {
	parts := strings.Fields(strings.ReplaceAll(strings.TrimSpace(text), ",", ""))
	if len(parts) < 3 {
		return "", false
	}
	month, ok := months[strings.ToLower(parts[0])]
	if !ok {
		return "", false
	}
	// Rust's integer parsing accepts one leading '+'; ParseUint accepts none.
	day, err := strconv.ParseUint(strings.TrimPrefix(parts[1], "+"), 10, 8)
	if err != nil {
		return "", false
	}
	year, err := strconv.ParseInt(parts[2], 10, 32)
	if err != nil {
		return "", false
	}
	t := time.Date(int(year), month, int(day), 0, 0, 0, 0, time.UTC)
	// time.Date normalises Feb 30 to Mar 2; a real calendar date round-trips.
	if t.Day() != int(day) || t.Month() != month {
		return "", false
	}
	return canonical(t)
}

// Now returns the current time in UTC, for passing to [AgeOf].
func Now() time.Time { return time.Now().UTC() }

// AgeOf describes how long ago ts was, relative to now.
//
// now is a parameter, not a read of the system clock, so that the
// StaleAfterYears boundary can be tested at an exact offset instead of
// drifting with the date the suite happens to run on. Its calendar fields are
// used as they are, so pass a UTC time such as [Now] returns.
//
// It reports ok == false if ts is not a timestamp this package wrote. A
// timestamp in the future — clock skew, or a registry publishing a
// post-dated release — reads as "today" and is never stale.
func AgeOf(ts string, now time.Time) (Age, bool) {
	then, ok := Parse(ts)
	if !ok {
		return Age{}, false
	}
	// Duration saturates at about 292 years, which only matters for the
	// day-based labels below a year, where it cannot occur.
	days := int64(now.Sub(then) / (24 * time.Hour))

	// Completed calendar years, deliberately not days / 365.2425: five
	// calendar years spanning a leap day is 1826 days, which a mean-year
	// divisor floors to four, so an exact five-year-old release displayed as
	// "4 years ago". Counting anniversaries has no such rounding edge.
	years := now.Year() - then.Year()
	if now.Month() < then.Month() || (now.Month() == then.Month() && now.Day() < then.Day()) {
		years--
	}

	var label string
	switch {
	case days <= 0:
		label = "today"
	case days == 1:
		label = "yesterday"
	case days < 31:
		label = fmt.Sprintf("%d days ago", days)
	case years < 1:
		// Capped at 11 so the 360–364 day range can't read "12 months ago"
		// while still being under a year.
		switch m := min(max(days/30, 1), 11); m {
		case 1:
			label = "1 month ago"
		default:
			label = fmt.Sprintf("%d months ago", m)
		}
	case years == 1:
		label = "1 year ago"
	default:
		label = fmt.Sprintf("%d years ago", years)
	}
	return Age{Label: label, Stale: years >= StaleAfterYears}, true
}

// Lenient decodes a registry's date field, degrading a type change — a string
// where a number used to be, a null, an object — to "not present" instead of
// failing the entire response. Its UnmarshalJSON never returns an error.
//
// An absent field already decodes as not present; Lenient covers the case of
// a field that is there but has changed shape, which would otherwise take its
// adapter down with a decoding error and turn a cosmetic upstream change into
// a whole source going dark.
type Lenient[T any] struct {
	V  T
	OK bool
}

// UnmarshalJSON implements json.Unmarshaler.
func (l *Lenient[T]) UnmarshalJSON(b []byte) error {
	*l = Lenient[T]{}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return nil
	}
	var v T
	if json.Unmarshal(b, &v) == nil {
		*l = Lenient[T]{V: v, OK: true}
	}
	return nil
}
