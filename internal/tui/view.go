package tui

import (
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miku/silt/internal/freshness"
	"github.com/miku/silt/internal/model"
	"github.com/miku/silt/internal/verdict"
)

var (
	accent = lipgloss.Cyan
	muted  = lipgloss.BrightBlack
	stale  = lipgloss.Yellow

	accentStyle = lipgloss.NewStyle().Foreground(accent)
	accentBold  = accentStyle.Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(muted)
	boldStyle   = lipgloss.NewStyle().Bold(true)
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Red)
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Green).Bold(true)
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1)
)

// staleMark flags a match whose source reports no update in
// freshness.StaleAfterYears. It is single-width, so columns stay aligned.
const staleMark = "⚑"

func levelIcon(l model.Saturation) string {
	return [...]string{"🟢", "🟡", "🔴"}[l]
}

func levelColor(l model.Saturation) color.Color {
	return [...]color.Color{lipgloss.Green, lipgloss.Yellow, lipgloss.Red}[l]
}

// scoreColor colours a similarity by the verdict's thresholds, so that the
// table shows which matches the headline counts: yellow for a close match,
// green for one close enough to make a space crowded on its own, and muted
// for the rest.
func scoreColor(sim float32) color.Color {
	switch {
	case sim >= verdict.VeryStrong:
		return lipgloss.Green
	case sim >= verdict.Close:
		return lipgloss.Yellow
	}
	return muted
}

var sourceColors = map[model.Source]color.Color{
	model.CratesIO:          lipgloss.Yellow,
	model.GitHub:            lipgloss.White,
	model.Npm:               lipgloss.Red,
	model.PyPI:              lipgloss.Blue,
	model.HackerNews:        lipgloss.Color("#ff6600"),
	model.Go:                lipgloss.Cyan,
	model.Maven:             lipgloss.Color("#c83232"),
	model.RubyGems:          lipgloss.Magenta,
	model.DockerHub:         lipgloss.Color("#1e90ff"),
	model.VSCodeMarketplace: lipgloss.Color("#007acc"),
	model.JetBrains:         lipgloss.Color("#ff318c"),
	model.NuGet:             lipgloss.Color("#642daa"),
	model.Homebrew:          lipgloss.Color("#faaf05"),
	model.Packagist:         lipgloss.Color("#777bb4"),
	model.Hex:               lipgloss.Color("#6e4a7e"),
	model.ArtifactHub:       lipgloss.Color("#417598"),
	model.AUR:               lipgloss.Color("#1793d1"),
	model.Hackage:           lipgloss.Color("#5e5086"),
	model.Nixpkgs:           lipgloss.Color("#7ebae4"),
	model.Codeberg:          lipgloss.Color("#2185d0"),
	model.ArchLinux:         lipgloss.Color("#1793d1"), // Arch blue, as the AUR
}

func sourceStyle(s model.Source) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(sourceColors[s])
}

func key(s string) string { return accentBold.Render(s) }

// hints renders pairs of keys and what they do, e.g. hints("q", "quit").
func hints(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(key(pairs[i]) + dimStyle.Render(" "+pairs[i+1]))
	}
	return b.String()
}

// clean makes text from registries and models safe and single-line for the
// terminal: escape sequences are dropped, other control characters become
// spaces, and runs of whitespace collapse.
func clean(s string) string {
	return strings.Join(strings.FieldsFunc(ansi.Strip(s), func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}), " ")
}

// wrap word-wraps plain text to width cells, breaking words that are longer
// than a line, and prefixes the first line with first and the others with
// rest, which must be as wide.
func wrap(text string, width int, first, rest string, style lipgloss.Style) []string {
	w := max(1, width-ansi.StringWidth(first))
	lines := strings.Split(ansi.Wrap(text, w, ""), "\n")
	for i, l := range lines {
		prefix := rest
		if i == 0 {
			prefix = first
		}
		lines[i] = prefix + style.Render(strings.TrimRight(l, " "))
	}
	return lines
}

// pack lays out items after label, separated by dots, starting a new line
// whenever the next item would not fit in width cells.
func pack(label string, items []string, itemStyle func(int) lipgloss.Style, width int) []string {
	const sep = " · "
	indent := strings.Repeat(" ", ansi.StringWidth(label))
	var lines []string
	line, w := label, ansi.StringWidth(label)
	for i, item := range items {
		iw := ansi.StringWidth(item)
		if i > 0 {
			if w+len([]rune(sep))+iw > width {
				lines = append(lines, line)
				line, w = indent, len(indent)
			} else {
				line += dimStyle.Render(sep)
				w += len([]rune(sep))
			}
		}
		line += itemStyle(i).Render(item)
		w += iw
	}
	return append(lines, line)
}

func sourceNames(ss []model.Source) []string {
	names := make([]string, len(ss))
	for i, s := range ss {
		names[i] = s.String()
	}
	return names
}

func rule(width int) string { return mutedStyle.Render(strings.Repeat("─", width)) }

// titledRule is a rule with title near its left end.
func titledRule(title string, width int) string {
	title = ansi.Truncate(" "+title+" ", max(0, width-2), "…")
	return mutedStyle.Render("─") + boldStyle.Render(title) +
		mutedStyle.Render(strings.Repeat("─", max(0, width-1-ansi.StringWidth(title))))
}

// fit makes s exactly height lines of at most width cells.
func fit(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "")
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	var s string
	switch m.phase {
	case phaseSearch:
		s = m.searchView()
	case phaseSearching:
		s = m.searchingView()
	default:
		s = m.resultsView()
	}
	return fit(s, m.width, m.height)
}

// -- The search prompt and the progress screen --------------------------------

// promptWidth is the width of the search prompt's box.
func promptWidth(width int) int { return max(10, min(width-4, 72)) }

func (m *Model) searchView() string {
	w := promptWidth(m.width)
	quit := "quit"
	if m.hasResults {
		quit = "back to the results"
	}
	lines := []string{
		accentBold.Render("silt") + dimStyle.Render("  prior-art search for code ideas"),
		"",
		boxStyle.Width(w).Render(m.input.View()),
		hints("Enter", "search", "Esc", quit),
	}
	if m.inputErr != "" {
		lines = append(lines, "")
		for _, para := range strings.Split(m.inputErr, "\n") {
			lines = append(lines, wrap(clean(para), w, "", "", errorStyle)...)
		}
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// progressLines is how many of the pipeline's progress lines the progress
// screen shows.
const progressLines = 8

func (m *Model) searchingView() string {
	w := promptWidth(m.width)
	lines := []string{m.spinner.View() + " " + boldStyle.Render("Searching…"), ""}
	lines = append(lines, wrap(clean(m.searchIdea), w, "  ", "  ", accentStyle)...)
	lines = append(lines, "")
	for _, l := range m.logLines[max(0, len(m.logLines)-progressLines):] {
		lines = append(lines, dimStyle.Render("  "+ansi.Truncate(clean(l), w-2, "…")))
	}
	if m.download != "" {
		lines = append(lines, dimStyle.Render("  "+ansi.Truncate(m.download, w-2, "…")))
	}
	lines = append(lines, "", hints("Esc", "cancel", "Ctrl+C", "quit"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// -- The results ---------------------------------------------------------------

// resultsLayout is how the results screen divides its height.
type resultsLayout struct {
	// top are the header and verdict lines, in full.
	top []string
	// tooSmall is set when the verdict can't be shown in full; no verdict is
	// shown at all then.
	tooSmall bool
	// tableY is the line of the table's title, firstRowY that of its first
	// match, and tableRows how many matches fit; 0 if there is no table.
	tableY, firstRowY, tableRows int
}

// footerHeight is the status and hints line.
const footerHeight = 1

// minWidth is the narrowest results screen: enough for the label and the
// longest name on the "Not reached" line.
const minWidth = 30

// resultsLayout gives the verdict all the room it needs, and the table what
// is left. On a short terminal the idea and the sources
// checked go first; the sources that were not reached stay with the
// verdict.
func (m *Model) resultsLayout() resultsLayout {
	if m.width < minWidth {
		return resultsLayout{tooSmall: true}
	}
	header, notReached, verdict := m.headerLines(), m.notReachedLines(), m.verdictLines()
	var l resultsLayout
	switch {
	case len(header)+len(notReached)+1+len(verdict)+footerHeight <= m.height:
		l.top = slices.Concat(header, notReached, []string{rule(m.width)}, verdict)
	case len(notReached)+len(verdict)+footerHeight <= m.height:
		l.top = slices.Concat(notReached, verdict)
	default:
		return resultsLayout{tooSmall: true}
	}
	// The table needs its title, the column names and at least one row.
	if rest := m.height - len(l.top) - footerHeight; rest >= 3 {
		l.tableY = len(l.top)
		l.firstRowY = l.tableY + 2
		l.tableRows = rest - 2
	}
	return l
}

func (m *Model) headerLines() []string {
	lines := wrap(clean(m.idea), m.width-1, " ", " ", accentBold)
	label := dimStyle.Render(" Sources: ")
	if len(m.verdict.SourcesChecked) == 0 {
		return append(lines, label+dimStyle.Render("none reached"))
	}
	return append(lines, pack(label, sourceNames(m.verdict.SourcesChecked), func(i int) lipgloss.Style {
		return sourceStyle(m.verdict.SourcesChecked[i])
	}, m.width)...)
}

// notReachedLines name the sources that were selected but failed, so that a
// thin result is never mistaken for "nothing out there".
func (m *Model) notReachedLines() []string {
	if len(m.verdict.SourcesFailed) == 0 {
		return nil
	}
	return pack(errorStyle.Render(" Not reached: "), sourceNames(m.verdict.SourcesFailed),
		func(int) lipgloss.Style { return mutedStyle }, m.width)
}

func (m *Model) verdictLines() []string {
	v, w := m.verdict, m.width-1
	level := lipgloss.NewStyle().Foreground(levelColor(v.Level)).Bold(true)
	lines := []string{" " + level.Render(levelIcon(v.Level)+" "+v.Level.String())}
	return append(lines, wrap(clean(v.Headline), w, " ", " ", boldStyle)...)
}

func (m *Model) resultsView() string {
	l := m.resultsLayout()
	if l.tooSmall {
		msg := strings.Join(wrap("Enlarge the terminal to see the verdict.", m.width, "", "", dimStyle), "\n")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}
	lines := l.top
	if l.tableRows > 0 {
		lines = append(lines, m.tableLines(l.tableRows)...)
	}
	for len(lines) < m.height-footerHeight {
		lines = append(lines, "")
	}
	base := strings.Join(append(lines, m.footer()), "\n")
	switch m.mode {
	case modeHelp:
		return overlay(fit(base, m.width, m.height), m.helpPopup(), m.width, m.height)
	case modeDetail:
		return overlay(fit(base, m.width, m.height), m.detailPopup(), m.width, m.height)
	}
	return base
}

// overlay draws popup over the middle of base.
func overlay(base, popup string, width, height int) string {
	x := max(0, (width-lipgloss.Width(popup))/2)
	y := max(0, (height-lipgloss.Height(popup))/2)
	return lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(popup).X(x).Y(y).Z(1)).Render()
}

func (m *Model) footer() string {
	if m.status != "" {
		return statusStyle.Render(" " + m.status)
	}
	switch m.mode {
	case modeFilter:
		return " " + accentBold.Render("/") + m.list.filter + accentStyle.Render("▏") + "   " +
			hints("Enter", "keep", "Esc", "clear")
	case modeHelp:
		return " " + hints("?", "close help")
	case modeDetail:
		return " " + hints("↑↓", "scroll", "o", "open", "y", "copy", "Esc", "close")
	}
	return " " + hints("Enter", "details", "o", "open", "y", "copy", "/", "filter", "s", "sort",
		"n", "new search", "?", "help", "q", "quit")
}

// columns are the widths of the table's columns; 0 hides one.
type columns struct {
	name, source, popularity, updated, description int
}

// scoreWidth is the width of the score column, as wide as its heading.
const scoreWidth = 5

// tableColumns fits the columns into width, dropping the age and then the
// popularity on narrow terminals, and giving the name up to 2/5 of the rest.
func tableColumns(width, longestName int) columns {
	c := columns{source: 12, popularity: 6, updated: 13}
	room := func() int {
		// The left margin, the score, the source, the gaps and the
		// scrollbar.
		n := 1 + scoreWidth + 2 + c.source + 2 + 1
		if c.popularity > 0 {
			n += c.popularity + 2
		}
		if c.updated > 0 {
			n += c.updated + 2
		}
		return width - n
	}
	if room() < 40 {
		c.updated = 0
	}
	if room() < 30 {
		c.popularity = 0
	}
	r := room()
	c.name = max(1, min(longestName, max(16, r*2/5)))
	if c.description = r - c.name - 2; c.description < 10 {
		c.name, c.description = max(1, r), 0
	}
	return c
}

func (m *Model) tableLines(rows int) []string {
	l := &m.list
	title := fmt.Sprintf("Matches (%d)", len(l.matches))
	if l.filter != "" {
		title = fmt.Sprintf("Matches (%d of %d) · filter: %s", len(l.rows), len(l.matches), clean(l.filter))
	}
	title += " · sort: " + l.sort.String()
	longest := 0
	for _, match := range l.matches {
		longest = max(longest, ansi.StringWidth(clean(match.Name))+2)
	}
	c := tableColumns(m.width, longest)

	head := []string{padRight("Score", scoreWidth), padRight("Name", c.name), padRight("Source", c.source)}
	if c.popularity > 0 {
		head = append(head, padLeft("Pop.", c.popularity))
	}
	if c.updated > 0 {
		head = append(head, padRight("Updated", c.updated))
	}
	if c.description > 0 {
		head = append(head, "Description")
	}
	lines := []string{titledRule(title, m.width), accentBold.Render(" " + strings.Join(head, "  "))}

	if len(l.rows) == 0 {
		msg := "No matches turned up in the sources checked."
		if l.filter != "" {
			msg = "No match has the filter text in its name or description."
		}
		return append(lines, dimStyle.Render(" "+msg))
	}
	bar := scrollbar(len(l.rows), rows, l.offset)
	for i := range min(rows, len(l.rows)-l.offset) {
		row := l.offset + i
		lines = append(lines, m.rowLine(l.matches[l.rows[row]], c, row == l.cursor)+bar[i])
	}
	return lines
}

// scrollbar returns one character per viewport row: a thumb where the
// viewport sits within total rows, or blanks if everything fits.
func scrollbar(total, viewport, offset int) []string {
	bar := make([]string, viewport)
	if total <= viewport {
		for i := range bar {
			bar[i] = " "
		}
		return bar
	}
	size := max(1, viewport*viewport/total)
	pos := offset * (viewport - size) / (total - viewport)
	for i := range bar {
		if i >= pos && i < pos+size {
			bar[i] = accentStyle.Render("┃")
		} else {
			bar[i] = mutedStyle.Render("│")
		}
	}
	return bar
}

// age is how long ago match was updated, if its source reports a date.
func (m *Model) age(match model.Match) (freshness.Age, bool) {
	if match.LastUpdated == nil {
		return freshness.Age{}, false
	}
	return freshness.AgeOf(*match.LastUpdated, m.now())
}

// rowLine renders a match as a table row of width-1 cells, leaving the last
// column to the scrollbar.
func (m *Model) rowLine(match model.Match, c columns, selected bool) string {
	style := func(s lipgloss.Style) lipgloss.Style { return s.Reverse(selected) }
	age, dated := m.age(match)
	// Flagged, not demoted: a stale match keeps its rank and only gets a
	// marker. A match without a date is never flagged; most sources publish
	// none, and silence is not evidence of abandonment.
	isStale := dated && age.Stale
	name := clean(match.Name)
	nameStyle := boldStyle
	if isStale {
		name = ansi.Truncate(name, max(0, c.name-2), "…") + " " + staleMark
		nameStyle = nameStyle.Foreground(stale)
	}
	cells := []string{
		cell(fmt.Sprintf("%.2f", match.Similarity), scoreWidth, style(lipgloss.NewStyle().Foreground(scoreColor(match.Similarity)))),
		cell(name, c.name, style(nameStyle)),
		cell(match.Source.String(), c.source, style(sourceStyle(match.Source))),
	}
	if c.popularity > 0 {
		pop := "—"
		if match.Popularity != nil {
			pop = compact(*match.Popularity)
		}
		cells = append(cells, style(dimStyle).Render(padLeft(pop, c.popularity)))
	}
	if c.updated > 0 {
		label, ageStyle := "—", dimStyle
		if dated {
			label = age.Label
		}
		if isStale {
			ageStyle = lipgloss.NewStyle().Foreground(stale)
		}
		cells = append(cells, cell(label, c.updated, style(ageStyle)))
	}
	if c.description > 0 {
		cells = append(cells, cell(clean(match.Description), c.description, style(dimStyle)))
	}
	gap := style(lipgloss.NewStyle()).Render("  ")
	line := style(lipgloss.NewStyle()).Render(" ") + strings.Join(cells, gap)
	if pad := m.width - 1 - ansi.StringWidth(line); pad > 0 {
		line += style(lipgloss.NewStyle()).Render(strings.Repeat(" ", pad))
	}
	return ansi.Truncate(line, m.width-1, "")
}

// cell truncates text to width cells, pads it to width and styles it.
func cell(text string, width int, style lipgloss.Style) string {
	return style.Render(padRight(ansi.Truncate(text, width, "…"), width))
}

func padRight(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

func padLeft(s string, width int) string {
	return strings.Repeat(" ", max(0, width-ansi.StringWidth(s))) + s
}

// compact formats a popularity figure in at most 6 characters, e.g. 2.1M.
func compact(n uint64) string {
	for _, u := range []struct {
		div    float64
		suffix string
	}{{1e9, "B"}, {1e6, "M"}, {1e3, "k"}} {
		if float64(n) >= u.div {
			return strings.TrimSuffix(strconv.FormatFloat(float64(n)/u.div, 'f', 1, 64), ".0") + u.suffix
		}
	}
	return strconv.FormatUint(n, 10)
}

// -- The popups ----------------------------------------------------------------

// popupWidth is the outer width of a popup.
func (m *Model) popupWidth() int { return min(m.width, max(min(m.width, 44), min(100, m.width*3/4))) }

// detailLayout is the detail popup's content, with the description scrolled
// within viewport lines.
type detailLayout struct {
	head, description, tail []string
	viewport                int
}

func (m *Model) detailLayout() detailLayout {
	match, _ := m.list.selected()
	w := max(1, m.popupWidth()-4) // the border and the padding
	var d detailLayout
	d.head = wrap(clean(match.Name), w, "", "", boldStyle)
	score := lipgloss.NewStyle().Foreground(scoreColor(match.Similarity)).Bold(true)
	meta := []string{
		sourceStyle(match.Source).Render(match.Source.String()),
		score.Render(fmt.Sprintf("similarity %.2f", match.Similarity)),
	}
	if match.Popularity != nil {
		meta = append(meta, dimStyle.Render("popularity "+compact(*match.Popularity)))
	} else {
		meta = append(meta, dimStyle.Render("popularity —"))
	}
	// A source that publishes no date gets no age: an absent date is not a
	// staleness claim, so it must not read as one.
	if age, ok := m.age(match); ok {
		if age.Stale {
			meta = append(meta, lipgloss.NewStyle().Foreground(stale).Render("updated "+age.Label+" "+staleMark))
		} else {
			meta = append(meta, dimStyle.Render("updated "+age.Label))
		}
	}
	d.head = append(d.head, pack("", meta, func(int) lipgloss.Style { return lipgloss.NewStyle() }, w)...)
	d.head = append(d.head, "")

	desc := clean(match.Description)
	if desc == "" {
		desc = "—"
	}
	d.description = wrap(desc, w, "", "", lipgloss.NewStyle())
	url := clean(match.URL)
	if url == "" {
		url = "—"
	}
	d.tail = []string{""}
	for _, l := range strings.Split(ansi.Hardwrap("↳ "+url, w, true), "\n") {
		d.tail = append(d.tail, accentStyle.Render(l))
	}

	// The border, the header, the footer, and a line saying where the
	// description is scrolled to when it doesn't fit.
	room := m.height - 2 - 2 - len(d.head) - len(d.tail)
	d.viewport = max(1, min(len(d.description), room))
	if len(d.description) > d.viewport {
		d.viewport = max(1, room-1)
	}
	return d
}

func (m *Model) detailPopup() string {
	d := m.detailLayout()
	lines := slices.Clone(d.head)
	end := min(len(d.description), m.detailScroll+d.viewport)
	lines = append(lines, d.description[m.detailScroll:end]...)
	if len(d.description) > d.viewport {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("lines %d–%d of %d · ↑↓ to scroll", m.detailScroll+1, end, len(d.description))))
	}
	lines = append(lines, d.tail...)
	match, _ := m.list.selected()
	return boxStyle.BorderForeground(scoreColor(match.Similarity)).Width(m.popupWidth()).Render(strings.Join(lines, "\n"))
}

func (m *Model) helpPopup() string {
	row := func(k, desc string) string { return accentBold.Render(fmt.Sprintf("%12s", k)) + "  " + desc }
	section := func(s string) string { return boldStyle.Render(s) }
	lines := []string{
		section("Keys"),
		row("↑ k  ↓ j", "previous / next match"),
		row("g G", "first / last match"),
		row("PgUp PgDn", "page up / down"),
		row("Enter", "details of the match"),
		row("o", "open it in the browser"),
		row("y", "copy its URL"),
		row("/", "filter by name or description"),
		row("Esc", "clear the filter"),
		row("s", "sort by similarity, popularity, name"),
		row("n", "new search"),
		row("?", "this help"),
		row("q", "quit"),
		"",
		section("Mouse"),
		row("wheel", "scroll"),
		row("click", "select; click again for details"),
		"",
		section("Markers"),
		row(staleMark, fmt.Sprintf("no update in %d+ years, where the source reports a date", freshness.StaleAfterYears)),
		row(fmt.Sprintf("%.2f+", verdict.Close), "a close match, which the verdict counts"),
	}
	w := m.popupWidth()
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, max(1, w-4), "…")
	}
	return boxStyle.Width(w).Render(strings.Join(lines, "\n"))
}
