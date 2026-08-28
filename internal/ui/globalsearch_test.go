package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

func TestParseGlobalSearchKind(t *testing.T) {
	cases := []struct {
		input    string
		wantKind globalSearchKind
		wantTerm string
		wantOK   bool
	}{
		{"a hello", globalSearchArtist, "hello", true},
		{"artist hello", globalSearchArtist, "hello", true},
		{"A Hello World", globalSearchArtist, "Hello World", true},
		{"al hello", globalSearchAlbum, "hello", true},
		{"album hello", globalSearchAlbum, "hello", true},
		{"alb hello", globalSearchAlbum, "hello", true},
		{"AL Hello World", globalSearchAlbum, "Hello World", true},
		{"p Rock Oldies", globalSearchPlaylist, "Rock Oldies", true},
		{"playlist Rock Oldies", globalSearchPlaylist, "Rock Oldies", true},
		{"l never gonna", globalSearchLyrics, "never gonna", true},
		{"lyrics never gonna", globalSearchLyrics, "never gonna", true},
		{"l", globalSearchLyrics, "", true},
		{"t help me", globalSearchTrack, "help me", true},
		{"track help me", globalSearchTrack, "help me", true},
		{"  t   spaced term  ", globalSearchTrack, "spaced term", true},
		{"a", globalSearchArtist, "", true},    // kind selected, no term yet -- still valid, live hints show unfiltered
		{"a   ", globalSearchArtist, "", true}, // trailing space, still no term
		{"x hello", 0, "", false},              // unrecognized prefix
		{"", 0, "", false},                     // empty input
		{"   ", 0, "", false},                  // whitespace-only input
	}
	for _, tc := range cases {
		kind, term, ok := parseGlobalSearchKind(tc.input)
		if ok != tc.wantOK {
			t.Errorf("parseGlobalSearchKind(%q) ok = %v, want %v", tc.input, ok, tc.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if kind != tc.wantKind {
			t.Errorf("parseGlobalSearchKind(%q) kind = %v, want %v", tc.input, kind, tc.wantKind)
		}
		if term != tc.wantTerm {
			t.Errorf("parseGlobalSearchKind(%q) term = %q, want %q", tc.input, term, tc.wantTerm)
		}
	}
}

func TestFilterSubstringHints(t *testing.T) {
	// targets arrive already folded (lyricsindex.Fold), as stored in the
	// index -- lowercased, diacritics stripped.
	targets := []string{
		"we're no strangers to love",
		"never gonna give you up",
		"never gonna let you down",
		"is this the real life",
	}
	// Query is folded by filterSubstringHints itself, so mixed case is fine.
	shown, total := filterSubstringHints("NEVER Gonna", targets)
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(shown) != 2 || shown[0] != 1 || shown[1] != 2 {
		t.Errorf("shown = %v, want [1 2] in target order", shown)
	}

	// Empty term matches everything, order preserved.
	shown, total = filterSubstringHints("", targets)
	if total != 4 || len(shown) != 4 || shown[0] != 0 {
		t.Errorf("empty term: shown = %v total = %d, want all four in order", shown, total)
	}
}

func TestLyricsExcerptCell(t *testing.T) {
	raw := "We're no strangers to love\nYou know the rules and so do I"

	got := lyricsExcerptCell(raw, "know the")
	if !strings.Contains(got, "["+lyricsMatchColor+"]know the[-]") {
		t.Errorf("cell %q should color the matched term", got)
	}
	if !strings.Contains(got, "rules") {
		t.Errorf("cell %q should carry trailing context", got)
	}

	if got := lyricsExcerptCell(raw, ""); got != "" {
		t.Errorf("empty term: got %q, want empty", got)
	}
	if got := lyricsExcerptCell(raw, "absent"); got != "" {
		t.Errorf("no match: got %q, want empty", got)
	}

	// A bracket in the lyrics context is escaped so tview doesn't eat it.
	if got := lyricsExcerptCell("shout [chorus] know the way", "know the"); !strings.Contains(got, "[chorus[]") {
		t.Errorf("context brackets not escaped: %q", got)
	}
}

func TestRebuildLyricsMatchesAgainstTextNotLabels(t *testing.T) {
	labels := []string{"Rick Astley - Never Gonna Give You Up", "Queen - Bohemian Rhapsody"}
	texts := []string{"we're no strangers to love", "is this the real life is this just fantasy"}

	h := &globalSearchHints{}
	h.rebuild("l real life",
		func(globalSearchKind) []string { return labels },
		func(k globalSearchKind) []string {
			if k == globalSearchLyrics {
				return texts
			}
			return nil
		},
	)
	label, idx := h.current()
	if idx != 1 {
		t.Fatalf("current idx = %d, want 1 (matched by lyrics text, not track title)", idx)
	}
	if label != labels[1] {
		t.Errorf("current label = %q, want the track title %q", label, labels[1])
	}
}

func TestFuzzyScoreMatches(t *testing.T) {
	if _, ok := fuzzyScore("", "anything"); !ok {
		t.Fatal("empty query should match everything")
	}
	if _, ok := fuzzyScore("mkn", "Mark Knopfler"); !ok {
		t.Fatal("expected subsequence match")
	}
	if _, ok := fuzzyScore("xyz", "Mark Knopfler"); ok {
		t.Fatal("expected no match")
	}
	if _, ok := fuzzyScore("knm", "Mark Knopfler"); ok {
		t.Fatal("out-of-order subsequence should not match")
	}
}

func TestFuzzyScoreIsAccentInsensitive(t *testing.T) {
	if _, ok := fuzzyScore("buble", "Bublé"); !ok {
		t.Fatal("expected an unaccented query to match an accented candidate, matching every other search path in this app")
	}
}

func TestFuzzyFilterSortIndexRanksAndFilters(t *testing.T) {
	labels := []string{"Hotel California", "Mark Knopfler - In the Sky", "Bang Bang", "Mark Knopfler - Secondary Waltz"}

	got := fuzzyFilterSortIndex("knopfler", labels)
	want := []int{1, 3}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("fuzzyFilterSortIndex(%q) = %v, want %v", "knopfler", got, want)
	}

	if got := fuzzyFilterSortIndex("", labels); len(got) != len(labels) {
		t.Fatalf("empty query should return all %d labels, got %d", len(labels), len(got))
	}

	if got := fuzzyFilterSortIndex("zzz", labels); len(got) != 0 {
		t.Fatalf("expected no matches, got %v", got)
	}
}

// curRow is the selected data-row index of the results table (there is
// no header row, so it maps straight to hints.highlight).
func curRow(tbl *tview.Table) int {
	r, _ := tbl.GetSelection()
	return r
}

func indexLabels(order []int, labels []string) []string {
	out := make([]string, len(order))
	for i, idx := range order {
		out[i] = labels[idx]
	}
	return out
}

// TestRankGlobalSearchHintsPrefersTightMatchOverEarlyLooseMatch is a
// regression test for a real bug caught during manual verification (tmux
// against a live library): querying "chappa" ranked "Carolina Chocolate
// Drops - Political World" -- a loose, scattered subsequence match that
// merely happens to start at index 0 -- above "Hariharan - Chappa
// Chappa", which contains the literal, contiguous substring "Chappa
// Chappa". The original scoring (first*1000 + spread, copied from
// internal/picker) let position dominate over tightness; fuzzyScore now
// weights spread first specifically to fix this.
func TestRankGlobalSearchHintsPrefersTightMatchOverEarlyLooseMatch(t *testing.T) {
	labels := []string{
		"Carolina Chocolate Drops - Political World",
		"Hariharan - Chappa Chappa",
	}
	shown, total := rankGlobalSearchHints("chappa", labels)
	if total != 2 {
		t.Fatalf("total = %d, want 2 (both are valid subsequence matches)", total)
	}
	if len(shown) == 0 || labels[shown[0]] != "Hariharan - Chappa Chappa" {
		t.Fatalf("top hint = %v, want the exact substring match (%q) first", indexLabels(shown, labels), "Hariharan - Chappa Chappa")
	}
}

func TestRankGlobalSearchHintsTruncatesButReportsTrueTotal(t *testing.T) {
	labels := make([]string, maxGlobalSearchHints+5)
	for i := range labels {
		labels[i] = fmt.Sprintf("Track %d", i)
	}
	shown, total := rankGlobalSearchHints("", labels) // empty query matches everything
	if total != len(labels) {
		t.Fatalf("total = %d, want %d (the true match count, before truncation)", total, len(labels))
	}
	if len(shown) != maxGlobalSearchHints {
		t.Fatalf("len(shown) = %d, want %d (capped)", len(shown), maxGlobalSearchHints)
	}
}

func TestFuzzyScoreExactAndImpossibleMatches(t *testing.T) {
	// An exact (case-insensitive) full-string match is the tightest and
	// earliest possible match for that query: spread is exactly
	// len(query)-1 (every rune contiguous) and first is 0, so its score is
	// the theoretical minimum for a query of that length -- no other
	// candidate could score better against the same query.
	query := "mark knopfler"
	wantScore := (len([]rune(query)) - 1) * 1000
	if score, ok := fuzzyScore(query, "Mark Knopfler"); !ok || score != wantScore {
		t.Errorf("exact (case-insensitive) match: score=%d ok=%v, want score=%d ok=true", score, ok, wantScore)
	}
	if _, ok := fuzzyScore("a very long query nobody has", "short"); ok {
		t.Error("a query longer than the candidate should never match")
	}
	if _, ok := fuzzyScore("x", ""); ok {
		t.Error("a non-empty query should never match an empty candidate")
	}
}

func TestMoveHintHighlight(t *testing.T) {
	cases := []struct {
		name              string
		current, n, delta int
		want              int
	}{
		{"empty list always -1", 0, 0, 1, -1},
		{"empty list ignores current/delta", 5, 0, -1, -1},
		{"single item, forward, stays put", 0, 1, 1, 0},
		{"single item, backward, stays put", 0, 1, -1, 0},
		{"forward within bounds", 0, 3, 1, 1},
		{"forward wraps past the end", 2, 3, 1, 0},
		{"backward within bounds", 1, 3, -1, 0},
		{"backward wraps past the start", 0, 3, -1, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := moveHintHighlight(tc.current, tc.n, tc.delta); got != tc.want {
				t.Errorf("moveHintHighlight(%d, %d, %d) = %d, want %d", tc.current, tc.n, tc.delta, got, tc.want)
			}
		})
	}
}

func TestNonEmptyStrings(t *testing.T) {
	got := nonEmptyStrings([]string{"a", "", "b", "", "", "c"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("nonEmptyStrings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("nonEmptyStrings[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if got := nonEmptyStrings(nil); len(got) != 0 {
		t.Errorf("nonEmptyStrings(nil) = %v, want empty", got)
	}
	if got := nonEmptyStrings([]string{"", ""}); len(got) != 0 {
		t.Errorf("nonEmptyStrings(all empty) = %v, want empty", got)
	}
}

func TestPlaylistLabels(t *testing.T) {
	pls := []mpdclient.Playlist{{Name: "Rock Anthems"}, {Name: "Jazz Classics"}}
	got := playlistLabels(pls)
	want := []string{"Rock Anthems", "Jazz Classics"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("playlistLabels = %v, want %v", got, want)
	}
	if got := playlistLabels(nil); len(got) != 0 {
		t.Errorf("playlistLabels(nil) = %v, want empty", got)
	}
}

// --- globalSearchHints: the MPD-free bookkeeping behind the hint list ---

func TestGlobalSearchHintsRebuildSkipsLabelsForOnInvalidPrefix(t *testing.T) {
	cases := []string{"xyz nonsense", "", "   "}
	for _, text := range cases {
		var calls int
		labelsFor := func(globalSearchKind) []string { calls++; return nil }
		h := &globalSearchHints{}
		h.rebuild(text, labelsFor)

		if calls != 0 {
			t.Errorf("rebuild(%q): labelsFor called %d time(s), want 0 (an unrecognized/empty prefix should never touch MPD)", text, calls)
		}
		if h.kindValid {
			t.Errorf("rebuild(%q): kindValid = true, want false", text)
		}
		if _, idx := h.current(); idx != -1 {
			t.Errorf("rebuild(%q): current() idx = %d, want -1 (nothing to highlight)", text, idx)
		}
	}
}

func TestGlobalSearchHintsRebuildCallsLabelsForOnceForValidPrefix(t *testing.T) {
	var calls int
	var gotKind globalSearchKind
	labelsFor := func(k globalSearchKind) []string {
		calls++
		gotKind = k
		return []string{"Foo Bar", "Baz Qux"}
	}
	h := &globalSearchHints{}
	h.rebuild("al foo", labelsFor)

	if calls != 1 {
		t.Fatalf("labelsFor called %d time(s), want exactly 1", calls)
	}
	if gotKind != globalSearchAlbum {
		t.Errorf("labelsFor was called with kind %v, want globalSearchAlbum", gotKind)
	}
	if !h.kindValid {
		t.Error("kindValid = false, want true")
	}
	if label, idx := h.current(); idx != 0 || label != "Foo Bar" {
		t.Errorf("current() = (%q, %d), want (%q, 0)", label, idx, "Foo Bar")
	}
}

func TestGlobalSearchHintsEmptyTermShowsUnfilteredTopMatches(t *testing.T) {
	labelsFor := func(globalSearchKind) []string { return []string{"Alpha", "Beta", "Gamma"} }
	h := &globalSearchHints{}
	h.rebuild("t", labelsFor) // kind selected, no term typed yet

	if h.total != 3 {
		t.Errorf("total = %d, want 3 (empty query matches everything)", h.total)
	}
	if label, idx := h.current(); idx != 0 || label != "Alpha" {
		t.Errorf("current() = (%q, %d), want (%q, 0) -- first candidate highlighted by default", label, idx, "Alpha")
	}
}

func TestGlobalSearchHintsZeroMatchesLeavesNothingHighlighted(t *testing.T) {
	labelsFor := func(globalSearchKind) []string { return []string{"Rock Anthems", "Jazz Classics"} }
	h := &globalSearchHints{}
	h.rebuild("p nonexistent-xyz", labelsFor)

	if !h.kindValid {
		t.Error("kindValid = false, want true (playlist is a recognized kind, it just has no matches)")
	}
	if h.total != 0 {
		t.Errorf("total = %d, want 0", h.total)
	}
	if _, idx := h.current(); idx != -1 {
		t.Errorf("current() idx = %d, want -1", idx)
	}
	h.move(1) // must not panic or produce a valid-looking index on an empty list
	if _, idx := h.current(); idx != -1 {
		t.Errorf("after move() on zero matches, current() idx = %d, want -1", idx)
	}
}

func TestGlobalSearchHintsNavigationWrapsAndConfirmsHighlighted(t *testing.T) {
	labelsFor := func(globalSearchKind) []string { return []string{"Rock Anthems", "Jazz Classics", "Rock Ballads"} }
	h := &globalSearchHints{}
	h.rebuild("p rock", labelsFor) // matches "Rock Anthems" and "Rock Ballads", not "Jazz Classics"

	if h.total != 2 {
		t.Fatalf("total = %d, want 2", h.total)
	}
	label, _ := h.current()
	if label != "Rock Anthems" {
		t.Fatalf("initial highlight = %q, want %q", label, "Rock Anthems")
	}

	h.move(1)
	if label, _ := h.current(); label != "Rock Ballads" {
		t.Errorf("after move(1), highlight = %q, want %q", label, "Rock Ballads")
	}

	h.move(1) // past the end of 2 matches -- should wrap back to the first
	if label, _ := h.current(); label != "Rock Anthems" {
		t.Errorf("after wrapping move(1), highlight = %q, want %q", label, "Rock Anthems")
	}

	h.move(-1) // backward from the first -- should wrap to the last
	if label, _ := h.current(); label != "Rock Ballads" {
		t.Errorf("after wrapping move(-1), highlight = %q, want %q", label, "Rock Ballads")
	}
}

func TestGlobalSearchHintsJumpFirstAndLast(t *testing.T) {
	labelsFor := func(globalSearchKind) []string { return []string{"Alpha", "Beta", "Gamma"} }
	h := &globalSearchHints{}
	h.rebuild("t", labelsFor)
	if h.total != 3 {
		t.Fatalf("setup: total = %d, want 3", h.total)
	}

	h.move(1) // sit on "Beta" before jumping, so jumpFirst/jumpLast are visibly not no-ops
	if label, _ := h.current(); label != "Beta" {
		t.Fatalf("setup: highlight = %q, want %q", label, "Beta")
	}

	h.jumpLast()
	if label, _ := h.current(); label != "Gamma" {
		t.Errorf("after jumpLast(), highlight = %q, want %q", label, "Gamma")
	}

	h.jumpFirst()
	if label, _ := h.current(); label != "Alpha" {
		t.Errorf("after jumpFirst(), highlight = %q, want %q", label, "Alpha")
	}
}

func TestGlobalSearchHintsJumpFirstAndLastNoopOnEmptyOrder(t *testing.T) {
	labelsFor := func(globalSearchKind) []string { return []string{"Rock Anthems"} }
	h := &globalSearchHints{}
	h.rebuild("p nonexistent-xyz", labelsFor)
	if h.total != 0 {
		t.Fatalf("setup: total = %d, want 0", h.total)
	}

	h.jumpFirst()
	if _, idx := h.current(); idx != -1 {
		t.Errorf("jumpFirst() on zero matches: current() idx = %d, want -1", idx)
	}
	h.jumpLast()
	if _, idx := h.current(); idx != -1 {
		t.Errorf("jumpLast() on zero matches: current() idx = %d, want -1", idx)
	}
}

func TestGlobalSearchHintsKindSwitchResetsHighlightAndRefetches(t *testing.T) {
	calls := map[globalSearchKind]int{}
	labelsFor := func(k globalSearchKind) []string {
		calls[k]++
		switch k {
		case globalSearchArtist:
			return []string{"Queen", "Quicksilver"}
		case globalSearchAlbum:
			return []string{"Greatest Hits"}
		}
		return nil
	}
	h := &globalSearchHints{}

	h.rebuild("a qu", labelsFor) // matches both "Queen" and "Quicksilver"
	if h.total != 2 {
		t.Fatalf("setup: total = %d, want 2", h.total)
	}
	h.move(1) // highlight "Quicksilver"
	if label, _ := h.current(); label != "Quicksilver" {
		t.Fatalf("setup: highlight = %q, want %q", label, "Quicksilver")
	}

	// Switching kind mid-session (typing "al" over "a") must re-resolve
	// against the album candidates, not silently keep pointing at index 1
	// of a completely different label slice.
	h.rebuild("al greatest", labelsFor)
	if h.kind != globalSearchAlbum {
		t.Fatalf("kind after switch = %v, want globalSearchAlbum", h.kind)
	}
	label, idx := h.current()
	if idx != 0 || label != "Greatest Hits" {
		t.Errorf("current() after kind switch = (%q, %d), want (%q, 0)", label, idx, "Greatest Hits")
	}
	if calls[globalSearchArtist] != 1 || calls[globalSearchAlbum] != 1 {
		t.Errorf("labelsFor call counts = %v, want exactly one call per kind actually selected", calls)
	}
}

func TestGroupByAlbumGroupsByArtistAndAlbum(t *testing.T) {
	songs := []mpdclient.Song{
		{Artist: "Queen", Album: "A Night at the Opera", Title: "Bohemian Rhapsody"},
		{Artist: "Queen", Album: "A Night at the Opera", Title: "Love of My Life"},
		{Artist: "Abba", Album: "Gold", Title: "Dancing Queen"},
	}
	groups := groupByAlbum(songs)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	// Sorted by (Artist, Album) key -- "Abba" < "Queen".
	if groups[0].label != "Abba - Gold" {
		t.Errorf("groups[0].label = %q, want %q", groups[0].label, "Abba - Gold")
	}
	if len(groups[0].songs) != 1 {
		t.Errorf("groups[0] has %d songs, want 1", len(groups[0].songs))
	}
	if groups[1].label != "Queen - A Night at the Opera" {
		t.Errorf("groups[1].label = %q, want %q", groups[1].label, "Queen - A Night at the Opera")
	}
	if len(groups[1].songs) != 2 {
		t.Errorf("groups[1] has %d songs, want 2", len(groups[1].songs))
	}
}

func TestGroupByAlbumSameAlbumNameDifferentArtistsStaySeparate(t *testing.T) {
	songs := []mpdclient.Song{
		{Artist: "Artist A", Album: "Greatest Hits", Title: "Track 1"},
		{Artist: "Artist B", Album: "Greatest Hits", Title: "Track 2"},
	}
	groups := groupByAlbum(songs)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2 (same album name, different artists)", len(groups))
	}
}

func TestGroupByAlbumNoArtistOmitsDash(t *testing.T) {
	songs := []mpdclient.Song{{Artist: "", Album: "Compilation", Title: "Track"}}
	groups := groupByAlbum(songs)
	if got := groups[0].label; got != "Compilation" {
		t.Errorf("label = %q, want %q (no artist to prefix)", got, "Compilation")
	}
}

func TestGroupByArtistGroupsByArtist(t *testing.T) {
	songs := []mpdclient.Song{
		{Artist: "Queen", Album: "A Night at the Opera", Title: "Bohemian Rhapsody"},
		{Artist: "Queen", Album: "Jazz", Title: "Don't Stop Me Now"},
		{Artist: "Abba", Album: "Gold", Title: "Dancing Queen"},
	}
	groups := groupByArtist(songs)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	// Sorted alphabetically by Artist -- "Abba" < "Queen".
	if groups[0].label != "Abba" {
		t.Errorf("groups[0].label = %q, want %q", groups[0].label, "Abba")
	}
	if len(groups[0].songs) != 1 {
		t.Errorf("groups[0] has %d songs, want 1", len(groups[0].songs))
	}
	if groups[1].label != "Queen" {
		t.Errorf("groups[1].label = %q, want %q", groups[1].label, "Queen")
	}
	if len(groups[1].songs) != 2 {
		t.Errorf("groups[1] has %d songs, want 2 (both by Queen, across two albums)", len(groups[1].songs))
	}
}

func TestGroupByArtistNoArtistUsesPlaceholderLabel(t *testing.T) {
	songs := []mpdclient.Song{{Artist: "", Album: "Compilation", Title: "Track"}}
	groups := groupByArtist(songs)
	if got := groups[0].label; got != "(unknown artist)" {
		t.Errorf("label = %q, want %q", got, "(unknown artist)")
	}
}

func TestOpenGlobalSearchInvalidPrefixKeepsPopupOpen(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openGlobalSearch()

	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus after openGlobalSearch = %T, want *tview.InputField", a.tv.GetFocus())
	}
	field.SetText("xyz nonsense")
	handler := field.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if a.mode != modeOverlay {
		t.Error("mode after an unrecognized prefix should stay modeOverlay (popup stays open)")
	}
	if a.tv.GetFocus() != field {
		t.Error("focus after an unrecognized prefix should stay on the search field")
	}
}

func TestOpenGlobalSearchPlaylistNoMatchKeepsPopupOpen(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems", "Jazz Classics"})
	a.tv.SetFocus(a.library.tree)
	a.openGlobalSearch()

	field := a.tv.GetFocus().(*tview.InputField)
	field.SetText("p nonexistent-playlist-xyz")
	handler := field.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if a.mode != modeOverlay {
		t.Error("mode after no playlist match should stay modeOverlay (popup stays open)")
	}
	if a.tv.GetFocus() != field {
		t.Error("focus after no playlist match should stay on the search field")
	}
}

// TestRankGlobalSearchHintsPicksBestPlaylistMatch covers what used to be
// exercised end-to-end (type "p Rock", press Enter, land on the matched
// playlist) back when Enter ran a broader search. Now Enter on a playlist
// hint directly loads and plays it (PlaylistLoad clears the queue) --
// genuinely mutating state, so unlike the read-only Album/Artist live
// tests below, it can't safely run against a live MPD server the way
// internal/mpdclient/tests' own convention avoids destructive round-trips
// (see TestQueueRoundTrip's no-op move). rankGlobalSearchHints is the
// exact ranking logic confirm() would act on, so this covers the same
// "does typing 'Rock' surface the right playlist first" question without
// ever touching MPD.
func TestRankGlobalSearchHintsPicksBestPlaylistMatch(t *testing.T) {
	labels := []string{"Rock Anthems", "Jazz Classics", "Rock Ballads"}
	shown, total := rankGlobalSearchHints("Rock", labels)
	if total != 2 {
		t.Fatalf("total = %d, want 2 (Rock Anthems, Rock Ballads)", total)
	}
	if len(shown) == 0 || labels[shown[0]] != "Rock Anthems" {
		t.Fatalf("top hint = %v, want %q first", indexLabels(shown, labels), "Rock Anthems")
	}
}

func TestOpenGlobalSearchDefaultsToTrackPrefix(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openGlobalSearch()

	field := a.tv.GetFocus().(*tview.InputField)
	if got := field.GetText(); got != "t " {
		t.Errorf("default field text = %q, want %q", got, "t ")
	}
}

func TestOpenGlobalSearchEscRestoresOriginalFocus(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openGlobalSearch()

	if a.tv.GetFocus() == a.queue.table {
		t.Fatal("setup: focus should have moved to the search field")
	}

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape while the global search popup is open should be consumed, got %v", result)
	}
	if a.mode != modeNormal {
		t.Error("mode after Escape should be modeNormal")
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after Escape = %T, want the originally-focused Queue table", a.tv.GetFocus())
	}
}

// firstTaggedAlbum finds a real album name to search against. Real
// libraries often have an untagged-track bucket that shows up as an
// empty-string "artist"/"album" (first alphabetically, so artists[0] is a
// trap here) -- a substring search against "" trivially matches
// everything instead of testing anything meaningful.
func firstTaggedAlbum(t *testing.T, c *mpdclient.Client) string {
	t.Helper()
	artists, err := c.Artists()
	if err != nil {
		t.Fatalf("Artists: %v", err)
	}
	for _, a := range artists {
		if a == "" {
			continue
		}
		albums, err := c.Albums(a)
		if err != nil {
			t.Fatalf("Albums(%q): %v", a, err)
		}
		for _, al := range albums {
			if al != "" {
				return al
			}
		}
	}
	t.Skip("library has no artist with a non-empty tagged album to search against")
	return ""
}

// firstTaggedArtist finds a real, non-empty artist name to search against
// (see firstTaggedAlbum's doc comment on why the empty-string bucket has
// to be skipped explicitly).
func firstTaggedArtist(t *testing.T, c *mpdclient.Client) string {
	t.Helper()
	artists, err := c.Artists()
	if err != nil {
		t.Fatalf("Artists: %v", err)
	}
	for _, a := range artists {
		if a != "" {
			return a
		}
	}
	t.Skip("library has no non-empty tagged artist to search against")
	return ""
}

func TestOpenGlobalSearchAlbumMatchNeedsLiveMPD(t *testing.T) {
	c := dialOrSkip(t)
	a := &App{tv: tview.NewApplication(), client: c}
	a.build()
	a.tv.SetFocus(a.queue.table)

	album := firstTaggedAlbum(t, c)

	a.openGlobalSearch()
	field := a.tv.GetFocus().(*tview.InputField)
	field.SetText("al " + album)
	handler := field.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if a.mode != modeNormal {
		t.Fatal("mode after a successful album search should be modeNormal (popup closed)")
	}
	if a.tv.GetFocus() != a.library.tree {
		t.Errorf("focus after an album match = %T, want the Library tree", a.tv.GetFocus())
	}
	if got := len(a.library.root.GetChildren()); got == 0 {
		t.Error("expected at least one album-group result node")
	}
}

func TestOpenGlobalSearchArtistMatchNeedsLiveMPD(t *testing.T) {
	c := dialOrSkip(t)
	a := &App{tv: tview.NewApplication(), client: c}
	a.build()
	a.tv.SetFocus(a.queue.table)

	artist := firstTaggedArtist(t, c)

	a.openGlobalSearch()
	field := a.tv.GetFocus().(*tview.InputField)
	field.SetText("a " + artist)
	handler := field.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if a.mode != modeNormal {
		t.Fatal("mode after a successful artist search should be modeNormal (popup closed)")
	}
	if a.tv.GetFocus() != a.library.tree {
		t.Errorf("focus after an artist match = %T, want the Library tree", a.tv.GetFocus())
	}
	if got := len(a.library.root.GetChildren()); got == 0 {
		t.Error("expected at least one artist-group result node")
	}
}

// --- Tab-toggled hint-list navigation (Tab/'f'/j/k/g/G/'a') ---

func TestOpenGlobalSearchTabTogglesFocusBetweenFieldAndTable(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems"})
	a.tv.SetFocus(a.queue.table)
	a.openGlobalSearch()

	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus after openGlobalSearch = %T, want *tview.InputField", a.tv.GetFocus())
	}

	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	field.InputHandler()(tab, func(tview.Primitive) {})

	table, ok := a.tv.GetFocus().(*tview.Table)
	if !ok {
		t.Fatalf("focus after Tab from field = %T, want *tview.Table", a.tv.GetFocus())
	}
	if a.mode != modeOverlay {
		t.Error("mode after Tab within the popup should stay modeOverlay")
	}

	table.InputHandler()(tab, func(tview.Primitive) {})
	if a.tv.GetFocus() != field {
		t.Errorf("focus after Tab from the table = %T, want the original field back", a.tv.GetFocus())
	}
}

func TestOpenGlobalSearchFKeyReturnsFocusFromTableToField(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems"})
	a.tv.SetFocus(a.queue.table)
	a.openGlobalSearch()
	field := a.tv.GetFocus().(*tview.InputField)

	field.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})
	table, ok := a.tv.GetFocus().(*tview.Table)
	if !ok {
		t.Fatalf("setup: focus = %T, want *tview.Table", a.tv.GetFocus())
	}

	table.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'f', tcell.ModNone), func(tview.Primitive) {})
	if a.tv.GetFocus() != field {
		t.Errorf("focus after 'f' from the table = %T, want the field back", a.tv.GetFocus())
	}
}

// TestOpenGlobalSearchLettersUsedForNavigationStayTypeableInField guards
// against the navigation keys added for the hint list (j/k/g/G/a) ever
// leaking into the field's own typing -- they must only mean
// "navigate"/"add" once the list actually has focus, exactly like 'a' is
// a normal letter everywhere else in this app except when a non-text
// panel (Library, Playlists) is focused.
func TestOpenGlobalSearchLettersUsedForNavigationStayTypeableInField(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openGlobalSearch()
	field := a.tv.GetFocus().(*tview.InputField)

	// SetText replaces the default "t " outright (typing 'p' instead would
	// only append after it -- the cursor sits at the end -- leaving "t "
	// as the first word and the kind still track) but bypasses
	// SetChangedFunc, so nothing has actually rebuilt yet. Completing the
	// text with one real keystroke does both: switches to the playlist
	// prefix (offline-safe, no MPD fetch -- playlist candidates come from
	// playlistsPanel.pls) and fires the one rebuild() that would
	// otherwise never happen. j/k/a/g then get typed as part of the
	// playlist search term itself, doubling as proof they work as
	// ordinary text there too.
	field.SetText("p jkag")
	field.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'G', tcell.ModNone), func(tview.Primitive) {})
	if got, want := field.GetText(), "p jkagG"; got != want {
		t.Errorf("field text after typing j/k/a/g/G = %q, want %q (must stay normal typing while the field is focused)", got, want)
	}
}

func TestOpenGlobalSearchTableJKGNavigateHighlight(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems", "Rock Ballads", "Rock Classics"})
	a.tv.SetFocus(a.queue.table)
	a.openGlobalSearch()
	field := a.tv.GetFocus().(*tview.InputField)

	// SetText bypasses SetChangedFunc (same as this file's other
	// non-live tests), so a real keystroke is needed to actually trigger
	// rebuild() and populate the hint list -- completing the text with a
	// real typed 'k' does both at once (SetText leaves the cursor at the
	// end of the new text, so 'k' lands after "p roc").
	field.SetText("p roc")
	field.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone), func(tview.Primitive) {})

	field.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})
	table, ok := a.tv.GetFocus().(*tview.Table)
	if !ok {
		t.Fatalf("focus after Tab = %T, want *tview.Table", a.tv.GetFocus())
	}
	if table.GetRowCount() != 3 {
		t.Fatalf("hint count = %d, want 3 (Rock Anthems, Rock Ballads, Rock Classics)", table.GetRowCount())
	}
	if curRow(table) != 0 {
		t.Fatalf("initial highlight = %d, want 0", curRow(table))
	}

	table.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone), func(tview.Primitive) {})
	if curRow(table) != 1 {
		t.Errorf("highlight after 'j' = %d, want 1", curRow(table))
	}

	table.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'G', tcell.ModNone), func(tview.Primitive) {})
	if curRow(table) != 2 {
		t.Errorf("highlight after 'G' = %d, want 2 (last)", curRow(table))
	}

	table.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone), func(tview.Primitive) {})
	if curRow(table) != 0 {
		t.Errorf("highlight after 'g' = %d, want 0 (first)", curRow(table))
	}

	table.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone), func(tview.Primitive) {})
	if curRow(table) != 2 {
		t.Errorf("highlight after 'k' from the first item = %d, want 2 (wraps to the last)", curRow(table))
	}
}

// TestOpenGlobalSearchAddToQueueNoopWhenNothingHighlighted also proves
// 'a' never reaches appendPlaylist/QueueAdd when there's nothing to act
// on -- both would panic against this test's nil MPD client if reached,
// so a passing test here is itself evidence the guard in addToQueue
// works, not just that the popup stays open.
func TestOpenGlobalSearchAddToQueueNoopWhenNothingHighlighted(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems"})
	a.tv.SetFocus(a.queue.table)
	a.openGlobalSearch()
	field := a.tv.GetFocus().(*tview.InputField)

	field.SetText("p nonexistent-x")
	field.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone), func(tview.Primitive) {})

	field.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})
	table, ok := a.tv.GetFocus().(*tview.Table)
	if !ok {
		t.Fatalf("focus after Tab = %T, want *tview.Table", a.tv.GetFocus())
	}
	if table.GetRowCount() != 0 {
		t.Fatalf("setup: hint count = %d, want 0", table.GetRowCount())
	}

	table.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone), func(tview.Primitive) {})
	if a.mode != modeOverlay {
		t.Error("mode after a no-op 'a' should stay modeOverlay (popup stays open)")
	}
}

// TestOpenGlobalSearchAddToQueueInvalidForArtistKindNeedsLiveMPD is a
// live-MPD test (needs a real Artists() call to populate a hint) but a
// safe, read-only one -- 'a' on an artist/album hint must flash invalid
// rather than attempt an undefined "add this artist's whole catalog"
// action, so nothing here ever reaches a mutating MPD call either.
func TestOpenGlobalSearchAddToQueueInvalidForArtistKindNeedsLiveMPD(t *testing.T) {
	c := dialOrSkip(t)
	a := &App{tv: tview.NewApplication(), client: c}
	a.build()
	a.tv.SetFocus(a.queue.table)

	artist := firstTaggedArtist(t, c)
	runes := []rune(artist)

	a.openGlobalSearch()
	field := a.tv.GetFocus().(*tview.InputField)
	field.SetText("a " + string(runes[:len(runes)-1]))
	field.InputHandler()(tcell.NewEventKey(tcell.KeyRune, runes[len(runes)-1], tcell.ModNone), func(tview.Primitive) {})

	field.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})
	table, ok := a.tv.GetFocus().(*tview.Table)
	if !ok {
		t.Fatalf("focus after Tab = %T, want *tview.Table", a.tv.GetFocus())
	}
	if table.GetRowCount() == 0 {
		t.Fatalf("setup: expected at least one artist hint for %q", artist)
	}

	table.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone), func(tview.Primitive) {})
	if a.mode != modeOverlay {
		t.Error("'a' on an artist hint should be invalid (flashed), not close the popup")
	}
	if a.tv.GetFocus() != table {
		t.Errorf("focus after an invalid 'a' = %T, want to stay on the results table", a.tv.GetFocus())
	}
}
