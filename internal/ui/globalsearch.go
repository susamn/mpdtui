package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/lyricsindex"
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
	globalSearchLyrics
)

func (k globalSearchKind) label() string {
	switch k {
	case globalSearchArtist:
		return "artist"
	case globalSearchAlbum:
		return "album"
	case globalSearchPlaylist:
		return "playlist"
	case globalSearchLyrics:
		return "lyrics"
	default:
		return "track"
	}
}

// maxGlobalSearchHints caps how many fuzzy matches the hint list shows at
// once -- a glanceable fzf-style shortlist, not a full result dump.
const maxGlobalSearchHints = 10

// lyricsMatchColor tags the matched query text inside a lyrics hint's
// excerpt (theme-derived, see deriveColors) -- everything else in the
// row (track name, excerpt context) stays at the terminal's default
// foreground so the highlight is the one thing that stands out.
var lyricsMatchColor string

// lyricsHintItem builds one row of the "l" search hint list: the track
// label, a dim middle dot, then a one-line excerpt of the matching
// lyrics with the search term colored (see lyricsindex.Snippet). Every
// piece of data-derived text is escaped for tview's style-tag parser.
// Falls back to just the (escaped) label when term is empty or no
// excerpt can be found.
func lyricsHintItem(label, rawText, term string) string {
	label = tview.Escape(label)
	before, match, after, ok := lyricsindex.Snippet(rawText, term, lyricsindex.SnippetRadius)
	if !ok {
		return label
	}
	parts := make([]string, 0, 3)
	if before != "" {
		parts = append(parts, tview.Escape(before))
	}
	parts = append(parts, fmt.Sprintf("[%s]%s[-]", lyricsMatchColor, tview.Escape(match)))
	if after != "" {
		parts = append(parts, tview.Escape(after))
	}
	return label + "  [::d]·[-:-:-]  " + strings.Join(parts, " ")
}

// parseGlobalSearchKind splits raw input into a search kind and term. The
// first word selects the kind, case-insensitive: a leading "al" (e.g.
// "al", "album") means album, any other word starting with "a" (e.g. "a",
// "artist") means artist, "l"/"lyrics" means lyrics, "p"/"playlist" means
// playlist, "t"/"track" means track -- only that leading prefix matters,
// so "t", "track", and "trk" are all equivalent. Everything after the
// first word is the term.
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
	case strings.HasPrefix(word, "l"):
		return globalSearchLyrics, rest, true
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

// filterSubstringHints is rankGlobalSearchHints' counterpart for lyrics
// search: it keeps the indices of targets that contain term as a case-
// and diacritic-insensitive substring, in targets' own order. Lyrics
// search has no meaningful relevance score to sort by -- a phrase is
// either somewhere in a track's lyrics or it isn't -- and prose is long
// enough that fuzzyScore's subsequence match would pair almost any short
// query with almost every track. targets are expected already folded
// (lyricsindex.Fold, as stored in the index); only the term is folded
// here, with the same function. An empty term matches everything; total
// is the full match count before the maxGlobalSearchHints cap.
func filterSubstringHints(term string, targets []string) (shown []int, total int) {
	needle := lyricsindex.Fold(term)
	matched := make([]int, 0, len(targets))
	for i, t := range targets {
		if needle == "" || strings.Contains(t, needle) {
			matched = append(matched, i)
		}
	}
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
	term      string   // the search term from the last rebuild, for renderers that need it (lyrics excerpt highlighting)
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
// The optional matchTextFor, when supplied and returning a non-nil slice
// for the active kind, is what term is ranked against instead of the
// display labels -- lyrics search shows track names in h.labels but
// matches against a parallel slice of each track's sidecar-lyrics text.
// That slice must stay index-aligned with labelsFor(kind), since confirm
// looks the chosen hint's underlying Song up by the same index.
func (h *globalSearchHints) rebuild(text string, labelsFor func(globalSearchKind) []string, matchTextFor ...func(globalSearchKind) []string) {
	kind, term, ok := parseGlobalSearchKind(text)
	h.kind, h.kindValid, h.term = kind, ok, term
	h.labels, h.order, h.total, h.highlight = nil, nil, 0, -1
	if !ok {
		h.term = ""
		return
	}
	h.labels = labelsFor(kind)
	targets := h.labels
	if len(matchTextFor) > 0 && matchTextFor[0] != nil {
		if mt := matchTextFor[0](kind); mt != nil {
			targets = mt
		}
	}
	if kind == globalSearchLyrics {
		h.order, h.total = filterSubstringHints(term, targets)
	} else {
		h.order, h.total = rankGlobalSearchHints(term, targets)
	}
	if len(h.order) > 0 {
		h.highlight = 0
	}
}

// move shifts the highlight by delta, wrapping around; a no-op (stays -1)
// on an empty hint list.
func (h *globalSearchHints) move(delta int) {
	h.highlight = moveHintHighlight(h.highlight, len(h.order), delta)
}

// jumpFirst/jumpLast move the highlight straight to either end of the
// hint list ('g'/'G', matching Library/Queue's own native vim bindings).
// A no-op on an empty list.
func (h *globalSearchHints) jumpFirst() {
	if len(h.order) > 0 {
		h.highlight = 0
	}
}

func (h *globalSearchHints) jumpLast() {
	if len(h.order) > 0 {
		h.highlight = len(h.order) - 1
	}
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
// prefix word (a/al/l/p/t) plus a search term, e.g. "a queen" for artists
// or "al hello" for albums. Matching candidates for the active kind
// appear live, fzf-style, in a hint list below the input as each
// character is typed, ranked by fuzzyScore -- except lyrics ("l"), which
// matches the term as a plain case/accent-insensitive substring against
// each track's sidecar lyrics text (see filterSubstringHints) and shows
// the matching tracks. Lyrics hits are read from the prebuilt index
// (internal/lyricsindex, rebuilt with the 'I' key); this popup never
// scans the filesystem itself. An index that's missing or empty just
// yields no lyrics hits, with a hint to build one.
//
// The popup has two focus states, toggled with Tab/Backtab (from the hint
// list, 'f' also returns to typing -- the same muscle memory that opened
// the popup in the first place):
//   - field focused (typing): Up/Down or Ctrl-P/Ctrl-N still move the
//     highlight without leaving typing mode, for quickly skimming a
//     couple of results; Enter confirms the highlighted hint and closes
//     the popup.
//   - list focused (navigating): Up/Down/Ctrl-P/Ctrl-N/j/k move the
//     highlight, g/G jump to the first/last hint (matching Library/Queue's
//     own vim bindings), Enter confirms and closes same as above, and 'a'
//     adds the highlighted hint to the queue WITHOUT playing it and
//     WITHOUT closing the popup -- so several tracks can be queued
//     back-to-back: type a term, j/k to the one you want, 'a', 'f' back to
//     the field, type the next term, repeat.
//
// Enter (from either focus state) acts on whichever hint is currently
// highlighted:
//   - track / lyrics: added to the queue and played immediately (mirrors
//     Library's Enter-on-a-file, and the `-t` CLI picker); 'a' instead
//     only adds it (mirrors Library/Playlists' own Enter-vs-'a'
//     convention). A lyrics hint is just a track found by its words.
//   - artist/album: scopes the Library panel to that exact group, the same
//     presentation showArtistSearch/showAlbumSearch already give a typed
//     substring search, just landing on it directly instead of requiring
//     the term to already narrow it to one match; 'a' is invalid for these
//     two kinds -- there's no defined "add without navigating" behavior,
//     and queueing an entire artist's or album's catalog from a stray
//     keypress is a much bigger, easier-to-regret action than a single
//     track or playlist
//   - playlist: loaded and played immediately (mirrors the `-p` CLI
//     picker), rather than the old behavior of just filtering the
//     Playlists panel and leaving Enter-to-load as a second step; 'a'
//     appends it to the queue instead (mirrors appendPlaylist, used
//     identically by Library/Playlists' own 'a' key)
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
	field.SetBorder(true).SetTitle(" a:artist  al:album  l:lyrics  p:playlist  t:track ")

	list := tview.NewList()
	list.ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetSelectedTextColor(colorSelectedFg)
	list.SetSelectedBackgroundColor(colorSelectedBg)
	list.SetBorder(true)

	var trackSongs []mpdclient.Song
	var trackLabels, artistLabels, albumLabels, playlistNames []string
	// Lyrics hits come from the prebuilt index (internal/lyricsindex), not
	// a live MPD/filesystem scan, so all the popup keeps is four parallel
	// slices: the track's MPD path, its display label, its folded lyrics
	// text to match against, and the raw lyrics text to excerpt for the
	// hint row.
	var lyricsFiles, lyricsLabels, lyricsTexts, lyricsRaw []string
	tracksLoaded, artistsLoaded, albumsLoaded, lyricsLoaded := false, false, false, false

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

	// loadLyrics reads the prebuilt lyrics index off disk once per popup
	// session (lazily, the first keystroke after an "l" prefix) -- a single
	// sequential SQLite read, no MPD call and no music-directory walk. The
	// index is only ever as fresh as its last rebuild (the 'I' key, see
	// handleReindexLyrics); an empty or missing one just yields no hits,
	// with a nudge toward building it.
	loadLyrics := func() {
		if lyricsLoaded {
			return
		}
		lyricsLoaded = true
		entries, err := lyricsindex.Load(a.cfg.LyricsIndexPath)
		if err != nil {
			a.showError(err)
			return
		}
		if len(entries) == 0 {
			a.showMessage("lyrics index is empty -- press 'I' to build it (needs music_dir)")
			return
		}
		lyricsFiles = make([]string, len(entries))
		lyricsLabels = make([]string, len(entries))
		lyricsTexts = make([]string, len(entries))
		lyricsRaw = make([]string, len(entries))
		for i, e := range entries {
			lyricsFiles[i] = e.File
			lyricsLabels[i] = mpdclient.Song{File: e.File, Artist: e.Artist, Title: e.Title}.DisplayName()
			lyricsTexts[i] = e.TextFolded
			lyricsRaw[i] = e.Text
		}
	}

	labelsFor := func(kind globalSearchKind) []string {
		switch kind {
		case globalSearchTrack:
			loadTracks()
			return trackLabels
		case globalSearchLyrics:
			loadLyrics()
			return lyricsLabels
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

	// matchTextFor feeds lyrics search its per-track lyrics text to rank
	// against; every other kind ranks against its display labels, so nil.
	matchTextFor := func(kind globalSearchKind) []string {
		if kind == globalSearchLyrics {
			return lyricsTexts
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
			text := hints.labels[idx]
			// Lyrics hits show why they matched: the track name plus a
			// one-line excerpt of the lyrics with the query term colored.
			if hints.kind == globalSearchLyrics && hints.term != "" {
				text = lyricsHintItem(hints.labels[idx], lyricsRaw[idx], hints.term)
			}
			list.AddItem(text, "", 0, nil)
		}
		syncHighlight()
		switch {
		case !hints.kindValid:
			field.SetTitle(" a:artist  al:album  l:lyrics  p:playlist  t:track ")
		case hints.total == 0:
			field.SetTitle(fmt.Sprintf(" no %s found ", hints.kind.label()))
		default:
			field.SetTitle(fmt.Sprintf(" %s (%d) ", hints.kind.label(), hints.total))
		}
	}

	rebuild := func() {
		hints.rebuild(field.GetText(), labelsFor, matchTextFor)
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
		case globalSearchLyrics:
			a.closeOverlay()
			a.addAndPlay(mpdclient.Song{File: lyricsFiles[idx]})
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

	// addToQueue is 'a', reachable only while list has focus (see list's
	// own InputCapture below) -- 'a' stays a plain typeable letter while
	// field is focused, exactly like the rest of this app only treats 'a'
	// as the add-without-play key while a non-text panel (Library,
	// Playlists) has focus. Unlike confirm, this deliberately does NOT
	// close the popup: the whole point is letting several tracks get
	// queued back-to-back -- type a term, arrow/j-k to the one you want,
	// 'a', 'f' or Tab back to the field, type the next term, repeat --
	// instead of the popup closing after every single add. Mirrors
	// appendPlaylist/queueAddPath's own "add without playing" semantics
	// already used by Library/Playlists' 'a' key. Artist/album have no
	// defined "add without navigating to Library" behavior -- queueing an
	// entire artist's or album's catalog from a stray keypress is a much
	// bigger, easier-to-regret action than a single track or playlist, so
	// 'a' is simply invalid for those two kinds rather than guessing at
	// one.
	addToQueue := func() {
		label, idx := hints.current()
		if idx < 0 {
			return
		}
		switch hints.kind {
		case globalSearchTrack, globalSearchLyrics:
			file, name := trackSongs[idx].File, trackSongs[idx].DisplayName()
			if hints.kind == globalSearchLyrics {
				file, name = lyricsFiles[idx], lyricsLabels[idx]
			}
			if err := a.client.QueueAdd(file); err != nil {
				a.showError(err)
				return
			}
			a.queue.refresh()
			a.showMessage("added to queue: " + name)
		case globalSearchPlaylist:
			a.appendPlaylist(label)
		default:
			a.invalidKey("a")
		}
	}

	// focusList/focusField toggle between typing (field) and navigating
	// the hint list (list has no text input of its own, so Down/Up/j/k/
	// g/G/Enter/a only make sense once it actually holds keyboard focus).
	// Bound to Tab/Backtab in both directions (either key toggles,
	// forgiving of however a given terminal reports Shift-Tab) and to 'f'
	// from list back to field specifically -- 'f' is already the muscle
	// memory for "start searching" everywhere else in this app.
	focusList := func() { a.tv.SetFocus(list) }
	focusField := func() { a.tv.SetFocus(field) }

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
		case tcell.KeyTab, tcell.KeyBacktab:
			focusList()
			return nil
		case tcell.KeyEnter:
			rebuild()
			confirm()
			return nil
		}
		return event
	})

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN:
			hints.move(1)
			syncHighlight()
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			hints.move(-1)
			syncHighlight()
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			focusField()
			return nil
		case tcell.KeyEnter:
			confirm()
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'j':
				hints.move(1)
				syncHighlight()
				return nil
			case 'k':
				hints.move(-1)
				syncHighlight()
				return nil
			case 'g':
				hints.jumpFirst()
				syncHighlight()
				return nil
			case 'G':
				hints.jumpLast()
				syncHighlight()
				return nil
			case 'a':
				addToQueue()
				return nil
			case 'f':
				focusField()
				return nil
			}
		}
		return event
	})

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(field, 3, 0, true).
		AddItem(list, 0, 1, false)

	// Wider than a plain name list needs, to give lyrics excerpts
	// (track name + a middle dot + ~64 chars of context) room before
	// tview.List truncates the row.
	a.showOverlay("global-search", centered(layout, 96, 3+maxGlobalSearchHints+2), field)
}
