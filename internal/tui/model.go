// Package tui is silt's terminal interface: a search prompt, a progress
// screen while the pipeline runs, and the results — the verdict above a
// filterable, sortable table of matches — with detail and help popups.
//
// It is inspired by patent's TUI, not ported from it (D7 in
// notes/decisions.md), but it keeps patent's integrity rules: the verdict is
// never clipped (if it can't be shown in full, it isn't shown at all),
// sources that failed are always named, and the stale marker appears only
// where a source reports a date. Unlike patent, it leaves out the verdict's
// caveat; the JSON output keeps it.
package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/miku/silt/freshness"
	"github.com/miku/silt/internal/pipeline"
	"github.com/miku/silt/model"
	"github.com/miku/silt/rank"
)

// SearchFunc runs a search for idea. log receives the pipeline's progress
// lines and progress the download of the embedding model on the first run.
type SearchFunc func(ctx context.Context, idea string, log func(string), progress rank.Progress) (pipeline.Result, error)

// Options configure the TUI.
type Options struct {
	// Idea and Result, if Result is not nil, are a finished search to open
	// on. Otherwise the TUI opens on the search prompt.
	Idea   string
	Result *pipeline.Result
	// Search runs the searches started from the prompt.
	Search SearchFunc
}

// Run runs the TUI until the user quits.
func Run(ctx context.Context, opts Options) error {
	m := New(ctx, opts)
	defer m.cancelSearch()
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	return err
}

type phase int

const (
	phaseSearch phase = iota
	phaseSearching
	phaseResults
)

// mode is what the results screen is doing.
type mode int

const (
	modeNormal mode = iota
	modeFilter
	modeHelp
	modeDetail
)

// statusDuration is how long a status message stays in the footer.
const statusDuration = 2 * time.Second

// Model is the TUI's state. It implements [tea.Model].
type Model struct {
	ctx           context.Context
	search        SearchFunc
	width, height int
	phase         phase
	mode          mode

	// The search prompt, and the error of the last attempt.
	input    textinput.Model
	inputErr string

	// The running search.
	spinner    spinner.Model
	searchIdea string
	searchID   int
	cancel     context.CancelFunc
	logLines   []string
	download   string

	// The results.
	hasResults   bool
	idea         string
	verdict      model.Verdict
	list         list
	detailScroll int

	status   string
	statusID int

	// Side effects and the clock, replaced in tests.
	openURL  func(string) error
	copyText func(string) error
	now      func() time.Time
}

// New returns the TUI's initial state.
func New(ctx context.Context, opts Options) *Model {
	m := &Model{
		ctx:      ctx,
		search:   opts.Search,
		input:    textinput.New(),
		spinner:  spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(accentStyle)),
		openURL:  openURL,
		copyText: copyText,
		now:      freshness.Now,
	}
	m.input.Prompt = ""
	m.input.Placeholder = "describe your dev-tool idea…"
	m.input.Focus()
	if opts.Result != nil {
		m.load(opts.Idea, *opts.Result)
	}
	return m
}

// Init implements [tea.Model].
func (m *Model) Init() tea.Cmd {
	if m.phase == phaseSearch {
		return textinput.Blink
	}
	return nil
}

// Messages.
type (
	// logMsg and downloadMsg are progress from search id; next waits for
	// its next message.
	logMsg struct {
		id   int
		line string
		next tea.Cmd
	}
	downloadMsg struct {
		id   int
		line string
		next tea.Cmd
	}
	// doneMsg is the outcome of search id.
	doneMsg struct {
		id     int
		idea   string
		result pipeline.Result
		err    error
	}
	statusMsg      string
	clearStatusMsg int
	// osc52Msg asks for text to be copied through the terminal, as the
	// system clipboard was unavailable.
	osc52Msg string
)

func (m *Model) load(idea string, res pipeline.Result) {
	m.hasResults = true
	m.phase, m.mode = phaseResults, modeNormal
	m.idea, m.verdict = idea, res.Verdict
	m.list = newList(res.Matches)
	m.detailScroll = 0
	m.status = ""
}

// Update implements [tea.Model].
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(1, promptWidth(m.width)-5))
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, m.quit()
		}
		switch m.phase {
		case phaseSearch:
			cmd = m.updateSearch(msg)
		case phaseSearching:
			if msg.String() == "esc" {
				m.cancelSearch()
				m.toPrompt(m.searchIdea)
			}
		case phaseResults:
			cmd = m.updateResults(msg)
		}
	case tea.MouseWheelMsg:
		m.wheel(msg)
	case tea.MouseClickMsg:
		m.click(msg)
	case spinner.TickMsg:
		if m.phase == phaseSearching {
			m.spinner, cmd = m.spinner.Update(msg)
		}
	case logMsg:
		if msg.id == m.searchID {
			m.logLines = append(m.logLines, msg.line)
			cmd = msg.next
		}
	case downloadMsg:
		if msg.id == m.searchID {
			m.download = msg.line
			cmd = msg.next
		}
	case doneMsg:
		if msg.id == m.searchID {
			m.cancelSearch()
			if msg.err != nil {
				m.toPrompt(msg.idea)
				m.inputErr = "Search failed: " + msg.err.Error()
			} else {
				m.load(msg.idea, msg.result)
			}
		}
	case statusMsg:
		cmd = m.setStatus(string(msg))
	case osc52Msg:
		cmd = tea.Batch(tea.SetClipboard(string(msg)), m.setStatus("Sent the URL to the terminal's clipboard (OSC 52)"))
	case clearStatusMsg:
		if int(msg) == m.statusID {
			m.status = ""
		}
	default:
		if m.phase == phaseSearch {
			m.input, cmd = m.input.Update(msg)
		}
	}
	if m.phase == phaseResults {
		m.list.scrollTo(m.resultsLayout().tableRows)
		m.clampDetailScroll()
	}
	return m, cmd
}

func (m *Model) quit() tea.Cmd {
	m.cancelSearch()
	return tea.Quit
}

// toPrompt returns to the search prompt with idea in it.
func (m *Model) toPrompt(idea string) {
	m.phase = phaseSearch
	m.input.SetValue(idea)
	m.input.CursorEnd()
	m.inputErr = ""
}

func (m *Model) updateSearch(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		idea := strings.TrimSpace(m.input.Value())
		if idea == "" {
			return nil
		}
		if err := pipeline.ValidateIdea(idea); err != nil {
			m.inputErr = err.Error()
			return nil
		}
		return m.startSearch(idea)
	case "esc":
		if !m.hasResults {
			return m.quit()
		}
		m.phase = phaseResults
		return nil
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.inputErr = ""
	}
	return cmd
}

// startSearch runs the search for idea in the background. Its progress and
// outcome arrive as messages, tagged with the search's id so that those of a
// canceled search are ignored.
func (m *Model) startSearch(idea string) tea.Cmd {
	m.cancelSearch()
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.searchID++
	id := m.searchID
	m.phase, m.searchIdea, m.logLines, m.download = phaseSearching, idea, nil, ""

	events := make(chan tea.Msg)
	next := func() tea.Msg { return <-events }
	// A canceled search stops sending, so it never blocks on a channel
	// nobody reads any more, and never delivers an outcome after all.
	send := func(msg tea.Msg) {
		if ctx.Err() != nil {
			return
		}
		select {
		case events <- msg:
		case <-ctx.Done():
		}
	}
	var (
		mu      sync.Mutex
		lastPct = map[string]int{}
	)
	progress := func(file string, done, total int64) {
		pct := int(done * 100 / max(total, 1))
		mu.Lock()
		last, seen := lastPct[file]
		lastPct[file] = pct
		mu.Unlock()
		if !seen || pct != last {
			send(downloadMsg{id, fmt.Sprintf("downloading %s: %d%% of %.1f MB", file, pct, float64(total)/1e6), next})
		}
	}
	search := m.search
	go func() {
		// bubbletea restores the terminal after a panic in its own
		// goroutines, not in this one, so a panic becomes a failed search.
		defer func() {
			if r := recover(); r != nil {
				send(doneMsg{id: id, idea: idea, err: fmt.Errorf("the search crashed: %v", r)})
			}
		}()
		res, err := search(ctx, idea, func(line string) { send(logMsg{id, line, next}) }, progress)
		send(doneMsg{id, idea, res, err})
	}()
	return tea.Batch(next, m.spinner.Tick)
}

func (m *Model) cancelSearch() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m *Model) updateResults(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	switch m.mode {
	case modeFilter:
		switch key {
		case "esc":
			m.mode = modeNormal
			m.setFilter("")
		case "enter":
			m.mode = modeNormal
		case "backspace":
			if r := []rune(m.list.filter); len(r) > 0 {
				m.setFilter(string(r[:len(r)-1]))
			}
		default:
			if msg.Text != "" {
				m.setFilter(m.list.filter + msg.Text)
			}
		}
	case modeHelp:
		switch key {
		case "esc", "?", "q":
			m.mode = modeNormal
		}
	case modeDetail:
		switch key {
		case "o", "enter":
			return m.openSelected()
		case "y":
			return m.copySelected()
		case "down", "j":
			m.detailScroll++
		case "up", "k":
			m.detailScroll--
		case "esc", "q":
			m.mode = modeNormal
		}
	default:
		switch key {
		case "q":
			return m.quit()
		case "n":
			m.toPrompt(m.idea)
			return textinput.Blink
		case "down", "j":
			m.list.move(1, true)
		case "up", "k":
			m.list.move(-1, true)
		case "pgdown", "ctrl+d":
			m.list.move(max(1, m.resultsLayout().tableRows), false)
		case "pgup", "ctrl+u":
			m.list.move(-max(1, m.resultsLayout().tableRows), false)
		case "home", "g":
			m.list.top()
		case "end", "G":
			m.list.bottom()
		case "/":
			m.mode = modeFilter
		case "esc":
			m.setFilter("")
		case "s":
			m.list.cycleSort()
		case "enter":
			m.openDetail()
		case "o":
			return m.openSelected()
		case "y":
			return m.copySelected()
		case "?":
			m.mode = modeHelp
		}
	}
	return nil
}

// setFilter filters the matches and selects the first one left.
func (m *Model) setFilter(s string) {
	m.list.setFilter(s)
	m.list.cursor, m.list.offset = 0, 0
}

func (m *Model) openDetail() {
	if _, ok := m.list.selected(); ok {
		m.mode, m.detailScroll = modeDetail, 0
	}
}

func (m *Model) clampDetailScroll() {
	if m.mode != modeDetail {
		return
	}
	d := m.detailLayout()
	m.detailScroll = max(0, min(m.detailScroll, len(d.description)-d.viewport))
}

func (m *Model) wheel(msg tea.MouseWheelMsg) {
	if m.phase != phaseResults {
		return
	}
	delta := 1
	if msg.Button == tea.MouseWheelUp {
		delta = -1
	}
	switch m.mode {
	case modeNormal:
		m.list.move(delta, false)
	case modeDetail:
		m.detailScroll += delta
	}
}

// click selects the clicked row, or opens the details of a row that is
// already selected.
func (m *Model) click(msg tea.MouseClickMsg) {
	if m.phase != phaseResults || m.mode != modeNormal || msg.Button != tea.MouseLeft {
		return
	}
	l := m.resultsLayout()
	if l.tableRows == 0 {
		return
	}
	before := m.list.cursor
	if row := msg.Y - l.firstRowY; row < l.tableRows && m.list.selectRow(row) && m.list.cursor == before {
		m.openDetail()
	}
}

func (m *Model) openSelected() tea.Cmd {
	sel, ok := m.list.selected()
	if !ok {
		return nil
	}
	if !isSafeURL(sel.URL) {
		return m.setStatus("Only http and https links are opened")
	}
	open, url := m.openURL, sel.URL
	return func() tea.Msg {
		if err := open(url); err != nil {
			return statusMsg("Could not open a browser: " + err.Error())
		}
		return statusMsg("Opened in the browser")
	}
}

func (m *Model) copySelected() tea.Cmd {
	sel, ok := m.list.selected()
	if !ok || strings.TrimSpace(sel.URL) == "" {
		return nil
	}
	copyText, url := m.copyText, sel.URL
	return func() tea.Msg {
		if copyText(url) != nil {
			return osc52Msg(url)
		}
		return statusMsg("Copied the URL to the clipboard")
	}
}

// setStatus shows msg in the footer for statusDuration.
func (m *Model) setStatus(msg string) tea.Cmd {
	m.statusID++
	m.status = msg
	id := m.statusID
	return tea.Tick(statusDuration, func(time.Time) tea.Msg { return clearStatusMsg(id) })
}

// View implements [tea.Model].
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "silt"
	return v
}
