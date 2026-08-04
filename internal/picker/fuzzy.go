package picker

import (
	"sort"
	"strings"
)

// FuzzyScore reports whether every rune of query appears in candidate,
// in order, case-insensitively (a plain subsequence match -- the same
// basic idea fzf/fzy use). An empty query matches everything. Lower
// scores are better matches: earlier and more compact matches win.
// Exported so it's testable from internal/picker/tests without a terminal.
func FuzzyScore(query, candidate string) (score int, ok bool) {
	if query == "" {
		return 0, true
	}
	q := []rune(strings.ToLower(query))
	c := []rune(strings.ToLower(candidate))

	qi := 0
	first, last := -1, -1
	for ci := 0; ci < len(c) && qi < len(q); ci++ {
		if c[ci] == q[qi] {
			if first < 0 {
				first = ci
			}
			last = ci
			qi++
		}
	}
	if qi < len(q) {
		return 0, false
	}
	return first*1000 + (last - first), true
}

// FilterSortIndex filters labels by FuzzyScore against query and returns
// their indices best-match-first, stable on ties.
func FilterSortIndex(query string, labels []string) []int {
	type scored struct {
		idx   int
		score int
	}
	matched := make([]scored, 0, len(labels))
	for i, l := range labels {
		if s, ok := FuzzyScore(query, l); ok {
			matched = append(matched, scored{i, s})
		}
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].score < matched[j].score })

	out := make([]int, len(matched))
	for i, m := range matched {
		out[i] = m.idx
	}
	return out
}
