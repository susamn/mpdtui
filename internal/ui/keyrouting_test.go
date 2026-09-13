package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/mpdclient"
)

// These cover globalInputCapture's normal-mode dispatch -- which key
// opens what, which pass through to the focused widget, and which are
// reported as having no action here. It is the single largest switch in
// the package and the first thing every keypress hits.

// TestKeysThatOpenAnOverlay walks the keys whose whole job is to put
// something on screen.
func TestKeysThatOpenAnOverlay(t *testing.T) {
	cases := []struct {
		key  rune
		page string
	}{
		{'?', "help"},
		{'f', "global-search"},
		{'e', "settings"},
	}
	for _, tc := range cases {
		t.Run(string(tc.key), func(t *testing.T) {
			a := newFakeApp(t, &fakeMPD{})

			if got := a.globalInputCapture(runeKey(tc.key)); got != nil {
				t.Fatalf("%q was not consumed", tc.key)
			}
			if a.mode != modeOverlay {
				t.Fatalf("%q did not open an overlay", tc.key)
			}
			if a.pages.GetPage(tc.page) == nil {
				t.Errorf("%q opened something other than the %q page", tc.key, tc.page)
			}

			// Escape closes it again and restores normal mode.
			if got := a.globalInputCapture(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
				t.Error("Escape was not consumed while an overlay was open")
			}
			if a.mode != modeNormal {
				t.Errorf("Escape did not close the %q overlay", tc.page)
			}
		})
	}
}

func TestQuitKey(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	if got := a.globalInputCapture(runeKey('q')); got != nil {
		t.Error("'q' was not consumed")
	}
}

// TestPanelFocusKeys covers 1/2/3 outside the Queue, where they switch
// panels rather than rating a track.
func TestPanelFocusKeys(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.tv.SetFocus(a.library.tree)

	cases := []struct {
		key   rune
		panel string
		want  func(*App) any
	}{
		{'1', "Library", func(a *App) any { return a.library.tree }},
		{'2', "Playlists", func(a *App) any { return a.playlists.table }},
		{'3', "Queue", func(a *App) any { return a.queue.table }},
	}
	for _, tc := range cases {
		t.Run(string(tc.key), func(t *testing.T) {
			if got := a.globalInputCapture(runeKey(tc.key)); got != nil {
				t.Fatalf("%q was not consumed", tc.key)
			}
			if any(a.tv.GetFocus()) != tc.want(a) {
				t.Errorf("%q focused %T, want the %s panel", tc.key, a.tv.GetFocus(), tc.panel)
			}
		})
	}

	// 4 and 5 are not panels, so outside the Queue they say so.
	a.tv.SetFocus(a.library.tree)
	a.globalInputCapture(runeKey('4'))
	if got := a.hintBar.GetText(true); !strings.Contains(got, "4") {
		t.Errorf("hint bar = %q, want '4' reported as having no action here", got)
	}
}

// TestRatingKeysInsideTheQueue is the deliberate overload: with the
// Queue focused, 1-5 rate the selected track instead of switching
// panels.
func TestRatingKeysInsideTheQueue(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeAppWithMetaDB(t, f)
	seedQueue(a, f, mpdclient.Song{ID: 1, Pos: 0, Title: "Track", File: "a/b.mp3"})
	a.tv.SetFocus(a.queue.table)
	a.queue.table.Select(queueHeaderRows, 0)

	if got := a.globalInputCapture(runeKey('4')); got != nil {
		t.Fatal("'4' was not consumed in the Queue")
	}

	if a.tv.GetFocus() != a.queue.table {
		t.Error("a rating key moved panel focus")
	}
	meta, err := a.metaDB.Get("a/b.mp3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if meta.Rating != 4 {
		t.Errorf("rating = %d, want 4", meta.Rating)
	}
}

// TestVimMotionKeysPassThrough covers the keys this app deliberately
// does not translate: the Table and TreeView widgets handle them
// natively, so capturing them would mean reimplementing navigation.
func TestVimMotionKeysPassThrough(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.tv.SetFocus(a.queue.table)

	for _, r := range []rune{'j', 'k', 'g', 'G', 'h', 'l'} {
		ev := runeKey(r)
		if got := a.globalInputCapture(ev); got != ev {
			t.Errorf("%q was captured, want it passed through to the focused widget", r)
		}
	}
}

// TestQueueMoveKeysOnlyBindInsideTheQueue covers J/K: they reorder the
// queue when it has focus, and otherwise fall through so the Library
// tree keeps its own motions.
func TestQueueMoveKeysOnlyBindInsideTheQueue(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	seedQueue(a, f,
		mpdclient.Song{ID: 1, Pos: 0},
		mpdclient.Song{ID: 2, Pos: 1},
	)

	a.tv.SetFocus(a.queue.table)
	a.queue.table.Select(queueHeaderRows, 0)
	if got := a.globalInputCapture(runeKey('J')); got != nil {
		t.Error("'J' was not consumed in the Queue")
	}
	if !f.did("move:1->1") {
		t.Errorf("did %v, want the track moved down", f.commands())
	}

	a.tv.SetFocus(a.library.tree)
	for _, r := range []rune{'J', 'K'} {
		ev := runeKey(r)
		if got := a.globalInputCapture(ev); got != ev {
			t.Errorf("%q was captured outside the Queue, want it passed through", r)
		}
	}
}

func TestTabCyclesPanelFocus(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.focusPanel(libraryPanelIdx)

	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	if got := a.globalInputCapture(tab); got != nil {
		t.Fatal("Tab was not consumed")
	}
	if a.tv.GetFocus() == a.library.tree {
		t.Error("Tab did not move focus off the Library")
	}

	backtab := tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
	if got := a.globalInputCapture(backtab); got != nil {
		t.Fatal("Backtab was not consumed")
	}
	if a.tv.GetFocus() != a.library.tree {
		t.Error("Backtab did not move focus back to the Library")
	}
}

// TestEscapeClearsAPanelSearch covers Escape's panel-local meaning in
// normal mode: it backs out of a Library search or a Playlists filter,
// and is otherwise left alone.
func TestEscapeClearsAPanelSearch(t *testing.T) {
	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)

	t.Run("library search", func(t *testing.T) {
		f := &fakeMPD{songs: []mpdclient.Song{{Title: "One", File: "a.mp3"}}}
		a := newFakeApp(t, f)
		a.library.showSearch("one")
		a.tv.SetFocus(a.library.tree)

		if got := a.globalInputCapture(esc); got != nil {
			t.Fatal("Escape was not consumed with a Library search open")
		}
		if a.library.mode == libSearch {
			t.Error("Escape did not leave Library search mode")
		}
	})

	t.Run("playlists filter", func(t *testing.T) {
		a := newFakeApp(t, &fakeMPD{})
		setPlaylistsForTest(a.playlists, []string{"Road Trip", "Chill"})
		a.playlists.setFilter("chill")
		a.tv.SetFocus(a.playlists.table)

		if got := a.globalInputCapture(esc); got != nil {
			t.Fatal("Escape was not consumed with a Playlists filter set")
		}
		if a.playlists.filter != "" {
			t.Errorf("filter = %q after Escape, want it cleared", a.playlists.filter)
		}
	})

	t.Run("nothing to clear", func(t *testing.T) {
		a := newFakeApp(t, &fakeMPD{})
		a.tv.SetFocus(a.queue.table)

		if got := a.globalInputCapture(esc); got == nil {
			t.Error("Escape was consumed with nothing to clear, want it passed through")
		}
	})
}

// TestBackspaceGoesUpTheLibraryTree covers the Library's own back
// navigation, which is bound only while that panel has focus.
func TestBackspaceGoesUpTheLibraryTree(t *testing.T) {
	f := &fakeMPD{songs: []mpdclient.Song{{Title: "One", File: "a.mp3"}}}
	a := newFakeApp(t, f)
	a.library.showSearch("one")
	a.tv.SetFocus(a.library.tree)

	bs := tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone)
	if got := a.globalInputCapture(bs); got != nil {
		t.Fatal("Backspace was not consumed with the Library focused")
	}
	if a.library.mode == libSearch {
		t.Error("Backspace did not back out of the search")
	}

	// Elsewhere it passes through.
	a.tv.SetFocus(a.queue.table)
	if got := a.globalInputCapture(bs); got == nil {
		t.Error("Backspace was consumed outside the Library, want it passed through")
	}
}

// TestUnboundKeySaysSo covers the default arm: an unbound key is
// reported rather than silently doing nothing, so a mistyped shortcut
// is visible.
func TestUnboundKeySaysSo(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.tv.SetFocus(a.queue.table)

	if got := a.globalInputCapture(runeKey('%')); got != nil {
		t.Error("an unbound key was passed through, want it consumed and reported")
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "%") {
		t.Errorf("hint bar = %q, want the unbound key named", got)
	}
}

// TestClearAllSearchesKey covers 'F', which resets every panel's
// persistent filter regardless of which one has focus -- unlike Escape,
// which only clears the panel it was pressed in.
func TestClearAllSearchesKey(t *testing.T) {
	f := &fakeMPD{songs: []mpdclient.Song{{Title: "One", File: "a.mp3"}}}
	a := newFakeApp(t, f)
	a.library.showSearch("one")
	setPlaylistsForTest(a.playlists, []string{"Road Trip"})
	a.playlists.setFilter("road")
	a.tv.SetFocus(a.queue.table) // focused on neither

	if got := a.globalInputCapture(runeKey('F')); got != nil {
		t.Fatal("'F' was not consumed")
	}
	if a.library.mode == libSearch {
		t.Error("'F' did not clear the Library search")
	}
	if a.playlists.filter != "" {
		t.Error("'F' did not clear the Playlists filter")
	}

	// With nothing to clear it says so rather than claiming success.
	a.globalInputCapture(runeKey('F'))
	if got := a.hintBar.GetText(true); !strings.Contains(got, "nothing to clear") {
		t.Errorf("hint bar = %q, want it to report there was nothing to clear", got)
	}
}
