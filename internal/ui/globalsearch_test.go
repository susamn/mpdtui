package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

func TestParseGlobalSearch(t *testing.T) {
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
		{"t help me", globalSearchTrack, "help me", true},
		{"track help me", globalSearchTrack, "help me", true},
		{"  t   spaced term  ", globalSearchTrack, "spaced term", true},
		{"x hello", 0, "", false}, // unrecognized prefix
		{"a", 0, "", false},       // no term
		{"a   ", 0, "", false},    // no term after trimming
		{"", 0, "", false},        // empty input
		{"   ", 0, "", false},     // whitespace-only input
	}
	for _, tc := range cases {
		kind, term, ok := parseGlobalSearch(tc.input)
		if ok != tc.wantOK {
			t.Errorf("parseGlobalSearch(%q) ok = %v, want %v", tc.input, ok, tc.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if kind != tc.wantKind {
			t.Errorf("parseGlobalSearch(%q) kind = %v, want %v", tc.input, kind, tc.wantKind)
		}
		if term != tc.wantTerm {
			t.Errorf("parseGlobalSearch(%q) term = %q, want %q", tc.input, term, tc.wantTerm)
		}
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

func TestOpenGlobalSearchPlaylistMatchClosesAndFocusesPlaylists(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems", "Jazz Classics"})
	a.tv.SetFocus(a.queue.table)
	a.openGlobalSearch()

	field := a.tv.GetFocus().(*tview.InputField)
	field.SetText("p Rock")
	handler := field.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if a.mode != modeNormal {
		t.Error("mode after a playlist match should return to modeNormal (popup closed)")
	}
	if a.tv.GetFocus() != a.playlists.table {
		t.Errorf("focus after a playlist match = %T, want the Playlists table", a.tv.GetFocus())
	}
	if a.playlists.filter != "Rock" {
		t.Errorf("playlists.filter = %q, want %q", a.playlists.filter, "Rock")
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
