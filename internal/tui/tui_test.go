package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miku/silt/internal/pipeline"
	"github.com/miku/silt/model"
	"github.com/miku/silt/rank"
	"github.com/miku/silt/verdict"
)

var testNow = time.Date(2026, time.October, 26, 0, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

// testMatches has a stale match, a fresh one, an undated one and one
// without popularity.
func testMatches() []model.Match {
	return []model.Match{
		{Name: "kill-port", Source: model.Npm, URL: "https://www.npmjs.com/package/kill-port",
			Description: "Kill the process running on a given port", Popularity: ptr(uint64(2_100_000)),
			Similarity: 0.875, LastUpdated: ptr("2019-02-28T12:00:00Z")},
		{Name: "fkill-cli", Source: model.Npm, URL: "https://www.npmjs.com/package/fkill-cli",
			Description: "Fabulously kill processes", Popularity: ptr(uint64(98_000)),
			Similarity: 0.81, LastUpdated: ptr("2026-10-20T08:00:00Z")},
		{Name: "jkfran/killport", Source: model.GitHub, URL: "https://github.com/jkfran/killport",
			Description: "A command-line tool to easily kill processes running on a specified port.",
			Popularity:  ptr(uint64(1200)), Similarity: 0.75},
		{Name: "Port-Kill", Source: model.CratesIO, URL: "https://crates.io/crates/port-kill",
			Description: "ports & processes", Similarity: 0.45},
	}
}

func testVerdict(gaps int, failed ...model.Source) model.Verdict {
	v := model.Verdict{
		Level:          model.Crowded,
		Headline:       "kill-port and fkill-cli already do this in the sources checked.",
		Gaps:           []string{},
		SourcesChecked: []model.Source{model.CratesIO, model.GitHub, model.Npm, model.HackerNews},
		SourcesFailed:  failed,
		Caveat:         verdict.Caveat,
	}
	for i := range gaps {
		v.Gaps = append(v.Gaps, fmt.Sprintf("gap %d: a tool that also shows which process holds the port before killing it", i+1))
	}
	return v
}

func testResult() *pipeline.Result {
	return &pipeline.Result{Verdict: testVerdict(2, model.PyPI), Matches: testMatches()}
}

// fakes records the side effects of a model.
type fakes struct {
	opened, copied []string
	copyErr        error
	searches       []string
}

// newModel returns a model of the given size, with fake side effects and a
// fixed clock. A nil res opens on the search prompt; search may be nil.
func newModel(t *testing.T, width, height int, res *pipeline.Result, search SearchFunc) (*Model, *fakes) {
	t.Helper()
	f := &fakes{}
	if search == nil {
		search = func(ctx context.Context, idea string, _ func(string), _ rank.Progress) (pipeline.Result, error) {
			<-ctx.Done()
			return pipeline.Result{}, ctx.Err()
		}
	}
	m := New(t.Context(), Options{
		Idea:   "cli tool that kills a process on a given port",
		Result: res,
		Search: func(ctx context.Context, idea string, log func(string), p rank.Progress) (pipeline.Result, error) {
			f.searches = append(f.searches, idea)
			return search(ctx, idea, log, p)
		},
		Backend: "Verdicts by qwen3.5 on the local Ollama.",
	})
	m.now = func() time.Time { return testNow }
	m.openURL = func(url string) error { f.opened = append(f.opened, url); return nil }
	m.copyText = func(s string) error { f.copied = append(f.copied, s); return f.copyErr }
	t.Cleanup(m.cancelSearch)
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m, f
}

// keyMsg is the key press that bubbletea names k.
func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

// press sends keys to m, running each command that answers at once (not a
// running search or a timer) and feeding its message back, and returns the
// command of the last key.
func press(m *Model, keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.Update(keyMsg(k))
	}
	return cmd
}

// typeText types s into m, one key per rune.
func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(keyMsg(string(r)))
	}
}

// screen is what m shows, without styles.
func screen(m *Model) string { return ansi.Strip(m.View().Content) }

// squash collapses whitespace, so that text wrapped over lines can be found
// with strings.Contains.
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

// checkFits checks that a screen has exactly height lines of at most width
// cells.
func checkFits(t *testing.T, s string, width, height int) {
	t.Helper()
	lines := strings.Split(s, "\n")
	if len(lines) != height {
		t.Errorf("%dx%d: %d lines", width, height, len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > width {
			t.Errorf("%dx%d: line %d is %d cells wide: %q", width, height, i, w, l)
		}
	}
}

func lineWith(t *testing.T, s, substr string) string {
	t.Helper()
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, substr) {
			return l
		}
	}
	t.Fatalf("no line with %q in:\n%s", substr, s)
	return ""
}

// rows are the names in the table, top to bottom.
func rows(m *Model) []string {
	var names []string
	for _, i := range m.list.rows {
		names = append(names, m.list.matches[i].Name)
	}
	return names
}

func selectedName(m *Model) string {
	sel, _ := m.list.selected()
	return sel.Name
}

func TestKeyNames(t *testing.T) {
	for _, k := range []string{"enter", "esc", "backspace", "up", "down", "pgup", "pgdown", "home", "end", "ctrl+c", "j", "G", "?", "/"} {
		if got := keyMsg(k).String(); got != k {
			t.Errorf("keyMsg(%q).String() = %q", k, got)
		}
	}
}

// -- The integrity rules ---------------------------------------------------------

var commonSizes = [][2]int{{80, 24}, {100, 30}, {120, 40}, {160, 50}, {200, 60}}

func TestCaveatIsNeverClippedAtCommonSizes(t *testing.T) {
	for _, size := range commonSizes {
		for gaps := range 7 {
			res := &pipeline.Result{Verdict: testVerdict(gaps, model.PyPI, model.Go), Matches: testMatches()}
			m, _ := newModel(t, size[0], size[1], res, nil)
			s := screen(m)
			checkFits(t, s, size[0], size[1])
			if !strings.Contains(squash(s), squash(verdict.Caveat)) {
				t.Errorf("%v, %d gaps: the caveat is clipped:\n%s", size, gaps, s)
			}
			if !strings.Contains(s, "Not reached: PyPI · Go") {
				t.Errorf("%v, %d gaps: the sources not reached are not named:\n%s", size, gaps, s)
			}
		}
	}
}

func TestNotReachedSourcesAreSurfaced(t *testing.T) {
	m, _ := newModel(t, 100, 30, &pipeline.Result{Verdict: testVerdict(0, model.PyPI, model.Go), Matches: testMatches()}, nil)
	if s := screen(m); !strings.Contains(s, "Not reached: PyPI · Go") {
		t.Errorf("failed sources must be named:\n%s", s)
	}
	m, _ = newModel(t, 100, 30, &pipeline.Result{Verdict: testVerdict(0), Matches: testMatches()}, nil)
	if s := screen(m); strings.Contains(s, "Not reached") {
		t.Errorf("no failed source, no line:\n%s", s)
	}
}

func TestRendersWithoutPanicAtTinySizes(t *testing.T) {
	// At any size, a verdict is either shown with its whole caveat and the
	// sources not reached, or not at all.
	for w := 1; w <= 40; w += 3 {
		for h := 1; h <= 30; h += 2 {
			for _, mode := range []string{"", "enter", "?"} {
				m, _ := newModel(t, w, h, &pipeline.Result{Verdict: testVerdict(3, model.PyPI), Matches: testMatches()}, nil)
				if mode != "" {
					press(m, mode)
				}
				s := screen(m)
				checkFits(t, s, w, h)
				if mode == "" && strings.Contains(s, "Crowded") &&
					(!strings.Contains(squash(s), squash(verdict.Caveat)) || !strings.Contains(squash(s), "Not reached: PyPI")) {
					t.Errorf("%dx%d: a verdict without its caveat:\n%s", w, h, s)
				}
			}
		}
	}
	// The prompt and the progress screen too.
	for _, size := range [][2]int{{1, 1}, {5, 3}, {20, 6}} {
		m, _ := newModel(t, size[0], size[1], nil, nil)
		checkFits(t, screen(m), size[0], size[1])
		typeText(m, "cli tool that kills a process on a given port")
		press(m, "enter")
		checkFits(t, screen(m), size[0], size[1])
	}
}

func TestEveryScreenFitsAtCommonSizes(t *testing.T) {
	for _, size := range commonSizes {
		for _, keys := range [][]string{nil, {"enter"}, {"?"}, {"/", "k"}, {"n"}} {
			m, _ := newModel(t, size[0], size[1], testResult(), nil)
			press(m, keys...)
			checkFits(t, screen(m), size[0], size[1])
		}
	}
}

func TestStaleMatchIsFlaggedInTheTable(t *testing.T) {
	m, _ := newModel(t, 120, 30, testResult(), nil)
	if l := lineWith(t, screen(m), " 0.88 "); !strings.Contains(l, "kill-port "+staleMark) || !strings.Contains(l, "7 years ago") {
		t.Errorf("a stale match must carry the marker and its age: %q", l)
	}
}

func TestFreshMatchCarriesNoStaleMarker(t *testing.T) {
	m, _ := newModel(t, 120, 30, testResult(), nil)
	if l := lineWith(t, screen(m), " 0.81 "); !strings.Contains(l, "fkill-cli") || strings.Contains(l, staleMark) || !strings.Contains(l, "5 days ago") {
		t.Errorf("a fresh match is dated but not flagged: %q", l)
	}
}

func TestUndatedMatchIsNeverFlaggedAsStale(t *testing.T) {
	m, _ := newModel(t, 120, 30, testResult(), nil)
	if l := lineWith(t, screen(m), " 0.75 "); !strings.Contains(l, "jkfran/killport") || strings.Contains(l, staleMark) || strings.Contains(l, "ago") {
		t.Errorf("an undated match must claim nothing about its age: %q", l)
	}
}

func TestTextFromOutsideCannotReachTheTerminal(t *testing.T) {
	res := testResult()
	res.Matches[0].Description = "evil\x1b]0;pwned\x07 \x1b[2Jclear"
	res.Verdict.Gaps = []string{"gap\x1b[31m red"}
	m, _ := newModel(t, 120, 30, res, nil)
	raw := m.View().Content
	for _, seq := range []string{"\x1b]0;pwned", "\x1b[2J", "\x1b[31m red"} {
		if strings.Contains(raw, seq) {
			t.Errorf("the escape sequence %q reached the screen", seq)
		}
	}
	if s := screen(m); !strings.Contains(s, "evil clear") || !strings.Contains(s, "gap red") {
		t.Errorf("the text around the sequences must stay:\n%s", s)
	}
}

// -- The table -------------------------------------------------------------------

func TestTableTitleShowsActiveSortKey(t *testing.T) {
	m, _ := newModel(t, 100, 30, testResult(), nil)
	for _, want := range []string{"similarity", "popularity", "name", "similarity"} {
		if l := lineWith(t, screen(m), "Matches ("); !strings.HasSuffix(strings.TrimRight(l, "─ "), "sort: "+want) {
			t.Errorf("title = %q, want sort %s", l, want)
		}
		press(m, "s")
	}
}

func TestSortOrders(t *testing.T) {
	m, _ := newModel(t, 100, 30, testResult(), nil)
	for _, want := range [][]string{
		{"kill-port", "fkill-cli", "jkfran/killport", "Port-Kill"}, // similarity
		{"kill-port", "fkill-cli", "jkfran/killport", "Port-Kill"}, // popularity, missing last
		{"fkill-cli", "jkfran/killport", "kill-port", "Port-Kill"}, // name, ignoring case
	} {
		if got := rows(m); !reflect.DeepEqual(got, want) {
			t.Errorf("sort %s: %q, want %q", m.list.sort, got, want)
		}
		press(m, "j", "s")
		if m.list.cursor != 0 {
			t.Error("sorting must select the first row")
		}
	}
}

func TestSortIsStableAndComposesWithFilter(t *testing.T) {
	ms := []model.Match{
		{Name: "b", Similarity: 0.9, Popularity: ptr(uint64(5))},
		{Name: "a", Similarity: 0.8},
		{Name: "c", Similarity: 0.7, Popularity: ptr(uint64(5))},
		{Name: "d", Similarity: 0.6},
	}
	l := newList(ms)
	l.cycleSort()
	names := func() string {
		var s string
		for _, i := range l.rows {
			s += ms[i].Name
		}
		return s
	}
	if got := names(); got != "bcad" {
		t.Errorf("by popularity: %s, want ties in the ranker's order", got)
	}
	l.setFilter("a")
	if got := names(); got != "a" {
		t.Errorf("filtered: %s", got)
	}
}

func TestFilterMatchesNameOrDescriptionIgnoringCase(t *testing.T) {
	m, _ := newModel(t, 100, 30, testResult(), nil)
	press(m, "/")
	typeText(m, "FKILL")
	if got := rows(m); !reflect.DeepEqual(got, []string{"fkill-cli"}) {
		t.Errorf("by name: %q", got)
	}
	if l := lineWith(t, screen(m), "Matches ("); !strings.Contains(l, "Matches (1 of 4) · filter: FKILL") {
		t.Errorf("title = %q", l)
	}
	press(m, "backspace", "backspace", "backspace", "backspace", "backspace")
	typeText(m, "specified")
	if got := rows(m); !reflect.DeepEqual(got, []string{"jkfran/killport"}) {
		t.Errorf("by description: %q", got)
	}
	// Enter keeps the filter, Esc clears it.
	press(m, "enter")
	if m.mode != modeNormal || len(rows(m)) != 1 {
		t.Errorf("mode %v, rows %q", m.mode, rows(m))
	}
	press(m, "esc")
	if len(rows(m)) != 4 || m.list.filter != "" {
		t.Errorf("rows %q, filter %q", rows(m), m.list.filter)
	}
	press(m, "/")
	typeText(m, "zzz")
	if s := screen(m); !strings.Contains(s, "No match has the filter text") {
		t.Errorf("an empty filter result must say so:\n%s", s)
	}
	press(m, "esc")
	if m.mode != modeNormal || len(rows(m)) != 4 {
		t.Errorf("Esc in the filter must clear it: mode %v, rows %q", m.mode, rows(m))
	}
}

func TestNavigationWrapsAndPages(t *testing.T) {
	m, _ := newModel(t, 100, 30, testResult(), nil)
	press(m, "k")
	if selectedName(m) != "Port-Kill" {
		t.Errorf("up from the first row must wrap to the last, got %s", selectedName(m))
	}
	press(m, "j")
	if selectedName(m) != "kill-port" {
		t.Errorf("down from the last row must wrap to the first, got %s", selectedName(m))
	}
	press(m, "G")
	if selectedName(m) != "Port-Kill" {
		t.Errorf("G: %s", selectedName(m))
	}
	press(m, "g")
	if selectedName(m) != "kill-port" {
		t.Errorf("g: %s", selectedName(m))
	}
	press(m, "pgdown")
	if selectedName(m) != "Port-Kill" {
		t.Errorf("a page down past the end stops at the last row: %s", selectedName(m))
	}
}

func manyMatches(n int) []model.Match {
	var ms []model.Match
	for i := range n {
		ms = append(ms, model.Match{Name: fmt.Sprintf("match-%02d", i), Source: model.Npm,
			URL: fmt.Sprintf("https://example.com/%d", i), Similarity: 0.9 - float32(i)/100})
	}
	return ms
}

func TestTheSelectedRowStaysOnScreen(t *testing.T) {
	m, _ := newModel(t, 100, 30, &pipeline.Result{Verdict: testVerdict(0), Matches: manyMatches(50)}, nil)
	for _, keys := range [][]string{{"G"}, {"g"}, {"pgdown"}, {"pgdown", "pgdown"}, {"k"}, {"j"}} {
		press(m, keys...)
		if s := screen(m); !strings.Contains(s, selectedName(m)) {
			t.Errorf("after %q the selected %s is not on screen:\n%s", keys, selectedName(m), s)
		}
	}
}

func TestScrollbarDoesNotMoveWithinViewport(t *testing.T) {
	m, _ := newModel(t, 100, 30, &pipeline.Result{Verdict: testVerdict(0), Matches: manyMatches(50)}, nil)
	before := screen(m)
	press(m, "j", "j")
	if m.list.offset != 0 {
		t.Errorf("moving within the viewport scrolled to %d", m.list.offset)
	}
	bar := func(s string) string {
		var b strings.Builder
		for _, l := range strings.Split(s, "\n") {
			if strings.HasPrefix(l, " 0.") {
				r := []rune(l)
				b.WriteRune(r[len(r)-1])
			}
		}
		return b.String()
	}
	if bar(screen(m)) != bar(before) || !strings.Contains(bar(before), "┃") {
		t.Errorf("scrollbar %q became %q", bar(before), bar(screen(m)))
	}
	press(m, "G")
	if b := bar(screen(m)); !strings.HasSuffix(b, "┃") || strings.HasPrefix(b, "┃") {
		t.Errorf("at the end the thumb must be at the bottom: %q", b)
	}
}

func TestNameColumnNotTruncatedAt100Wide(t *testing.T) {
	m, _ := newModel(t, 100, 30, testResult(), nil)
	if s := screen(m); !strings.Contains(s, "jkfran/killport") {
		t.Errorf("names this short must fit:\n%s", s)
	}
}

func TestTableColumnsFit(t *testing.T) {
	for w := 20; w <= 220; w++ {
		c := tableColumns(w, 30)
		total := 1 + scoreWidth + 2 + c.name + 2 + c.source + 1
		for _, n := range []int{c.popularity, c.updated, c.description} {
			if n > 0 {
				total += n + 2
			}
		}
		if w >= 40 && total > w {
			t.Errorf("width %d: columns %+v take %d", w, c, total)
		}
	}
	if c := tableColumns(80, 30); c.updated != 0 || c.popularity == 0 || c.description == 0 {
		t.Errorf("80 wide: %+v", c)
	}
	if c := tableColumns(120, 30); c.updated == 0 || c.description == 0 {
		t.Errorf("120 wide: %+v", c)
	}
}

func TestCompact(t *testing.T) {
	for n, want := range map[uint64]string{0: "0", 999: "999", 1000: "1k", 1200: "1.2k", 98_000: "98k", 2_100_000: "2.1M", 3_000_000_000: "3B"} {
		if got := compact(n); got != want {
			t.Errorf("compact(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestEmptyResultsSayWhereNothingTurnedUp(t *testing.T) {
	m, _ := newModel(t, 100, 30, &pipeline.Result{Verdict: testVerdict(0)}, nil)
	if s := screen(m); !strings.Contains(s, "No matches turned up in the sources checked.") {
		t.Errorf("screen:\n%s", s)
	}
	press(m, "enter", "j", "o", "y")
	if m.mode != modeNormal {
		t.Errorf("mode = %v", m.mode)
	}
}

// -- Details, opening and copying -----------------------------------------------------

func TestDetailPopupShowsFullMatchInfo(t *testing.T) {
	m, _ := newModel(t, 120, 40, testResult(), nil)
	press(m, "j", "j", "enter")
	s := squash(screen(m))
	for _, want := range []string{
		"jkfran/killport", "GitHub", "similarity 0.75", "popularity 1.2k",
		"A command-line tool to easily kill processes running on a specified port.",
		"↳ https://github.com/jkfran/killport",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the details lack %q:\n%s", want, screen(m))
		}
	}
}

func TestDetailPopupShowsHowLongAgoItWasUpdated(t *testing.T) {
	m, _ := newModel(t, 120, 40, testResult(), nil)
	press(m, "enter")
	if s := screen(m); !strings.Contains(s, "updated 7 years ago "+staleMark) {
		t.Errorf("screen:\n%s", s)
	}
}

func TestDetailPopupOmitsTheAgeWhenTheSourcePublishesNoDate(t *testing.T) {
	m, _ := newModel(t, 120, 40, testResult(), nil)
	press(m, "j", "j", "enter")
	if l := lineWith(t, screen(m), "similarity 0.75"); strings.Contains(l, "updated") {
		t.Errorf("an undated match must claim nothing about its age: %q", l)
	}
}

func TestDetailPopupNoPopularityShowsDash(t *testing.T) {
	m, _ := newModel(t, 120, 40, testResult(), nil)
	press(m, "G", "enter")
	if s := screen(m); !strings.Contains(s, "popularity —") {
		t.Errorf("screen:\n%s", s)
	}
}

func TestDetailPopupScrollsALongDescription(t *testing.T) {
	res := testResult()
	res.Matches[0].Description = strings.Repeat("word ", 400) + "THE-END"
	m, _ := newModel(t, 80, 24, res, nil)
	press(m, "enter")
	if s := screen(m); strings.Contains(s, "THE-END") || !strings.Contains(s, "↑↓ to scroll") {
		t.Errorf("a long description starts at its top:\n%s", s)
	}
	for range 200 {
		press(m, "j")
	}
	if s := screen(m); !strings.Contains(s, "THE-END") {
		t.Errorf("scrolling must reach the end:\n%s", s)
	}
	// Overscrolling is clamped, so one step up moves at once.
	end := m.detailScroll
	press(m, "k")
	if m.detailScroll != end-1 {
		t.Errorf("scroll %d after k, want %d", m.detailScroll, end-1)
	}
	press(m, "esc", "enter")
	if m.detailScroll != 0 {
		t.Error("reopened details must start at the top")
	}
}

func TestOpenOnlyOpensWebLinks(t *testing.T) {
	res := testResult()
	res.Matches[1].URL = "javascript:alert(1)"
	m, f := newModel(t, 100, 30, res, nil)
	run(m, press(m, "o"))
	if !reflect.DeepEqual(f.opened, []string{"https://www.npmjs.com/package/kill-port"}) || m.status != "Opened in the browser" {
		t.Errorf("opened %q, status %q", f.opened, m.status)
	}
	press(m, "j", "o")
	if len(f.opened) != 1 || m.status != "Only http and https links are opened" {
		t.Errorf("opened %q, status %q", f.opened, m.status)
	}
	// From the details too.
	run(m, press(m, "k", "enter", "enter"))
	if len(f.opened) != 2 {
		t.Errorf("opened %q", f.opened)
	}
}

func TestIsSafeURLAllowsHTTPSAndHTTPOnly(t *testing.T) {
	for url, want := range map[string]bool{
		"https://crates.io/crates/x": true, "HTTP://example.com": true,
		"javascript:alert(1)": false, "file:///etc/passwd": false, "": false, "   ": false,
	} {
		if got := isSafeURL(url); got != want {
			t.Errorf("isSafeURL(%q) = %v", url, got)
		}
	}
}

// run runs cmd, which must answer at once, and feeds its message back to m.
// What that returns, the status message's timer, is not run.
func run(m *Model, cmd tea.Cmd) {
	if cmd != nil {
		m.Update(cmd())
	}
}

func TestYankCopiesTheURL(t *testing.T) {
	m, f := newModel(t, 100, 30, testResult(), nil)
	run(m, press(m, "y"))
	if !reflect.DeepEqual(f.copied, []string{"https://www.npmjs.com/package/kill-port"}) || m.status != "Copied the URL to the clipboard" {
		t.Errorf("copied %q, status %q", f.copied, m.status)
	}
	if s := screen(m); !strings.Contains(s, "Copied the URL to the clipboard") {
		t.Errorf("the status must show in the footer:\n%s", s)
	}
}

func TestYankFallsBackToTheTerminalClipboard(t *testing.T) {
	m, f := newModel(t, 100, 30, testResult(), nil)
	f.copyErr = errors.New("no xclip")
	msg := press(m, "y")()
	if msg != osc52Msg("https://www.npmjs.com/package/kill-port") {
		t.Fatalf("msg = %#v", msg)
	}
	m.Update(msg)
	if m.status != "Sent the URL to the terminal's clipboard (OSC 52)" {
		t.Errorf("status = %q", m.status)
	}
}

func TestStatusExpires(t *testing.T) {
	m, _ := newModel(t, 100, 30, testResult(), nil)
	m.setStatus("first")
	old := m.statusID
	m.setStatus("second")
	m.Update(clearStatusMsg(old))
	if m.status != "second" {
		t.Error("an older timer must not clear a newer status")
	}
	m.Update(clearStatusMsg(m.statusID))
	if m.status != "" {
		t.Errorf("status = %q", m.status)
	}
}

// -- The mouse --------------------------------------------------------------------

func TestClickSelectsARowAndAgainOpensIt(t *testing.T) {
	m, _ := newModel(t, 100, 30, testResult(), nil)
	l := m.resultsLayout()
	click := tea.MouseClickMsg{X: 10, Y: l.firstRowY + 2, Button: tea.MouseLeft}
	m.Update(click)
	if selectedName(m) != "jkfran/killport" || m.mode != modeNormal {
		t.Errorf("selected %s, mode %v", selectedName(m), m.mode)
	}
	m.Update(click)
	if m.mode != modeDetail {
		t.Error("clicking the selected row must open its details")
	}
	// Clicks above the table or below the last row select nothing.
	press(m, "esc")
	for _, y := range []int{0, l.firstRowY - 1, l.firstRowY + 10} {
		m.Update(tea.MouseClickMsg{X: 10, Y: y, Button: tea.MouseLeft})
		if selectedName(m) != "jkfran/killport" || m.mode != modeNormal {
			t.Errorf("click at %d: selected %s, mode %v", y, selectedName(m), m.mode)
		}
	}
}

func TestWheelScrolls(t *testing.T) {
	m, _ := newModel(t, 100, 30, testResult(), nil)
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if selectedName(m) != "fkill-cli" {
		t.Errorf("selected %s", selectedName(m))
	}
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if selectedName(m) != "kill-port" {
		t.Errorf("the wheel stops at the top: %s", selectedName(m))
	}
}

// -- Help, quitting --------------------------------------------------------------------

func TestHelpOverlayListsNewSearch(t *testing.T) {
	m, _ := newModel(t, 100, 40, testResult(), nil)
	press(m, "?")
	s := screen(m)
	for _, want := range []string{"new search", "copy its URL", staleMark + "  no update in 2+ years, where the source reports a date"} {
		if !strings.Contains(s, want) {
			t.Errorf("help lacks %q:\n%s", want, s)
		}
	}
	press(m, "?")
	if m.mode != modeNormal {
		t.Error("? must close the help")
	}
}

func TestResultsFooterShowsNewSearchKey(t *testing.T) {
	m, _ := newModel(t, 100, 30, testResult(), nil)
	if l := lineWith(t, screen(m), "details"); !strings.Contains(l, "n new search") || !strings.Contains(l, "q quit") {
		t.Errorf("footer = %q", l)
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestQuitKeys(t *testing.T) {
	m, _ := newModel(t, 100, 30, testResult(), nil)
	if !isQuit(press(m, "q")) {
		t.Error("q must quit")
	}
	m, _ = newModel(t, 100, 30, testResult(), nil)
	if cmd := press(m, "/", "q"); isQuit(cmd) || m.list.filter != "q" {
		t.Error("q in the filter is text")
	}
	for _, keys := range [][]string{{"ctrl+c"}, {"/", "ctrl+c"}, {"enter", "ctrl+c"}, {"n", "ctrl+c"}} {
		m, _ = newModel(t, 100, 30, testResult(), nil)
		if !isQuit(press(m, keys...)) {
			t.Errorf("%q must quit", keys)
		}
	}
}

// -- The prompt and the search -------------------------------------------------------

func TestSearchScreenShowsPlaceholderWhenEmpty(t *testing.T) {
	m, _ := newModel(t, 100, 30, nil, nil)
	if s := screen(m); !strings.Contains(s, "describe your dev-tool idea…") {
		t.Errorf("screen:\n%s", s)
	}
}

func TestSearchScreenShowsTypedInput(t *testing.T) {
	m, _ := newModel(t, 100, 30, nil, nil)
	typeText(m, "kubernetes log viewer")
	if s := screen(m); !strings.Contains(s, "kubernetes log viewer") || strings.Contains(s, "describe your") {
		t.Errorf("screen:\n%s", s)
	}
}

func TestSearchScreenShowsKeybindingHints(t *testing.T) {
	m, _ := newModel(t, 100, 30, nil, nil)
	s := screen(m)
	for _, want := range []string{"Enter search", "Esc quit", "Verdicts by qwen3.5 on the local Ollama."} {
		if !strings.Contains(s, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, s)
		}
	}
}

func TestSearchRejectsAVagueIdea(t *testing.T) {
	m, f := newModel(t, 100, 30, nil, nil)
	typeText(m, "hi")
	press(m, "enter")
	if len(f.searches) != 0 || m.phase != phaseSearch {
		t.Fatalf("searched %q", f.searches)
	}
	if s := screen(m); !strings.Contains(s, "Too vague") {
		t.Errorf("the prompt must say why:\n%s", s)
	}
	typeText(m, " there")
	if m.inputErr != "" {
		t.Error("editing the idea clears the error")
	}
	if cmd := press(m, "enter"); cmd != nil || len(f.searches) != 0 {
		t.Error("still too vague")
	}
	m.input.SetValue("   ")
	if cmd := press(m, "enter"); cmd != nil || len(f.searches) != 0 {
		t.Error("a blank prompt does nothing")
	}
}

func TestEscOnThePromptQuitsOrGoesBack(t *testing.T) {
	m, _ := newModel(t, 100, 30, nil, nil)
	if !isQuit(press(m, "esc")) {
		t.Error("without results, Esc quits")
	}
	m, _ = newModel(t, 100, 30, testResult(), nil)
	press(m, "n")
	if m.phase != phaseSearch || m.input.Value() != "cli tool that kills a process on a given port" {
		t.Errorf("n opens the prompt on the current idea: %v %q", m.phase, m.input.Value())
	}
	if s := screen(m); !strings.Contains(s, "Esc back to the results") {
		t.Errorf("screen:\n%s", s)
	}
	press(m, "esc")
	if m.phase != phaseResults || len(rows(m)) != 4 {
		t.Errorf("Esc returns to the results: %v", m.phase)
	}
}

// startSearch types idea and submits it, and returns the command that waits
// for the search's first message.
func submit(t *testing.T, m *Model, idea string) tea.Cmd {
	t.Helper()
	m.input.SetValue(idea)
	cmd := press(m, "enter")
	if m.phase != phaseSearching {
		t.Fatalf("phase = %v, inputErr %q", m.phase, m.inputErr)
	}
	return cmd().(tea.BatchMsg)[0]
}

func TestSearchingScreenShowsSpinnerProgressAndIdea(t *testing.T) {
	release := make(chan struct{})
	m, f := newModel(t, 100, 30, nil, func(ctx context.Context, idea string, log func(string), progress rank.Progress) (pipeline.Result, error) {
		log("   96 matches from 6 sources in 1.9s: crates.io, GitHub")
		progress("model.onnx", 45_000_000, 90_000_000)
		<-release
		return *testResult(), nil
	})
	next := submit(t, m, "kubernetes log viewer with live filtering")
	for range 2 {
		_, next = m.Update(next())
	}
	s := screen(m)
	for _, want := range []string{"Searching…", "kubernetes log viewer with live filtering",
		"96 matches from 6 sources in 1.9s", "downloading model.onnx: 50% of 90.0 MB", "Esc cancel", "Ctrl+C quit"} {
		if !strings.Contains(s, want) {
			t.Errorf("the progress screen lacks %q:\n%s", want, s)
		}
	}
	close(release)
	m.Update(next())
	if m.phase != phaseResults || m.idea != "kubernetes log viewer with live filtering" || len(rows(m)) != 4 {
		t.Errorf("phase %v, idea %q, rows %q", m.phase, m.idea, rows(m))
	}
	if !reflect.DeepEqual(f.searches, []string{"kubernetes log viewer with live filtering"}) {
		t.Errorf("searches = %q", f.searches)
	}
}

func TestSearchingScreenSpinnerChangesWithTick(t *testing.T) {
	m, _ := newModel(t, 100, 30, nil, nil)
	submit(t, m, "kubernetes log viewer with live filtering")
	before := screen(m)
	m.Update(m.spinner.Tick())
	if screen(m) == before {
		t.Error("the spinner must move")
	}
}

func TestEscCancelsTheSearch(t *testing.T) {
	canceled := make(chan struct{})
	m, _ := newModel(t, 100, 30, nil, func(ctx context.Context, _ string, _ func(string), _ rank.Progress) (pipeline.Result, error) {
		<-ctx.Done()
		close(canceled)
		return *testResult(), nil
	})
	next := submit(t, m, "kubernetes log viewer with live filtering")
	press(m, "esc")
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("Esc must cancel the pipeline")
	}
	if m.phase != phaseSearch || m.input.Value() != "kubernetes log viewer with live filtering" {
		t.Errorf("back on the prompt with the idea: %v %q", m.phase, m.input.Value())
	}
	// The canceled search sends nothing more, and a stale outcome is
	// ignored.
	done := make(chan tea.Msg, 1)
	go func() { done <- next() }()
	select {
	case msg := <-done:
		t.Errorf("a canceled search sent %#v", msg)
	case <-time.After(50 * time.Millisecond):
	}
	m.Update(doneMsg{id: m.searchID - 1, result: *testResult()})
	if m.phase != phaseSearch {
		t.Error("a stale outcome must be ignored")
	}
}

func TestAFailedSearchReturnsToThePrompt(t *testing.T) {
	m, _ := newModel(t, 100, 30, nil, func(context.Context, string, func(string), rank.Progress) (pipeline.Result, error) {
		return pipeline.Result{}, errors.New("embedding failed: no network")
	})
	next := submit(t, m, "kubernetes log viewer with live filtering")
	m.Update(next())
	if m.phase != phaseSearch || m.hasResults {
		t.Errorf("phase %v", m.phase)
	}
	if s := screen(m); !strings.Contains(s, "Search failed: embedding failed: no network") ||
		!strings.Contains(s, "kubernetes log viewer with live filtering") {
		t.Errorf("the prompt must keep the idea and say what failed:\n%s", s)
	}
}

func TestAPanickingSearchFailsInsteadOfCrashing(t *testing.T) {
	m, _ := newModel(t, 100, 30, nil, func(context.Context, string, func(string), rank.Progress) (pipeline.Result, error) {
		panic("index out of range")
	})
	next := submit(t, m, "kubernetes log viewer with live filtering")
	m.Update(next())
	if s := screen(m); m.phase != phaseSearch || !strings.Contains(s, "Search failed: the search crashed: index out of range") {
		t.Errorf("screen:\n%s", s)
	}
}

func TestANewSearchReplacesTheResults(t *testing.T) {
	m, _ := newModel(t, 100, 30, testResult(), func(context.Context, string, func(string), rank.Progress) (pipeline.Result, error) {
		return pipeline.Result{Verdict: model.Verdict{Level: model.Open, Headline: "Nothing close.", Caveat: verdict.Caveat,
			SourcesChecked: []model.Source{model.Npm}}, Matches: manyMatches(3)}, nil
	})
	press(m, "/")
	typeText(m, "kill")
	press(m, "enter", "s", "n")
	m.input.SetValue("")
	next := submit(t, m, "a second idea about log viewers")
	m.Update(next())
	if m.idea != "a second idea about log viewers" || m.verdict.Level != model.Open || m.list.filter != "" ||
		m.list.sort != bySimilarity || m.list.cursor != 0 || len(rows(m)) != 3 {
		t.Errorf("idea %q, level %v, filter %q, sort %v, rows %q", m.idea, m.verdict.Level, m.list.filter, m.list.sort, rows(m))
	}
}

func TestCtrlCCancelsARunningSearch(t *testing.T) {
	canceled := make(chan struct{})
	m, _ := newModel(t, 100, 30, nil, func(ctx context.Context, _ string, _ func(string), _ rank.Progress) (pipeline.Result, error) {
		<-ctx.Done()
		close(canceled)
		return pipeline.Result{}, ctx.Err()
	})
	submit(t, m, "kubernetes log viewer with live filtering")
	if !isQuit(press(m, "ctrl+c")) {
		t.Error("Ctrl+C must quit")
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("quitting must cancel the pipeline")
	}
}

// -- The list ----------------------------------------------------------------------

func TestListMoveAndScroll(t *testing.T) {
	l := newList(manyMatches(10))
	l.move(-1, true)
	if l.cursor != 9 {
		t.Errorf("wrap up: %d", l.cursor)
	}
	l.move(3, false)
	if l.cursor != 9 {
		t.Errorf("no wrap: %d", l.cursor)
	}
	l.scrollTo(4)
	if l.offset != 6 {
		t.Errorf("offset %d, want the last 4 rows", l.offset)
	}
	l.top()
	l.scrollTo(4)
	if l.offset != 0 {
		t.Errorf("offset %d", l.offset)
	}
	if !l.selectRow(2) || l.cursor != 2 || l.selectRow(20) || l.selectRow(-1) {
		t.Errorf("selectRow: cursor %d", l.cursor)
	}
	empty := newList(nil)
	empty.move(1, true)
	empty.bottom()
	if _, ok := empty.selected(); ok || empty.cursor != 0 {
		t.Error("an empty list selects nothing")
	}
}
