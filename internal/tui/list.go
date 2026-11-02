package tui

import (
	"cmp"
	"slices"
	"strings"

	"github.com/miku/silt/internal/model"
)

// sortKey orders the matches table; s cycles through the keys.
type sortKey int

const (
	// bySimilarity is the ranker's order, and the default.
	bySimilarity sortKey = iota
	// byPopularity puts the most popular first and matches whose source
	// reports no popularity last.
	byPopularity
	// byName sorts A to Z, ignoring case.
	byName
)

func (k sortKey) String() string {
	return [...]string{"similarity", "popularity", "name"}[k]
}

// list is the matches table: the matches, the rows the filter lets through
// in the current order, and a cursor over those rows.
type list struct {
	matches []model.Match
	// rows are indexes into matches.
	rows   []int
	filter string
	sort   sortKey
	// cursor is the selected row, offset the first row on screen.
	cursor, offset int
}

func newList(matches []model.Match) list {
	l := list{matches: matches}
	l.refresh()
	return l
}

// refresh recomputes the rows from the filter and the sort order. The sorts
// are stable, so equal keys keep the ranker's order.
func (l *list) refresh() {
	needle := strings.ToLower(l.filter)
	l.rows = l.rows[:0]
	for i, m := range l.matches {
		if strings.Contains(strings.ToLower(m.Name), needle) || strings.Contains(strings.ToLower(m.Description), needle) {
			l.rows = append(l.rows, i)
		}
	}
	ms := l.matches
	switch l.sort {
	case bySimilarity:
		slices.SortStableFunc(l.rows, func(a, b int) int { return cmp.Compare(ms[b].Similarity, ms[a].Similarity) })
	case byPopularity:
		slices.SortStableFunc(l.rows, func(a, b int) int { return comparePopularity(ms[b].Popularity, ms[a].Popularity) })
	case byName:
		slices.SortStableFunc(l.rows, func(a, b int) int {
			return strings.Compare(strings.ToLower(ms[a].Name), strings.ToLower(ms[b].Name))
		})
	}
	l.cursor = max(0, min(l.cursor, len(l.rows)-1))
}

// comparePopularity orders a missing popularity before any known one.
func comparePopularity(a, b *uint64) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	return cmp.Compare(*a, *b)
}

func (l *list) setFilter(s string) {
	l.filter = s
	l.refresh()
}

func (l *list) cycleSort() {
	l.sort = (l.sort + 1) % 3
	l.refresh()
	l.cursor, l.offset = 0, 0
}

// selected returns the match under the cursor.
func (l *list) selected() (model.Match, bool) {
	if len(l.rows) == 0 {
		return model.Match{}, false
	}
	return l.matches[l.rows[l.cursor]], true
}

// move moves the cursor by delta rows. With wrap, moving past either end
// continues from the other; without, the cursor stops there.
func (l *list) move(delta int, wrap bool) {
	n := len(l.rows)
	switch {
	case n == 0:
	case wrap:
		l.cursor = ((l.cursor+delta)%n + n) % n
	default:
		l.cursor = max(0, min(l.cursor+delta, n-1))
	}
}

func (l *list) top()    { l.cursor = 0 }
func (l *list) bottom() { l.cursor = max(0, len(l.rows)-1) }

// selectRow selects the row shown at position i of a viewport, if there is
// one.
func (l *list) selectRow(i int) bool {
	if row := l.offset + i; i >= 0 && row < len(l.rows) {
		l.cursor = row
		return true
	}
	return false
}

// scrollTo adjusts the offset so that the cursor is within a viewport of
// height rows, moving it as little as possible.
func (l *list) scrollTo(height int) {
	switch {
	case height <= 0:
		l.offset = 0
	case l.cursor < l.offset:
		l.offset = l.cursor
	case l.cursor >= l.offset+height:
		l.offset = l.cursor - height + 1
	}
	// Don't leave blank rows below the last match when there's room for it.
	l.offset = max(0, min(l.offset, len(l.rows)-height))
}
