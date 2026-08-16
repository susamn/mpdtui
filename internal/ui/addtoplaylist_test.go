package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// --- playlistPickerHints: pure bookkeeping, no tview/MPD involved ---

func TestPlaylistPickerHintsEmptyQueryShowsAllUnfiltered(t *testing.T) {
	h := &playlistPickerHints{}
	h.rebuild("", []string{"Alpha", "Beta", "Gamma"})

	if h.total != 3 {
		t.Errorf("total = %d, want 3 (empty query matches everything)", h.total)
	}
	if name, idx := h.current(); idx != 0 || name != "Alpha" {
		t.Errorf("current() = (%q, %d), want (%q, 0) -- first candidate highlighted by default", name, idx, "Alpha")
	}
}

func TestPlaylistPickerHintsFiltersAndRanks(t *testing.T) {
	h := &playlistPickerHints{}
	h.rebuild("rock", []string{"Rock Anthems", "Jazz Classics", "Rock Ballads"})

	if h.total != 2 {
		t.Fatalf("total = %d, want 2 (Rock Anthems, Rock Ballads)", h.total)
	}
	if name, _ := h.current(); name != "Rock Anthems" {
		t.Errorf("current() = %q, want %q (tighter/earlier match first)", name, "Rock Anthems")
	}
}

func TestPlaylistPickerHintsZeroMatchesLeavesNothingHighlighted(t *testing.T) {
	h := &playlistPickerHints{}
	h.rebuild("nonexistent-xyz", []string{"Rock Anthems", "Jazz Classics"})

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

func TestPlaylistPickerHintsNavigationWraps(t *testing.T) {
	h := &playlistPickerHints{}
	h.rebuild("", []string{"Alpha", "Beta", "Gamma"})

	h.move(1)
	if name, _ := h.current(); name != "Beta" {
		t.Fatalf("after move(1), highlight = %q, want %q", name, "Beta")
	}
	h.move(1)
	if name, _ := h.current(); name != "Gamma" {
		t.Fatalf("after move(1), highlight = %q, want %q", name, "Gamma")
	}
	h.move(1) // past the end -- wraps to the first
	if name, _ := h.current(); name != "Alpha" {
		t.Errorf("after wrapping move(1), highlight = %q, want %q", name, "Alpha")
	}
	h.move(-1) // backward from the first -- wraps to the last
	if name, _ := h.current(); name != "Gamma" {
		t.Errorf("after wrapping move(-1), highlight = %q, want %q", name, "Gamma")
	}
}

func TestPlaylistPickerHintsJumpFirstAndLast(t *testing.T) {
	h := &playlistPickerHints{}
	h.rebuild("", []string{"Alpha", "Beta", "Gamma"})

	h.move(1) // sit on "Beta" so jumpFirst/jumpLast are visibly not no-ops
	h.jumpLast()
	if name, _ := h.current(); name != "Gamma" {
		t.Errorf("after jumpLast(), highlight = %q, want %q", name, "Gamma")
	}
	h.jumpFirst()
	if name, _ := h.current(); name != "Alpha" {
		t.Errorf("after jumpFirst(), highlight = %q, want %q", name, "Alpha")
	}
}

func TestPlaylistPickerHintsJumpFirstAndLastNoopOnEmptyOrder(t *testing.T) {
	h := &playlistPickerHints{}
	h.rebuild("nonexistent-xyz", []string{"Rock Anthems"})

	h.jumpFirst()
	if _, idx := h.current(); idx != -1 {
		t.Errorf("jumpFirst() on zero matches: idx = %d, want -1", idx)
	}
	h.jumpLast()
	if _, idx := h.current(); idx != -1 {
		t.Errorf("jumpLast() on zero matches: idx = %d, want -1", idx)
	}
}

func TestPlaylistPickerHintsTruncatesButReportsTrueTotal(t *testing.T) {
	names := make([]string, maxAddToPlaylistHints+5)
	for i := range names {
		names[i] = "Playlist"
	}
	h := &playlistPickerHints{}
	h.rebuild("", names)

	if h.total != len(names) {
		t.Errorf("total = %d, want %d (true count before the cap)", h.total, len(names))
	}
	if got := len(h.order); got != maxAddToPlaylistHints {
		t.Errorf("len(order) = %d, want %d (capped)", got, maxAddToPlaylistHints)
	}
}

// --- openAddToPlaylistPicker: tview wiring ---

// selectQueueSong seeds the Queue panel with a single song and selects
// it, mirroring internal/ui/trackmetadata_test.go's own setup for
// exercising Queue-scoped 'a'-key handlers without a live MPD client.
func selectQueueSong(a *App, song mpdclient.Song) {
	a.queue.songs = []mpdclient.Song{song}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows, 0)
}

func TestOpenAddToPlaylistPickerNoopWhenQueueEmpty(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)

	a.openAddToPlaylistPicker()

	if a.mode != modeNormal {
		t.Error("mode after opening the picker with nothing selected should stay modeNormal (no popup)")
	}
}

func TestOpenAddToPlaylistPickerFocusesFieldAndShowsAllPlaylists(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems", "Jazz Classics"})
	selectQueueSong(a, mpdclient.Song{ID: 1, Title: "Track", File: "artist/track.mp3"})
	a.tv.SetFocus(a.queue.table)

	a.openAddToPlaylistPicker()

	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus after openAddToPlaylistPicker = %T, want *tview.InputField", a.tv.GetFocus())
	}
	if a.mode != modeOverlay {
		t.Error("mode after opening the picker should be modeOverlay")
	}
	if got := field.GetTitle(); got != ` Add "Track" to playlist ` {
		t.Errorf("field title = %q, want it to name the selected track", got)
	}
}

func TestOpenAddToPlaylistPickerEscRestoresOriginalFocus(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems"})
	selectQueueSong(a, mpdclient.Song{ID: 1, File: "artist/track.mp3"})
	a.tv.SetFocus(a.queue.table)

	a.openAddToPlaylistPicker()

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape while the picker is open should be consumed, got %v", result)
	}
	if a.mode != modeNormal {
		t.Error("mode after Escape should be modeNormal")
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after Escape = %T, want the originally-focused Queue table", a.tv.GetFocus())
	}
}

func TestOpenAddToPlaylistPickerTabTogglesFocusBetweenFieldAndList(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems"})
	selectQueueSong(a, mpdclient.Song{ID: 1, File: "artist/track.mp3"})
	a.tv.SetFocus(a.queue.table)
	a.openAddToPlaylistPicker()

	field := a.tv.GetFocus().(*tview.InputField)
	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	field.InputHandler()(tab, func(tview.Primitive) {})

	list, ok := a.tv.GetFocus().(*tview.List)
	if !ok {
		t.Fatalf("focus after Tab from field = %T, want *tview.List", a.tv.GetFocus())
	}

	list.InputHandler()(tab, func(tview.Primitive) {})
	if a.tv.GetFocus() != field {
		t.Errorf("focus after Tab from list = %T, want the original field back", a.tv.GetFocus())
	}
}

// TestOpenAddToPlaylistPickerLettersStayTypeableInField guards against
// the navigation keys added for the hint list (j/k/g/G/f) ever leaking
// into the field's own typing -- mirrors
// TestOpenGlobalSearchLettersUsedForNavigationStayTypeableInField.
func TestOpenAddToPlaylistPickerLettersStayTypeableInField(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"jazz greats"})
	selectQueueSong(a, mpdclient.Song{ID: 1, File: "artist/track.mp3"})
	a.tv.SetFocus(a.queue.table)
	a.openAddToPlaylistPicker()
	field := a.tv.GetFocus().(*tview.InputField)

	for _, r := range "jazz" {
		field.InputHandler()(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone), func(tview.Primitive) {})
	}
	if got := field.GetText(); got != "jazz" {
		t.Errorf("field text after typing = %q, want %q (letters must stay literal)", got, "jazz")
	}
}

func TestOpenAddToPlaylistPickerListJKGNavigateHighlight(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems", "Rock Ballads", "Rock Classics"})
	selectQueueSong(a, mpdclient.Song{ID: 1, File: "artist/track.mp3"})
	a.tv.SetFocus(a.queue.table)
	a.openAddToPlaylistPicker()
	field := a.tv.GetFocus().(*tview.InputField)

	field.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})
	list, ok := a.tv.GetFocus().(*tview.List)
	if !ok {
		t.Fatalf("focus after Tab = %T, want *tview.List", a.tv.GetFocus())
	}
	if list.GetItemCount() != 3 {
		t.Fatalf("hint count = %d, want 3", list.GetItemCount())
	}

	list.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone), func(tview.Primitive) {})
	if list.GetCurrentItem() != 1 {
		t.Errorf("highlight after 'j' = %d, want 1", list.GetCurrentItem())
	}
	list.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'G', tcell.ModNone), func(tview.Primitive) {})
	if list.GetCurrentItem() != 2 {
		t.Errorf("highlight after 'G' = %d, want 2 (last)", list.GetCurrentItem())
	}
	list.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone), func(tview.Primitive) {})
	if list.GetCurrentItem() != 0 {
		t.Errorf("highlight after 'g' = %d, want 0 (first)", list.GetCurrentItem())
	}
}

// TestOpenAddToPlaylistPickerConfirmNoopWhenNothingHighlighted proves
// Enter never reaches AddTrackToPlaylist when there's nothing to act on
// -- it would panic against this test's nil MPD client if reached, so a
// passing test here is itself evidence the guard in confirm() works.
func TestOpenAddToPlaylistPickerConfirmNoopWhenNothingHighlighted(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems"})
	selectQueueSong(a, mpdclient.Song{ID: 1, File: "artist/track.mp3"})
	a.tv.SetFocus(a.queue.table)
	a.openAddToPlaylistPicker()
	field := a.tv.GetFocus().(*tview.InputField)

	field.SetText("nonexistent-xyz")
	field.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone), func(tview.Primitive) {})

	field.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
	if a.mode != modeOverlay {
		t.Error("mode after a no-op Enter should stay modeOverlay (popup stays open)")
	}
}

func TestHandleAddOnQueuePanelOpensAddToPlaylistPicker(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems"})
	selectQueueSong(a, mpdclient.Song{ID: 1, File: "artist/track.mp3"})
	a.tv.SetFocus(a.queue.table)

	aKey := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	if result := a.globalInputCapture(aKey); result != nil {
		t.Errorf("'a' on the Queue panel should be consumed (opens the picker), got %v", result)
	}
	if a.mode != modeOverlay {
		t.Error("mode after 'a' on the Queue panel should be modeOverlay")
	}
}

// --- live-MPD: the actual mutation ---

func TestOpenAddToPlaylistPickerAddsTrackNeedsLiveMPD(t *testing.T) {
	c := dialOrSkip(t)
	a := &App{tv: tview.NewApplication(), client: c}
	a.build()

	songs, err := c.AllSongs()
	if err != nil {
		t.Fatalf("AllSongs: %v", err)
	}
	if len(songs) == 0 {
		t.Skip("library has no tracks")
	}
	track := songs[0]

	const testPlaylist = "zz_mpdtui_test_add_to_playlist_picker"
	t.Cleanup(func() { c.PlaylistDelete(testPlaylist) })
	c.PlaylistDelete(testPlaylist) // best-effort: clear any stale leftover

	setPlaylistsForTest(a.playlists, []string{testPlaylist})
	selectQueueSong(a, track)
	a.tv.SetFocus(a.queue.table)

	a.openAddToPlaylistPicker()
	field := a.tv.GetFocus().(*tview.InputField)
	field.SetText(testPlaylist)
	handler := field.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if a.mode != modeNormal {
		t.Fatal("mode after a successful add should be modeNormal (popup closed)")
	}
	tracks, err := c.PlaylistTracks(testPlaylist)
	if err != nil {
		t.Fatalf("PlaylistTracks: %v", err)
	}
	if len(tracks) != 1 || tracks[0].File != track.File {
		t.Errorf("PlaylistTracks(%q) = %+v, want exactly [%q]", testPlaylist, tracks, track.File)
	}
}

func TestOpenAddToPlaylistPickerDuplicateFlashesErrorAndLeavesFileUntouchedNeedsLiveMPD(t *testing.T) {
	c := dialOrSkip(t)
	a := &App{tv: tview.NewApplication(), client: c}
	a.build()

	songs, err := c.AllSongs()
	if err != nil {
		t.Fatalf("AllSongs: %v", err)
	}
	if len(songs) == 0 {
		t.Skip("library has no tracks")
	}
	track := songs[0]

	const testPlaylist = "zz_mpdtui_test_add_to_playlist_picker_dup"
	t.Cleanup(func() { c.PlaylistDelete(testPlaylist) })
	c.PlaylistDelete(testPlaylist)
	if err := c.AddTrackToPlaylist(testPlaylist, track.File); err != nil {
		t.Fatalf("setup AddTrackToPlaylist: %v", err)
	}

	setPlaylistsForTest(a.playlists, []string{testPlaylist})
	selectQueueSong(a, track)
	a.tv.SetFocus(a.queue.table)

	a.openAddToPlaylistPicker()
	field := a.tv.GetFocus().(*tview.InputField)
	field.SetText(testPlaylist)
	handler := field.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if a.mode != modeNormal {
		t.Fatal("mode after a rejected duplicate should still be modeNormal (popup closes either way)")
	}
	tracks, err := c.PlaylistTracks(testPlaylist)
	if err != nil {
		t.Fatalf("PlaylistTracks: %v", err)
	}
	if len(tracks) != 1 {
		t.Errorf("PlaylistTracks(%q) after rejected duplicate = %+v, want still exactly 1 entry", testPlaylist, tracks)
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "already in playlist") {
		t.Errorf("hint bar after a rejected duplicate = %q, want it to mention the track is already in the playlist", got)
	}
}
