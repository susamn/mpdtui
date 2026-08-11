package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
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

// parseGlobalSearch splits raw input into a search kind and term. The
// first word selects the kind, case-insensitive: a leading "al" (e.g.
// "al", "album") means album, any other word starting with "a" (e.g. "a",
// "artist") means artist, "p"/"playlist" means playlist, "t"/"track" means
// track -- only that leading prefix matters, so "t", "track", and "trk"
// are all equivalent. Everything after the first word is the term.
// Returns ok=false for empty input, an empty term, or an unrecognized
// leading word.
func parseGlobalSearch(input string) (kind globalSearchKind, term string, ok bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, "", false
	}
	firstWord, rest, _ := strings.Cut(input, " ")
	rest = strings.TrimSpace(rest)
	if firstWord == "" || rest == "" {
		return 0, "", false
	}
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

// openGlobalSearch opens a "f" search reachable from any panel: type a
// prefix word (a/al/p/t) plus a search term, e.g. "a queen" for an artist
// search or "al hello" for an album search. Unlike the panel-local '/'
// searches (which always close on Enter, even with no results, reporting
// failure via a hint-bar flash after the fact), this one stays open on no
// results or an unrecognized prefix -- the popup's own title becomes the
// feedback, so the query can be adjusted immediately without reopening.
// Esc (handled globally in overlay mode) cancels and restores whichever
// panel was focused before 'f' was pressed, same as every other overlay.
//
// The field starts pre-filled with "t " (track search, the most common
// case) so a plain search term can be typed immediately; the cursor lands
// after the space since SetText places it at the end of the new text.
func (a *App) openGlobalSearch() {
	field := tview.NewInputField().SetLabel("Search: ").SetFieldWidth(50)
	field.SetText("t ")
	field.SetBorder(true).SetTitle(" a:artist  al:album  p:playlist  t:track ")
	field.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			return
		}
		kind, term, ok := parseGlobalSearch(field.GetText())
		if !ok {
			field.SetTitle(" start with a/al/p/t, then a search term ")
			return
		}

		var found bool
		var label string
		switch kind {
		case globalSearchArtist:
			found, label = a.library.showArtistSearch(term) > 0, "artist"
		case globalSearchAlbum:
			found, label = a.library.showAlbumSearch(term) > 0, "album"
		case globalSearchPlaylist:
			found, label = a.playlists.setFilter(term) > 0, "playlist"
		case globalSearchTrack:
			found, label = a.library.showSearch(term) > 0, "track"
		}

		if !found {
			field.SetTitle(fmt.Sprintf(" no %s found ", label))
			return
		}

		a.closeOverlay()
		switch kind {
		case globalSearchArtist, globalSearchAlbum, globalSearchTrack:
			a.focusPanelPrimitive(a.library.tree)
		case globalSearchPlaylist:
			a.focusPanelPrimitive(a.playlists.list)
		}
	})

	a.showOverlay("global-search", centered(field, 70, 3), field)
}
