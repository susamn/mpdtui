package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// globalSearchKind is which section of the app a global ('f') search
// targets, chosen by the first word of the input.
type globalSearchKind int

const (
	globalSearchTrack globalSearchKind = iota
	globalSearchArtist
	globalSearchAlbum
	globalSearchPlaylist
)

func (k globalSearchKind) label() string {
	switch k {
	case globalSearchArtist:
		return "artist"
	case globalSearchAlbum:
		return "album"
	case globalSearchPlaylist:
		return "playlist"
	default:
		return "track"
	}
}

// maxGlobalSearchHints caps how many fuzzy matches the hint list shows at
// once -- a glanceable fzf-style shortlist, not a full result dump.
const maxGlobalSearchHints = 10

// parseGlobalSearchKind splits raw input into a search kind and term. The
// first word selects the kind, case-insensitive: a leading "al" (e.g.
// "al", "album") means album, any other word starting with "a" (e.g. "a",
// "artist") means artist, "p"/"playlist" means playlist, "t"/"track" means
// track -- only that leading prefix matters, so "t", "track", and "trk"
// are all equivalent. Everything after the first word is the term.
// Unlike a stricter parse that requires a term too, this succeeds the
// moment the prefix word is recognized -- even with no term yet -- since
// live hints should populate (unfiltered, showing the first
// maxGlobalSearchHints candidates) as soon as the kind is selected, not
// only once a search term follows it. Returns ok=false for empty input or
// an unrecognized leading word.
func parseGlobalSearchKind(input string) (kind globalSearchKind, term string, ok bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, "", false
	}
	firstWord, rest, _ := strings.Cut(input, " ")
	rest = strings.TrimSpace(rest)
	word := strings.ToLower(firstWord)
	switch {
	case strings.HasPrefix(word, "al"):
		return globalSearchAlbum, rest, true
	case strings.HasPrefix(word, "a"):
		return globalSearchArtist, rest, true
	case strings.HasPrefix(word, "p"):
		return globalSearchPlaylist, rest, true
	case strings.HasPrefix(word, "t"):
		return globalSearchTrack, rest, true
	}
	return 0, "", false
}

// fuzzyScore reports whether every rune of query appears in candidate, in
// order, case-insensitively (a plain subsequence match -- the same basic
// idea fzf/fzy use). An empty query matches everything. Lower scores are
// better matches: earlier and more compact matches win. This is a
// deliberate duplicate of internal/picker's identical FuzzyScore rather
// than an import of it: per DEPENDENCY.md, internal/ui and
// internal/picker are independent siblings under internal/mpdclient, and
// neither imports the other -- adding that edge for a ~20-line matcher
// isn't worth breaking the documented module boundary. Unlike picker's
// version, this one folds diacritics too (foldSearch, not a plain
// strings.ToLower) -- every other search path in this app (/, f's old
// substring searches) is accent-insensitive (README: "buble" matches
// "Bublé"), and a hint list that can't even surface an accented track for
// its own unaccented query would silently break that promise, since
// picking a track hint acts on it directly rather than re-running a
// containsFold search the way confirm's artist/album paths still do.
func fuzzyScore(query, candidate string) (score int, ok bool) {
	if query == "" {
		return 0, true
	}
	q := []rune(foldSearch(query))
	c := []rune(foldSearch(candidate))

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
	// Tightness (how compact the match is) dominates the score, position
	// only breaks ties -- weighting it the other way around (as
	// internal/picker's identical matcher does, spread as the minor term)
	// lets a loose match that merely happens to start at candidate[0]
	// consistently outrank a tight, exact substring match starting a few
	// characters in, e.g. querying "chappa" against a library containing
	// the literal track "Hariharan - Chappa Chappa" would rank an
	// unrelated "Carolina Chocolate Drops - Political World" first,
	// purely because its scattered chappa-subsequence starts at index 0.
	spread := last - first
	return spread*1000 + first, true
}

// rankGlobalSearchHints fuzzy-ranks labels against term and returns the
// indices to actually show (best-first, capped to maxGlobalSearchHints)
// along with the true total match count before that cap -- split out from
// openGlobalSearch's rebuild() closure purely so hint ranking is
// unit-testable without constructing a live popup or touching MPD.
func rankGlobalSearchHints(term string, labels []string) (shown []int, total int) {
	matched := fuzzyFilterSortIndex(term, labels)
	total = len(matched)
	if total > maxGlobalSearchHints {
		matched = matched[:maxGlobalSearchHints]
	}
	return matched, total
}

// fuzzyFilterSortIndex filters labels by fuzzyScore against query and
// returns their indices best-match-first, stable on ties.
func fuzzyFilterSortIndex(query string, labels []string) []int {
	type scored struct {
		idx   int
		score int
	}
	matched := make([]scored, 0, len(labels))
	for i, l := range labels {
		if s, ok := fuzzyScore(query, l); ok {
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

// nonEmptyStrings drops empty entries from in -- MPD's "list" commands
// report untagged tracks as an empty-string artist/album bucket, which
// would otherwise show up as a blank, unselectable hint.
func nonEmptyStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func playlistLabels(pls []mpdclient.Playlist) []string {
	labels := make([]string, len(pls))
	for i, p := range pls {
		labels[i] = p.Name
	}
	return labels
}

// moveHintHighlight returns the new highlighted index after moving by
// delta among n items, wrapping around. Returns -1 if n == 0 (nothing to
// highlight). Split out as a pure function (no tview.List involved) so
// wrap-around edge cases are directly testable; globalSearchHints.move is
// a thin wrapper around it. Mirrors internal/picker/picker.go's
// moveSelection arithmetic exactly (current + delta + n, mod n), which is
// why an out-of-range current (in practice only ever -1, tview.List's own
// "nothing selected" convention) can land on index 1 rather than the last
// index when moving backward from there -- a pre-existing quirk in the
// already-shipped picker code this mirrors, and not reachable in normal
// use here since rebuild() always sets the highlight to 0 the moment
// there's at least one match, so current is never -1 by the time a move
// actually happens.
func moveHintHighlight(current, n, delta int) int {
	if n == 0 {
		return -1
	}
	return (current + delta + n) % n
}

// globalSearchHints is the MPD- and tview-free bookkeeping behind the
// global-search popup's live hint list: given the input field's current
// text and a way to fetch the active kind's candidate labels, it resolves
// the kind, ranks and caps the matches, and tracks which one is
// highlighted. Split out from openGlobalSearch's tview wiring purely for
// testability -- mirrors trackChangedForJump's split from
// refreshNowPlaying in app.go -- so kind detection, ranking, and
// arrow-key navigation are all exercisable directly in tests, without a
// live MPD connection or a running tview.Application.
type globalSearchHints struct {
	kind      globalSearchKind
	kindValid bool
	labels    []string // the active kind's full candidate label slice, as returned by the last rebuild's labelsFor
	order     []int    // indices into labels, best-match-first, capped to maxGlobalSearchHints
	total     int      // true match count before the cap
	highlight int      // index into order of the currently highlighted hint; -1 if order is empty
}

// rebuild re-parses text via parseGlobalSearchKind and, if it resolves to
// a valid kind, calls labelsFor(kind) for that kind's full candidate
// label slice -- the caller's (openGlobalSearch's) chance to lazily load
// it from MPD first, memoized so this only ever fetches once per kind per
// popup session regardless of how many times rebuild runs -- then
// fuzzy-ranks it against the term. The highlight resets to the top match,
// or -1 if there are none (unrecognized prefix, or zero matches).
func (h *globalSearchHints) rebuild(text string, labelsFor func(globalSearchKind) []string) {
	kind, term, ok := parseGlobalSearchKind(text)
	h.kind, h.kindValid = kind, ok
	h.labels, h.order, h.total, h.highlight = nil, nil, 0, -1
	if !ok {
		return
	}
	h.labels = labelsFor(kind)
	h.order, h.total = rankGlobalSearchHints(term, h.labels)
	if len(h.order) > 0 {
		h.highlight = 0
	}
}

// move shifts the highlight by delta, wrapping around; a no-op (stays -1)
// on an empty hint list.
func (h *globalSearchHints) move(delta int) {
	h.highlight = moveHintHighlight(h.highlight, len(h.order), delta)
}

// current returns the currently highlighted hint's label and its index
// into h.labels (the same index confirm needs to look up the underlying
// Song/artist/album/playlist), or ("", -1) if there's nothing highlighted
// -- either rebuild hasn't found a valid kind, or it found one with zero
// matches.
func (h *globalSearchHints) current() (label string, idx int) {
	if h.highlight < 0 || h.highlight >= len(h.order) {
		return "", -1
	}
	idx = h.order[h.highlight]
	return h.labels[idx], idx
}

// openGlobalSearch opens a "f" search reachable from any panel: type a
// prefix word (a/al/p/t) plus a search term, e.g. "a queen" for artists
// or "al hello" for albums. Matching candidates for the active kind
// appear live, fzf-style, in a hint list below the input as each
// character is typed, ranked by fuzzyScore; Up/Down (or Ctrl-P/Ctrl-N)
// move the highlight, Enter acts on whichever hint is currently
// highlighted:
//   - track: added to the queue and played immediately (mirrors Library's
//     Enter-on-a-file, and the `-t` CLI picker)
//   - artist/album: scopes the Library panel to that exact group, the same
//     presentation showArtistSearch/showAlbumSearch already give a typed
//     substring search, just landing on it directly instead of requiring
//     the term to already narrow it to one match
//   - playlist: loaded and played immediately (mirrors the `-p` CLI
//     picker), rather than the old behavior of just filtering the
//     Playlists panel and leaving Enter-to-load as a second step
//
// An unrecognized prefix, or a kind with zero matches, keeps the popup
// open with feedback in its own title so the query can be adjusted
// immediately without reopening. Esc (handled globally in overlay mode)
// cancels and restores whichever panel was focused before 'f' was
// pressed, same as every other overlay.
//
// Candidate labels are fetched from MPD at most once per kind per popup
// open (lazily, the first time that kind becomes active) and then
// filtered entirely in memory on every keystroke -- refetching per
// keystroke would mean a full-library round-trip on every typed
// character. Playlist candidates need no fetch at all: playlistsPanel
// already keeps every stored playlist's name cached in memory.
//
// The field starts pre-filled with "t " (track search, the most common
// case) so a plain search term can be typed immediately; the cursor lands
// after the space since SetText places it at the end of the new text.
func (a *App) openGlobalSearch() {
	field := tview.NewInputField().SetLabel("Search: ").SetFieldWidth(50)
	field.SetText("t ")
	field.SetBorder(true).SetTitle(" a:artist  al:album  p:playlist  t:track ")

	list := tview.NewList()
	list.ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetSelectedTextColor(colorSelectedFg)
	list.SetSelectedBackgroundColor(colorSelectedBg)
	list.SetBorder(true)

	var trackSongs []mpdclient.Song
	var trackLabels, artistLabels, albumLabels, playlistNames []string
	tracksLoaded, artistsLoaded, albumsLoaded := false, false, false

	loadTracks := func() {
		if tracksLoaded {
			return
		}
		tracksLoaded = true
		songs, err := a.client.AllSongs()
		if err != nil {
			a.showError(err)
			return
		}
		trackSongs = songs
		trackLabels = make([]string, len(songs))
		for i, s := range songs {
			trackLabels[i] = s.DisplayName()
		}
	}
	loadArtists := func() {
		if artistsLoaded {
			return
		}
		artistsLoaded = true
		names, err := a.client.Artists()
		if err != nil {
			a.showError(err)
			return
		}
		artistLabels = nonEmptyStrings(names)
	}
	loadAlbums := func() {
		if albumsLoaded {
			return
		}
		albumsLoaded = true
		names, err := a.client.Albums("")
		if err != nil {
			a.showError(err)
			return
		}
		albumLabels = nonEmptyStrings(names)
	}

	labelsFor := func(kind globalSearchKind) []string {
		switch kind {
		case globalSearchTrack:
			loadTracks()
			return trackLabels
		case globalSearchArtist:
			loadArtists()
			return artistLabels
		case globalSearchAlbum:
			loadAlbums()
			return albumLabels
		case globalSearchPlaylist:
			playlistNames = playlistLabels(a.playlists.pls)
			return playlistNames
		}
		return nil
	}

	hints := &globalSearchHints{}

	// syncHighlight pushes hints.highlight onto the actual list widget --
	// shared by renderList (a full rebuild) and the arrow-key handlers
	// below (which only move the highlight, not the underlying matches).
	syncHighlight := func() {
		if hints.highlight >= 0 {
			list.SetCurrentItem(hints.highlight)
		}
	}

	// renderList repaints the hint list and title from hints' current
	// state -- called after rebuild(), never after a plain move() (an
	// arrow key doesn't change which matches exist, only which one's
	// highlighted, so re-adding every item on every keypress would be
	// wasted work).
	renderList := func() {
		list.Clear()
		for _, idx := range hints.order {
			list.AddItem(hints.labels[idx], "", 0, nil)
		}
		syncHighlight()
		switch {
		case !hints.kindValid:
			field.SetTitle(" a:artist  al:album  p:playlist  t:track ")
		case hints.total == 0:
			field.SetTitle(fmt.Sprintf(" no %s found ", hints.kind.label()))
		default:
			field.SetTitle(fmt.Sprintf(" %s (%d) ", hints.kind.label(), hints.total))
		}
	}

	rebuild := func() {
		hints.rebuild(field.GetText(), labelsFor)
		renderList()
	}
	// Deliberately not called eagerly here: rebuild() is what triggers a
	// kind's MPD fetch (loadTracks/loadArtists/loadAlbums), and running it
	// unconditionally at open -- before the user has typed anything --
	// would mean every single 'f' press pays for a full-library round-trip
	// even if it's immediately cancelled. Hints instead appear from the
	// first keystroke onward (SetChangedFunc below); the Enter case in
	// SetInputCapture also re-runs it first, so confirming still reflects
	// the field's current text even if it was set programmatically (e.g. a
	// test's field.SetText) rather than typed character by character.
	field.SetChangedFunc(func(string) { rebuild() })

	confirm := func() {
		label, idx := hints.current()
		if idx < 0 {
			return
		}
		switch hints.kind {
		case globalSearchTrack:
			song := trackSongs[idx]
			a.closeOverlay()
			a.addAndPlay(song)
			a.focusPanelPrimitive(a.queue.table)
		case globalSearchArtist:
			a.closeOverlay()
			a.library.showArtistSearch(label)
			a.focusPanelPrimitive(a.library.tree)
		case globalSearchAlbum:
			a.closeOverlay()
			a.library.showAlbumSearch(label)
			a.focusPanelPrimitive(a.library.tree)
		case globalSearchPlaylist:
			a.closeOverlay()
			a.loadPlaylist(label)
			a.focusPanelPrimitive(a.queue.table)
		}
	}

	field.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN:
			hints.move(1)
			syncHighlight()
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			hints.move(-1)
			syncHighlight()
			return nil
		case tcell.KeyEnter:
			rebuild()
			confirm()
			return nil
		}
		return event
	})

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(field, 3, 0, true).
		AddItem(list, 0, 1, false)

	a.showOverlay("global-search", centered(layout, 70, 3+maxGlobalSearchHints+2), field)
}
