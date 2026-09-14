package picker

import (
	"errors"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/mpdclient"
	"mpdtui/internal/theme"
)

// --- The fuzzy finder itself --------------------------------------------

func key(k tcell.Key) *tcell.EventKey { return tcell.NewEventKey(k, 0, tcell.ModNone) }

func TestFinderStartsShowingEverything(t *testing.T) {
	f := newFinder("Playlists", []string{"Road Trip", "Chill", "Bengali"})

	if got := f.list.GetItemCount(); got != 3 {
		t.Errorf("list shows %d items with no query, want all 3", got)
	}
	if got := f.list.GetCurrentItem(); got != 0 {
		t.Errorf("selection starts at %d, want the first row", got)
	}
}

func TestFinderFiltersAsYouType(t *testing.T) {
	f := newFinder("Tracks", []string{"Ay Hairathe", "Tere Bina", "Barso Re"})

	f.rebuild("bina")
	if got := f.list.GetItemCount(); got != 1 {
		t.Fatalf("query %q matched %d items, want 1", "bina", got)
	}
	main, _ := f.list.GetItemText(0)
	if main != "Tere Bina" {
		t.Errorf("matched %q, want %q", main, "Tere Bina")
	}

	// A query matching nothing empties the list rather than showing
	// stale results from the previous one.
	f.rebuild("zzzzz")
	if got := f.list.GetItemCount(); got != 0 {
		t.Errorf("a non-matching query left %d items, want none", got)
	}

	// Clearing the query brings everything back.
	f.rebuild("")
	if got := f.list.GetItemCount(); got != 3 {
		t.Errorf("clearing the query left %d items, want all 3", got)
	}
}

// TestFinderConfirmResolvesThroughTheFilteredOrder is the bug this
// indirection exists to prevent: after filtering, row 0 is not
// necessarily labels[0], so confirming must map the row back through
// the current ordering.
func TestFinderConfirmResolvesThroughTheFilteredOrder(t *testing.T) {
	labels := []string{"Alpha", "Beta", "Gamma"}
	f := newFinder("Tracks", labels)

	f.rebuild("gam") // only "Gamma", at row 0
	f.handleKey(key(tcell.KeyEnter))

	if f.selected != 2 {
		t.Errorf("selected = %d, want 2 (the index of %q in the original labels)", f.selected, "Gamma")
	}
	if !f.stopped {
		t.Error("confirming did not stop the finder")
	}
}

func TestFinderCancelSelectsNothing(t *testing.T) {
	for _, k := range []tcell.Key{tcell.KeyEscape, tcell.KeyCtrlC} {
		f := newFinder("Tracks", []string{"Alpha", "Beta"})
		f.handleKey(key(k))

		if f.selected != -1 {
			t.Errorf("after cancel, selected = %d, want -1", f.selected)
		}
		if !f.stopped {
			t.Error("cancelling did not stop the finder")
		}
	}
}

// TestFinderConfirmOnAnEmptyListCancels covers the guard: with nothing
// matching, Enter must not pick an arbitrary item.
func TestFinderConfirmOnAnEmptyListCancels(t *testing.T) {
	f := newFinder("Tracks", []string{"Alpha", "Beta"})
	f.rebuild("no such thing")

	f.handleKey(key(tcell.KeyEnter))

	if f.selected != -1 {
		t.Errorf("confirming an empty list selected %d, want -1", f.selected)
	}
}

func TestFinderNavigationWrapsBothWays(t *testing.T) {
	f := newFinder("Tracks", []string{"A", "B", "C"})

	for _, k := range []tcell.Key{tcell.KeyDown, tcell.KeyCtrlN} {
		f.list.SetCurrentItem(0)
		f.handleKey(key(k))
		if got := f.list.GetCurrentItem(); got != 1 {
			t.Errorf("after %v, selection = %d, want 1", k, got)
		}
	}
	for _, k := range []tcell.Key{tcell.KeyUp, tcell.KeyCtrlP} {
		f.list.SetCurrentItem(0)
		f.handleKey(key(k))
		if got := f.list.GetCurrentItem(); got != 2 {
			t.Errorf("after %v from the top, selection = %d, want it wrapped to the last row", k, got)
		}
	}

	// Past the end wraps back to the top.
	f.list.SetCurrentItem(2)
	f.handleKey(key(tcell.KeyDown))
	if got := f.list.GetCurrentItem(); got != 0 {
		t.Errorf("after Down from the last row, selection = %d, want it wrapped to 0", got)
	}
}

// TestFinderPassesTypingThrough covers the fall-through: anything the
// finder does not claim has to reach the input field so the query can
// actually be typed.
func TestFinderPassesTypingThrough(t *testing.T) {
	f := newFinder("Tracks", []string{"A"})

	for _, ev := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone),
		key(tcell.KeyBackspace),
		key(tcell.KeyBackspace2),
		key(tcell.KeyLeft),
	} {
		if got := f.handleKey(ev); got != ev {
			t.Errorf("key %v was swallowed, want it passed through to the input field", ev.Key())
		}
	}
	if f.stopped {
		t.Error("typing stopped the finder")
	}
}

func TestMoveSelectionOnAnEmptyList(t *testing.T) {
	f := newFinder("Tracks", nil)
	// Must not panic or divide by zero.
	moveSelection(f.list, 1)
	moveSelection(f.list, -1)
}

// --- What each picker does with the choice ------------------------------

type fakeConn struct {
	pls   []mpdclient.Playlist
	songs []mpdclient.Song
	calls []string
	err   error
}

func (f *fakeConn) Playlists() ([]mpdclient.Playlist, error) { return f.pls, f.err }
func (f *fakeConn) AllSongs() ([]mpdclient.Song, error)      { return f.songs, f.err }
func (f *fakeConn) PlaylistLoad(name string) error {
	f.calls = append(f.calls, "load:"+name)
	return f.err
}
func (f *fakeConn) QueueAddID(uri string) (int, error) {
	f.calls = append(f.calls, "add:"+uri)
	if f.err != nil {
		return 0, f.err
	}
	return 42, nil
}
func (f *fakeConn) PlayID(id int) error {
	f.calls = append(f.calls, "play")
	return f.err
}

// stubPick replaces the interactive finder for the duration of a test,
// standing in for the user's choice.
func stubPick(t *testing.T, choose func(labels []string) (int, error)) {
	t.Helper()
	old := pick
	t.Cleanup(func() { pick = old })
	pick = func(_ string, labels []string) (int, error) { return choose(labels) }
}

func TestRunPlaylistPickerLoadsTheChosenPlaylist(t *testing.T) {
	c := &fakeConn{pls: []mpdclient.Playlist{{Name: "Chill"}, {Name: "Road Trip"}}}

	var offered []string
	stubPick(t, func(labels []string) (int, error) { offered = labels; return 1, nil })

	if err := RunPlaylistPicker(c, ""); err != nil {
		t.Fatalf("RunPlaylistPicker: %v", err)
	}
	if len(offered) != 2 || offered[0] != "Chill" {
		t.Errorf("offered %v, want every playlist name", offered)
	}
	if len(c.calls) != 1 || c.calls[0] != "load:Road Trip" {
		t.Errorf("did %v, want the chosen playlist loaded", c.calls)
	}
}

func TestRunPlaylistPickerCancelDoesNothing(t *testing.T) {
	c := &fakeConn{pls: []mpdclient.Playlist{{Name: "Chill"}}}
	stubPick(t, func([]string) (int, error) { return -1, nil })

	if err := RunPlaylistPicker(c, ""); err != nil {
		t.Fatalf("RunPlaylistPicker: %v", err)
	}
	if len(c.calls) != 0 {
		t.Errorf("did %v after cancelling, want nothing", c.calls)
	}
}

func TestRunPlaylistPickerWithNoPlaylists(t *testing.T) {
	c := &fakeConn{}
	called := false
	stubPick(t, func([]string) (int, error) { called = true; return 0, nil })

	if err := RunPlaylistPicker(c, ""); err != nil {
		t.Fatalf("RunPlaylistPicker: %v", err)
	}
	if called {
		t.Error("opened a picker with no playlists to choose from")
	}
}

func TestRunPlaylistPickerReportsAFailedList(t *testing.T) {
	c := &fakeConn{err: errors.New("offline")}
	stubPick(t, func([]string) (int, error) { return 0, nil })

	err := RunPlaylistPicker(c, "")
	if err == nil || !strings.Contains(err.Error(), "list playlists") {
		t.Errorf("error = %v, want it to say listing playlists failed", err)
	}
}

func TestRunTrackPickerQueuesAndPlaysTheChoice(t *testing.T) {
	c := &fakeConn{songs: []mpdclient.Song{
		{Title: "Ay Hairathe", File: "a/1.mp3"},
		{Title: "Tere Bina", File: "a/2.mp3"},
	}}
	stubPick(t, func([]string) (int, error) { return 1, nil })

	if err := RunTrackPicker(c, ""); err != nil {
		t.Fatalf("RunTrackPicker: %v", err)
	}
	want := []string{"add:a/2.mp3", "play"}
	if len(c.calls) != 2 || c.calls[0] != want[0] || c.calls[1] != want[1] {
		t.Errorf("did %v, want %v -- the chosen track queued and then played", c.calls, want)
	}
}

func TestRunTrackPickerCancelDoesNothing(t *testing.T) {
	c := &fakeConn{songs: []mpdclient.Song{{Title: "T", File: "a.mp3"}}}
	stubPick(t, func([]string) (int, error) { return -1, nil })

	if err := RunTrackPicker(c, ""); err != nil {
		t.Fatalf("RunTrackPicker: %v", err)
	}
	if len(c.calls) != 0 {
		t.Errorf("did %v after cancelling, want nothing", c.calls)
	}
}

func TestRunTrackPickerWithNoTracks(t *testing.T) {
	c := &fakeConn{}
	called := false
	stubPick(t, func([]string) (int, error) { called = true; return 0, nil })

	if err := RunTrackPicker(c, ""); err != nil {
		t.Fatalf("RunTrackPicker: %v", err)
	}
	if called {
		t.Error("opened a picker with no tracks to choose from")
	}
}

func TestRunTrackPickerReportsFailures(t *testing.T) {
	stubPick(t, func([]string) (int, error) { return 0, nil })

	if err := RunTrackPicker(&fakeConn{err: errors.New("offline")}, ""); err == nil ||
		!strings.Contains(err.Error(), "list tracks") {
		t.Errorf("error = %v, want it to say listing tracks failed", err)
	}
}

// TestRunPickerSurfacesAFinderError covers the arm where the terminal
// itself fails rather than the user choosing.
func TestRunPickerSurfacesAFinderError(t *testing.T) {
	stubPick(t, func([]string) (int, error) { return -1, errors.New("no terminal") })

	if err := RunPlaylistPicker(&fakeConn{pls: []mpdclient.Playlist{{Name: "A"}}}, ""); err == nil {
		t.Error("a finder failure was swallowed")
	}
	if err := RunTrackPicker(&fakeConn{songs: []mpdclient.Song{{File: "a.mp3"}}}, ""); err == nil {
		t.Error("a finder failure was swallowed")
	}
}

// --- Colors -------------------------------------------------------------

func TestInitColorsAndContrast(t *testing.T) {
	initColors("") // no file: internal/theme's own default palette

	if colorAccent == 0 && colorSelectedBg == 0 {
		t.Error("colors are all zero after initColors")
	}
	// The selected foreground must be readable on the selected
	// background, whatever the theme chose.
	if colorSelectedFg == colorSelectedBg {
		t.Error("selected foreground and background are the same color")
	}

	if got := hexColor(""); got != tcell.ColorDefault {
		t.Errorf("hexColor(\"\") = %v, want ColorDefault", got)
	}
	if got, want := hexColor(theme.Color("#ff8800")), tcell.GetColor("#ff8800"); got != want {
		t.Errorf("hexColor = %v, want %v", got, want)
	}

	if got := contrastColor(tcell.NewRGBColor(250, 250, 250)); got != tcell.ColorBlack {
		t.Errorf("contrastColor(near-white) = %v, want black", got)
	}
	if got := contrastColor(tcell.NewRGBColor(5, 5, 5)); got != tcell.ColorWhite {
		t.Errorf("contrastColor(near-black) = %v, want white", got)
	}
}
