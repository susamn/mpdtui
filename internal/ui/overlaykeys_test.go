package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/lyricsindex"
	"mpdtui/internal/mpdclient"
)

// These cover globalInputCapture's overlay branch: which overlays let a
// transport key or 'q' through while they are open, and which do not.

// TestTransportKeysStayLiveUnderTheLyricsViewer covers the deliberate
// exception: the lyrics viewer and the two catalog pickers are meant to
// be used *while music plays*, so pausing or skipping must not require
// closing them first.
func TestTransportKeysStayLiveUnderTheLyricsViewer(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	a.openLyricsViewer()
	if a.tv.GetFocus() != a.lyricsViewer {
		t.Fatalf("focus is %T, want the lyrics viewer", a.tv.GetFocus())
	}

	if got := a.globalInputCapture(runeKey(' ')); got != nil {
		t.Error("Space was not consumed under the lyrics viewer")
	}
	if !f.did("toggle") {
		t.Errorf("did %v, want play/pause to still work", f.commands())
	}

	// 't' is the viewer's own key (cycle lyrics format), claimed before
	// the transport cluster since it is not one of those runes.
	if got := a.globalInputCapture(runeKey('t')); got != nil {
		t.Error("'t' was not consumed by the lyrics viewer")
	}

	// 'q' still quits from a read-only overlay.
	if got := a.globalInputCapture(runeKey('q')); got != nil {
		t.Error("'q' was not consumed under the lyrics viewer")
	}

	// 'y' closes it again.
	a.openLyricsViewer()
	if got := a.globalInputCapture(runeKey('y')); got != nil {
		t.Error("'y' was not consumed")
	}
	if a.mode != modeNormal {
		t.Error("'y' did not close the lyrics viewer")
	}
}

// TestTrackInfoCardNavigationKeys covers the card's own j/k, which it
// gets instead of the transport cluster -- unlike the lyrics viewer, it
// is a scrollable card rather than something to watch while playing.
func TestTrackInfoCardNavigationKeys(t *testing.T) {
	f := &fakeMPD{
		status: mpdclient.Status{SongID: 1},
		song:   mpdclient.Song{ID: 1, Title: "Ay Hairathe", File: "a/b.mp3"},
	}
	a := newFakeApp(t, f)
	seedQueue(a, f, mpdclient.Song{ID: 1, Pos: 0, Title: "Ay Hairathe", File: "a/b.mp3"})
	a.tv.SetFocus(a.queue.table)
	a.queue.table.Select(queueHeaderRows, 0)

	a.openTrackInfo()
	if a.tv.GetFocus() != a.trackInfo {
		t.Fatalf("focus is %T, want the Track Info card", a.tv.GetFocus())
	}

	for _, r := range []rune{'j', 'k'} {
		if got := a.globalInputCapture(runeKey(r)); got != nil {
			t.Errorf("%q was not consumed by the Track Info card", r)
		}
	}
	if got := a.globalInputCapture(runeKey('q')); got != nil {
		t.Error("'q' was not consumed under the Track Info card")
	}
	if got := a.globalInputCapture(runeKey('i')); got != nil {
		t.Error("'i' was not consumed")
	}
	if a.mode != modeNormal {
		t.Error("'i' did not close the Track Info card")
	}
}

// TestEscapeClosesAnyOverlay covers the one key every overlay shares.
func TestEscapeClosesAnyOverlay(t *testing.T) {
	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	for _, tc := range []struct {
		name string
		open func(*App)
	}{
		{"help", (*App).openHelp},
		{"global search", (*App).openGlobalSearch},
		{"settings", (*App).openSettings},
		{"lyrics viewer", (*App).openLyricsViewer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newFakeApp(t, &fakeMPD{})
			tc.open(a)
			if a.mode != modeOverlay {
				t.Fatal("the overlay did not open")
			}
			if got := a.globalInputCapture(esc); got != nil {
				t.Error("Escape was not consumed")
			}
			if a.mode != modeNormal {
				t.Error("Escape did not close the overlay")
			}
		})
	}
}

// TestBookmarkManagerKeyRouting covers the bookmark overlay's arm of
// globalInputCapture, including transport keys staying live while
// browsing and going literal while typing a note.
func TestBookmarkManagerKeyRouting(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeAppWithMetaDB(t, f)
	song := mpdclient.Song{ID: 1, Pos: 0, File: "a/b.mp3", Title: "Track"}
	seedQueue(a, f, song)
	a.openBookmarkManager(song)

	if !a.bookmarkPicker.focused() {
		t.Fatal("the bookmark manager does not hold focus")
	}

	// Browsing: transport keys and 'q' stay live.
	if got := a.globalInputCapture(runeKey(' ')); got != nil {
		t.Error("Space was not consumed while browsing bookmarks")
	}
	if !f.did("toggle") {
		t.Errorf("did %v, want play/pause to work while browsing bookmarks", f.commands())
	}
	if got := a.globalInputCapture(runeKey('q')); got != nil {
		t.Error("'q' was not consumed while browsing bookmarks")
	}

	// Typing a note: the same keys must stay literal.
	f.calls = nil
	a.bookmarkPicker.startAdd()
	a.globalInputCapture(runeKey('s'))
	if len(f.commands()) != 0 {
		t.Errorf("did %v while typing a note, want the key left literal", f.commands())
	}

	// Escape backs out of typing, then closes the manager.
	a.globalInputCapture(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if a.mode != modeOverlay {
		t.Error("Escape while typing closed the whole manager")
	}
	a.globalInputCapture(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if a.mode != modeNormal {
		t.Error("Escape did not close the bookmark manager")
	}
}

// --- Lyrics search kind -------------------------------------------------

// buildLyricsIndex writes a small real index, which is the only way to
// exercise the global search's lyrics kind.
func buildLyricsIndex(t *testing.T, tracks []lyricsindex.Track, sidecars map[string]string) (indexPath, musicDir string) {
	t.Helper()
	musicDir = t.TempDir()
	for rel, text := range sidecars {
		full := filepath.Join(musicDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	indexPath = filepath.Join(t.TempDir(), "lyrics.db")
	if _, err := lyricsindex.Reindex(context.Background(), indexPath, musicDir, tracks, nil); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	return indexPath, musicDir
}

func TestGlobalSearchLyricsKind(t *testing.T) {
	indexPath, musicDir := buildLyricsIndex(t,
		[]lyricsindex.Track{
			{File: "rahman/guru/05.mp3", Artist: "Hariharan", Title: "Ay Hairathe"},
			{File: "rahman/guru/02.mp3", Artist: "Chinmayi", Title: "Tere Bina"},
		},
		map[string]string{
			"rahman/guru/05.txt": "ay hairathe ashiqui\nsomething about the moonlight",
			"rahman/guru/02.txt": "tere bina beswadi beswadi ratiyan",
		},
	)

	f := searchLibrary()
	a := newFakeApp(t, f)
	a.musicDir = musicDir
	a.cfg.LyricsIndexPath = indexPath

	field := openSearchField(t, a)
	field.SetText("l moonlight")

	sendKey(field, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if !f.did("queueaddid:rahman/guru/05.mp3") {
		t.Errorf("did %v, want the track whose lyrics matched queued and played", f.commands())
	}
}

// TestGlobalSearchLyricsAddToQueue covers 'a' on a lyrics hit, which
// has its own file/label lookup separate from the track kind's.
func TestGlobalSearchLyricsAddToQueue(t *testing.T) {
	indexPath, musicDir := buildLyricsIndex(t,
		[]lyricsindex.Track{{File: "rahman/guru/05.mp3", Artist: "Hariharan", Title: "Ay Hairathe"}},
		map[string]string{"rahman/guru/05.txt": "something about the moonlight"},
	)

	f := searchLibrary()
	a := newFakeApp(t, f)
	a.cfg.LyricsIndexPath = indexPath
	a.musicDir = musicDir

	field := openSearchField(t, a)
	field.SetText("l moonlight")

	sendKey(field, tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), a)
	sendKey(a.tv.GetFocus(), runeKey('a'), a)

	if !f.did("queueadd:rahman/guru/05.mp3") {
		t.Errorf("did %v, want the lyrics hit added without playing", f.commands())
	}
}

// TestGlobalSearchLyricsWithNoIndexNudgesTheUser covers the empty-index
// path: there is nothing to search until 'I' has built one, and saying
// so beats showing no results.
func TestGlobalSearchLyricsWithNoIndexNudgesTheUser(t *testing.T) {
	indexPath, _ := buildLyricsIndex(t, nil, nil)

	a := newFakeApp(t, searchLibrary())
	a.cfg.LyricsIndexPath = indexPath

	field := openSearchField(t, a)
	field.SetText("l anything")

	if got := a.hintBar.GetText(true); !strings.Contains(got, "lyrics index is empty") {
		t.Errorf("hint bar = %q, want it to nudge toward building the index", got)
	}
}

func TestGlobalSearchLyricsReportsAFailedLoad(t *testing.T) {
	a := newFakeApp(t, searchLibrary())
	a.cfg.LyricsIndexPath = filepath.Join(t.TempDir(), "nonexistent-dir", "lyrics.db")

	field := openSearchField(t, a)
	field.SetText("l anything")

	if got := a.hintBar.GetText(true); got == "" {
		t.Error("a failed index load reported nothing")
	}
}

// TestCatalogPickerKeepsTransportKeysLive covers the two selection
// lists in globalInputCapture's overlay table: like the lyrics viewer
// they are meant to be used while music plays, and 'q' still quits --
// but neither has a "same key closes it" shortcut, since only Esc and
// Enter make sense on a selection list.
func TestCatalogPickerKeepsTransportKeysLive(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(*App)
		key  rune
	}{
		{"marks", (*App).handleOpenMarkPicker, 'm'},
		{"tags", (*App).handleOpenTagPicker, 't'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMPD{}
			a := newFakeAppWithMetaDB(t, f)
			seedQueue(a, f, mpdclient.Song{ID: 1, Pos: 0, File: "a/b.mp3", Title: "Track"})
			a.tv.SetFocus(a.queue.table)
			a.queue.table.Select(queueHeaderRows, 0)
			tc.open(a)

			if a.mode != modeOverlay {
				t.Fatal("the picker did not open")
			}

			if got := a.globalInputCapture(runeKey(' ')); got != nil {
				t.Error("Space was not consumed while the picker is open")
			}
			if !f.did("toggle") {
				t.Errorf("did %v, want play/pause to work under the picker", f.commands())
			}
			if got := a.globalInputCapture(runeKey('q')); got != nil {
				t.Error("'q' was not consumed while the picker is open")
			}

			// The opening key is not a close shortcut here.
			if got := a.globalInputCapture(runeKey(tc.key)); got == nil && a.mode == modeNormal {
				t.Errorf("%q closed the picker, want only Esc/Enter to", tc.key)
			}
		})
	}
}

// TestDeleteKeyGuards covers handleDelete's three arms that do nothing:
// an empty Playlists selection, an empty Queue, and any other panel.
func TestDeleteKeyGuards(t *testing.T) {
	t.Run("no playlist selected", func(t *testing.T) {
		f := &fakeMPD{}
		a := newFakeApp(t, f)
		setPlaylistsForTest(a.playlists, nil)
		a.tv.SetFocus(a.playlists.table)

		a.handleDelete()

		if a.mode == modeOverlay {
			t.Error("a confirmation opened with no playlist selected")
		}
		if got := f.commands(); len(got) != 0 {
			t.Errorf("did %v, want nothing", got)
		}
	})

	t.Run("empty queue", func(t *testing.T) {
		f := &fakeMPD{}
		a := newFakeApp(t, f)
		a.tv.SetFocus(a.queue.table)

		a.handleDelete()

		if got := f.commands(); len(got) != 0 {
			t.Errorf("did %v with an empty Queue, want nothing", got)
		}
	})

	t.Run("wrong panel", func(t *testing.T) {
		a := newFakeApp(t, &fakeMPD{})
		a.tv.SetFocus(a.library.tree)

		a.handleDelete()

		if got := a.hintBar.GetText(true); !strings.Contains(got, "d") {
			t.Errorf("hint bar = %q, want an invalid-key flash", got)
		}
	})
}

// TestDeleteQueueTrackReportsAFailure covers the error arm of the
// non-confirmed delete.
func TestDeleteQueueTrackReportsAFailure(t *testing.T) {
	f := &fakeMPD{err: errTest}
	a := newFakeApp(t, f)
	seedQueue(a, f, mpdclient.Song{ID: 7, Pos: 0, Title: "Track", File: "a.mp3"})
	a.tv.SetFocus(a.queue.table)
	a.queue.table.Select(queueHeaderRows, 0)

	a.handleDelete()

	if got := a.hintBar.GetText(true); !strings.Contains(got, errTest.Error()) {
		t.Errorf("hint bar = %q, want the failure reported", got)
	}
}

// TestDeletePlaylistReportsAFailure covers the same for the confirmed
// path.
func TestDeletePlaylistReportsAFailure(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	setPlaylistsForTest(a.playlists, []string{"Road Trip"})
	a.tv.SetFocus(a.playlists.table)
	a.playlists.table.Select(1, 0)

	a.handleDelete()
	f.err = errTest
	answerConfirm(t, a, "Yes")

	if got := a.hintBar.GetText(true); !strings.Contains(got, errTest.Error()) {
		t.Errorf("hint bar = %q, want the failure reported", got)
	}
}

// TestSettingsTabWorksWithMetadataOff is a regression test. With the
// track-metadata feature off, the Database tab is a plain explanation,
// and tview delegates focus from the tab's Pages down to that notice.
// settingsview.Focused compared only against the Pages, so it reported
// "not focused", globalInputCapture stopped routing keys to the view,
// and Tab died -- leaving the user stuck on the Database tab until Esc.
func TestSettingsTabWorksWithMetadataOff(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{}) // no metaDB
	a.openSettings()

	tab := tabKeyEvent()

	if got := a.globalInputCapture(tab); got != nil {
		t.Fatal("Tab was not consumed on the Config tab")
	}
	if !a.settings.Focused() {
		t.Error("the settings view stopped recognising its own focus on the Database tab")
	}

	if got := a.globalInputCapture(tab); got != nil {
		t.Fatal("Tab was not consumed on the Database tab")
	}
	if a.tv.GetFocus() != a.settings.InitialFocus() {
		t.Errorf("focus after tabbing back = %T, want the Config tab", a.tv.GetFocus())
	}
}

// TestOverlayKeysFallThroughToTheFocusedWidget covers globalInputCapture's
// overlay default: anything it does not claim has to reach whatever is
// focused, so text overlays stay typeable.
func TestOverlayKeysFallThroughToTheFocusedWidget(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.openHelp()

	// Help is not in the no-text-input table and claims no runes of its
	// own, so an unrelated key passes through untouched.
	ev := runeKey('Z')
	if got := a.globalInputCapture(ev); got != ev {
		t.Errorf("%q was captured under the Help overlay, want it passed through", 'Z')
	}

	// A non-rune key that no overlay claims also passes through.
	down := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	if got := a.globalInputCapture(down); got != down {
		t.Error("Down was captured under the Help overlay, want it passed through")
	}
}

// TestTransportKeysAreNotLiveUnderTheTrackInfoCard covers the other
// side of the deliberate split: unlike the lyrics viewer, the card is
// something to read rather than watch while playing, so the transport
// cluster is not kept live there.
func TestTransportKeysAreNotLiveUnderTheTrackInfoCard(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	seedQueue(a, f, mpdclient.Song{ID: 1, Pos: 0, Title: "Track", File: "a/b.mp3"})
	a.tv.SetFocus(a.queue.table)
	a.queue.table.Select(queueHeaderRows, 0)
	a.openTrackInfo()

	a.globalInputCapture(runeKey('s')) // stop, if it were live

	if f.did("stop") {
		t.Errorf("did %v, want the transport cluster inactive under the Track Info card", f.commands())
	}
}
