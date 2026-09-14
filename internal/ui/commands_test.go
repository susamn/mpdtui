package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
	"mpdtui/internal/theme"
)

// newFakeApp builds an App wired to an in-memory MPD stand-in, so the
// commands that talk to the server can be exercised without one -- and
// without the destructive ones acting on real playback.
func newFakeApp(t *testing.T, f *fakeMPD) *App {
	t.Helper()
	return newFakeAppWith(t, f)
}

// newFakeAppWith is newFakeApp for a stand-in other than *fakeMPD --
// used where a test needs to fail one specific command rather than all
// of them.
func newFakeAppWith(t *testing.T, f mpdConn) *App {
	t.Helper()
	a := &App{tv: tview.NewApplication(), client: f, playCountedSongID: -1}
	// Apply background results on the calling goroutine: nothing drains
	// a tview application's update queue unless Run() is going. Set
	// before build(), which otherwise wires the real QueueUpdateDraw.
	a.applyToUI = func(fn func()) { fn() }
	a.build()
	a.queue.table.SetRect(0, 0, 150, 40)
	a.runAsync = func(work func() error, onSuccess func()) {
		if err := work(); err != nil {
			a.showError(err)
			return
		}
		onSuccess()
	}
	return a
}

// errTest is a stand-in failure for the error arms.
var errTest = errors.New("mpd is unreachable")

func runeKey(r rune) *tcell.EventKey { return tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone) }

// --- Transport ----------------------------------------------------------

// TestTransportKeysIssueTheRightCommand covers every transport binding
// at once. Previously only Space was covered, against a live server and
// immediately toggled back, because proving 's' stops playback meant
// actually stopping it.
func TestTransportKeysIssueTheRightCommand(t *testing.T) {
	cases := []struct {
		key  rune
		want string
	}{
		{' ', "toggle"},
		{'s', "stop"},
		{'n', "next"},
		{'p', "previous"},
		{',', "seek-5s:rel=true"},
		{'.', "seek+5s:rel=true"},
		{'-', "volume-5"},
		// '=' is the same physical key as '+' without needing shift.
		{'=', "volume+5"},
	}
	for _, tc := range cases {
		t.Run(string(tc.key), func(t *testing.T) {
			f := &fakeMPD{}
			a := newFakeApp(t, f)

			if !a.handleTransportKey(tc.key) {
				t.Fatalf("handleTransportKey(%q) returned false, want it handled", tc.key)
			}
			if got := f.commands(); len(got) != 1 || got[0] != tc.want {
				t.Errorf("%q issued %v, want exactly [%s]", tc.key, got, tc.want)
			}
		})
	}
}

// TestOptionKeysToggleFromCurrentState covers z/x/c/Z: each reads the
// current status first and sends the opposite, so pressing the key
// twice must send true then false rather than true twice.
func TestOptionKeysToggleFromCurrentState(t *testing.T) {
	cases := []struct {
		key    rune
		option string
		set    func(*mpdclient.Status, bool)
	}{
		{'z', "random", func(s *mpdclient.Status, v bool) { s.Random = v }},
		{'x', "repeat", func(s *mpdclient.Status, v bool) { s.Repeat = v }},
		{'c', "consume", func(s *mpdclient.Status, v bool) { s.Consume = v }},
		{'Z', "single", func(s *mpdclient.Status, v bool) { s.Single = v }},
	}
	for _, tc := range cases {
		t.Run(tc.option, func(t *testing.T) {
			f := &fakeMPD{}
			a := newFakeApp(t, f)

			// Off -> on.
			tc.set(&f.status, false)
			a.handleTransportKey(tc.key)
			if want := tc.option + ":true"; !f.did(want) {
				t.Errorf("%q with %s off issued %v, want %s", tc.key, tc.option, f.commands(), want)
			}

			// On -> off.
			f.calls = nil
			tc.set(&f.status, true)
			a.handleTransportKey(tc.key)
			if want := tc.option + ":false"; !f.did(want) {
				t.Errorf("%q with %s on issued %v, want %s", tc.key, tc.option, f.commands(), want)
			}
		})
	}
}

func TestHandleTransportKeyIgnoresOtherRunes(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	for _, r := range []rune{'q', 'a', '1', 'Q', '/'} {
		if a.handleTransportKey(r) {
			t.Errorf("handleTransportKey(%q) claimed the key, want it left alone", r)
		}
	}
	if got := f.commands(); len(got) != 0 {
		t.Errorf("non-transport keys issued %v, want nothing", got)
	}
}

// TestTransportErrorsSurfaceToTheUser covers the error arm every one of
// these commands shares: a failure must reach the hint bar rather than
// being swallowed.
func TestTransportErrorsSurfaceToTheUser(t *testing.T) {
	for _, key := range []rune{' ', 's', 'n', 'p', ',', '-', 'z', 'x', 'c', 'Z'} {
		f := &fakeMPD{err: errors.New("mpd is gone")}
		a := newFakeApp(t, f)

		a.handleTransportKey(key)

		if got := a.hintBar.GetText(true); !strings.Contains(got, "mpd is gone") {
			t.Errorf("after %q with a failing server, hint bar = %q, want the error reported", key, got)
		}
	}
}

// --- Queue commands -----------------------------------------------------

func seedQueue(a *App, f *fakeMPD, songs ...mpdclient.Song) {
	f.queue = songs
	a.queue.songs = songs
	a.queue.render(-1)
}

func TestDeleteRemovesTheSelectedQueueTrack(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	seedQueue(a, f,
		mpdclient.Song{ID: 10, Pos: 0, Title: "One", File: "one.mp3"},
		mpdclient.Song{ID: 11, Pos: 1, Title: "Two", File: "two.mp3"},
	)
	a.tv.SetFocus(a.queue.table)
	a.queue.table.Select(1+queueHeaderRows, 0) // the second row

	a.handleDelete()

	if !f.did("remove:11") {
		t.Errorf("issued %v, want the selected track (id 11) removed", f.commands())
	}
}

// TestClearQueueAsksFirst covers the confirmation gate on a destructive
// command: nothing may be sent until the user actually answers Yes.
func TestClearQueueAsksFirst(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)

	a.handleClearQueue()

	if len(f.commands()) != 0 {
		t.Fatalf("the queue was cleared before confirming: %v", f.commands())
	}
	if a.mode != modeOverlay {
		t.Fatal("no confirmation overlay was opened")
	}

	answerConfirm(t, a, "Yes")

	if !f.did("queueclear") {
		t.Errorf("issued %v, want the queue cleared after confirming", f.commands())
	}
}

func TestClearQueueCancelled(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)

	a.handleClearQueue()
	answerConfirm(t, a, "No")

	if len(f.commands()) != 0 {
		t.Errorf("issued %v after declining, want nothing", f.commands())
	}
	if a.mode != modeNormal {
		t.Error("the confirmation overlay stayed open after declining")
	}
}

// answerConfirm answers whatever confirmation modal is currently open,
// by selecting the named button and pressing Enter on it -- the same
// path a keypress takes, rather than reaching for the callback.
func answerConfirm(t *testing.T, a *App, label string) {
	t.Helper()
	page := a.pages.GetPage("confirm")
	if page == nil {
		t.Fatal("no confirmation overlay is open")
	}
	modal, ok := page.(*tview.Modal)
	if !ok {
		t.Fatalf("the confirm page is %T, want a *tview.Modal", page)
	}

	switch label {
	case "Yes":
		modal.SetFocus(0)
	case "No":
		modal.SetFocus(1)
	default:
		t.Fatalf("unknown button %q", label)
	}

	// Focus the modal so its form routes the key to the selected
	// button, then press Enter on whatever that leaves focused.
	a.tv.SetFocus(modal)
	focused := a.tv.GetFocus()
	h := focused.InputHandler()
	if h == nil {
		t.Fatalf("focused primitive %T has no input handler", focused)
	}
	h(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(p tview.Primitive) { a.tv.SetFocus(p) })
}

func TestQueueMoveShiftsTheSelectedTrack(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	seedQueue(a, f,
		mpdclient.Song{ID: 10, Pos: 0, Title: "One"},
		mpdclient.Song{ID: 11, Pos: 1, Title: "Two"},
		mpdclient.Song{ID: 12, Pos: 2, Title: "Three"},
	)
	a.queue.table.Select(1+queueHeaderRows, 0) // "Two", at position 1

	a.handleQueueMove(1)
	if !f.did("move:11->2") {
		t.Errorf("issued %v, want track 11 moved to position 2", f.commands())
	}

	f.calls = nil
	a.queue.songs[1].Pos = 1
	a.queue.table.Select(1+queueHeaderRows, 0)
	a.handleQueueMove(-1)
	if !f.did("move:11->0") {
		t.Errorf("issued %v, want track 11 moved to position 0", f.commands())
	}
}

// TestQueueMoveStopsAtTheEnds covers the bounds check: moving the first
// track up or the last track down must send nothing at all rather than
// asking MPD for an out-of-range position.
func TestQueueMoveStopsAtTheEnds(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	seedQueue(a, f,
		mpdclient.Song{ID: 10, Pos: 0},
		mpdclient.Song{ID: 11, Pos: 1},
	)

	a.queue.table.Select(0+queueHeaderRows, 0)
	a.handleQueueMove(-1)

	a.queue.table.Select(1+queueHeaderRows, 0)
	a.handleQueueMove(1)

	if got := f.commands(); len(got) != 0 {
		t.Errorf("issued %v at the ends of the queue, want nothing", got)
	}
}

// --- Adding and playing -------------------------------------------------

func TestAddAndPlayQueuesThenPlays(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)

	a.addAndPlay(mpdclient.Song{File: "a-r-rahman/guru/05.mp3"})

	got := f.commands()
	if len(got) < 2 || got[0] != "queueaddid:a-r-rahman/guru/05.mp3" || got[1] != "play:1" {
		t.Errorf("issued %v, want the track queued and then played, in that order", got)
	}
}

func TestAddAndPlayReportsAFailedAdd(t *testing.T) {
	f := &fakeMPD{err: errors.New("add failed")}
	a := newFakeApp(t, f)

	a.addAndPlay(mpdclient.Song{File: "x.mp3"})

	if f.did("play:1") {
		t.Error("played a track the add had already failed for")
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "add failed") {
		t.Errorf("hint bar = %q, want the failure reported", got)
	}
}

func TestQueueAddPath(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)

	if !a.queueAddPath("artist/album") {
		t.Error("queueAddPath reported failure on a successful add")
	}
	if !f.did("queueadd:artist/album") {
		t.Errorf("issued %v, want the path added", f.commands())
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "album") {
		t.Errorf("hint bar = %q, want it to name what was added", got)
	}

	f.err = errors.New("nope")
	if a.queueAddPath("other") {
		t.Error("queueAddPath reported success despite an error")
	}
}

func TestLoadAndAppendPlaylist(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)

	a.loadPlaylist("Road Trip")
	if !f.did("plload:Road Trip") {
		t.Errorf("issued %v, want the playlist loaded", f.commands())
	}

	f.calls = nil
	if !a.appendPlaylist("Road Trip") {
		t.Error("appendPlaylist reported failure on success")
	}
	if !f.did("plappend:Road Trip") {
		t.Errorf("issued %v, want the playlist appended", f.commands())
	}

	f.err = errors.New("gone")
	if a.appendPlaylist("Road Trip") {
		t.Error("appendPlaylist reported success despite an error")
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "gone") {
		t.Errorf("hint bar = %q, want the failure reported", got)
	}
}

// TestDeletePlaylistAsksFirst is the other destructive command behind a
// confirmation.
func TestDeletePlaylistAsksFirst(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	setPlaylistsForTest(a.playlists, []string{"Road Trip"})
	a.tv.SetFocus(a.playlists.table)
	a.playlists.table.Select(1, 0)

	a.handleDelete()
	if len(f.commands()) != 0 {
		t.Fatalf("deleted before confirming: %v", f.commands())
	}

	answerConfirm(t, a, "Yes")
	if !f.did("pldelete:Road Trip") {
		t.Errorf("issued %v, want the playlist deleted after confirming", f.commands())
	}
}

// --- Error reporting ----------------------------------------------------

func TestShowErrorPutsTheMessageInTheHintBar(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})

	a.showError(errors.New("something broke"))

	got := a.hintBar.GetText(true)
	if !strings.Contains(got, "something broke") {
		t.Errorf("hint bar = %q, want the error text", got)
	}
}

// waitFor polls cond for up to a second, for the handful of paths that
// genuinely hand work to a goroutine.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	waitForUpTo(t, time.Second, cond)
}

// waitForUpTo is waitFor with an explicit budget, for waits that are
// bounded by a real timer rather than by scheduling.
func waitForUpTo(t *testing.T, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was never met")
}

// paletteWithAccent builds a complete-enough palette for repaint tests.
func paletteWithAccent(accent string) theme.Palette {
	p := theme.Default()
	p.Accent = theme.Color(accent)
	return p
}

// newFakeAppWithMetaDB is newFakeApp plus a real (temporary)
// track-metadata database, for the features gated on one.
func newFakeAppWithMetaDB(t *testing.T, f *fakeMPD) *App {
	t.Helper()
	db, err := metadata.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	a := &App{tv: tview.NewApplication(), client: f, metaDB: db, playCountedSongID: -1}
	a.applyToUI = func(fn func()) { fn() }
	a.build()
	a.queue.table.SetRect(0, 0, 150, 40)
	a.runAsync = func(work func() error, onSuccess func()) {
		if err := work(); err != nil {
			a.showError(err)
			return
		}
		onSuccess()
	}
	return a
}

// enterKey is the Enter keypress, used wherever a test confirms.
func enterKey() *tcell.EventKey { return tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone) }

// openInputFieldOf returns whatever text input the currently open
// overlay focused.
func openInputFieldOf(t *testing.T, a *App) *tview.InputField {
	t.Helper()
	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus is %T, want a text input overlay", a.tv.GetFocus())
	}
	return field
}

// trackInfoPlaylists is the Track Info card's Playlists section.
func trackInfoPlaylists(a *App) string {
	return a.trackInfo.playlists.GetText(true)
}
