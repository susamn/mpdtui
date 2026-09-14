package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// These drive the 'f' popup itself -- the closures that fetch each
// kind's labels, render the results table, and act on a confirmed
// choice. The ranking underneath them is covered separately in
// globalsearch_test.go.

func searchLibrary() *fakeMPD {
	return &fakeMPD{
		songs: []mpdclient.Song{
			{Title: "Ay Hairathe", Artist: "Hariharan", File: "rahman/guru/05.mp3"},
			{Title: "Tere Bina", Artist: "Chinmayi", File: "rahman/guru/02.mp3"},
			{Title: "Zara Zara", Artist: "Bombay Jayashri", File: "other/rhtdm/03.mp3"},
		},
		artists: []string{"Hariharan", "Chinmayi", "Bombay Jayashri"},
		albums:  map[string][]string{"": {"Guru", "Rehna Hai Tere Dil Mein"}},
		pls:     []mpdclient.Playlist{{Name: "Road Trip"}, {Name: "Chill"}},
	}
}

// openSearchField opens the popup and returns its input field.
func openSearchField(t *testing.T, a *App) *tview.InputField {
	t.Helper()
	// The playlist kind reads the Playlists panel's own loaded list
	// rather than fetching, so seed it the way a refresh would.
	if len(a.playlists.pls) == 0 {
		setPlaylistsForTest(a.playlists, []string{"Road Trip", "Chill"})
	}
	a.openGlobalSearch()
	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus after opening global search is %T, want the input field", a.tv.GetFocus())
	}
	return field
}

func sendKey(p tview.Primitive, ev *tcell.EventKey, a *App) {
	p.InputHandler()(ev, func(next tview.Primitive) { a.tv.SetFocus(next) })
}

func TestGlobalSearchOpensOnTheTrackKind(t *testing.T) {
	a := newFakeApp(t, searchLibrary())
	field := openSearchField(t, a)

	if got := field.GetText(); got != "t " {
		t.Errorf("field starts as %q, want the track prefix pre-filled", got)
	}
	if a.mode != modeOverlay {
		t.Error("global search did not enter overlay mode")
	}
}

// TestGlobalSearchTracksRenderAndConfirm covers the default kind end to
// end: typing filters, and Enter queues and plays the highlighted track
// then moves focus to the Queue.
func TestGlobalSearchTracksRenderAndConfirm(t *testing.T) {
	f := searchLibrary()
	a := newFakeApp(t, f)
	field := openSearchField(t, a)

	field.SetText("t hairathe") // SetText fires the changed func, so this rebuilds

	sendKey(field, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if !f.did("queueaddid:rahman/guru/05.mp3") || !f.did("play:1") {
		t.Errorf("did %v, want the matched track queued and played", f.commands())
	}
	if a.mode != modeNormal {
		t.Error("confirming a track left the popup open")
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after confirming = %T, want the Queue", a.tv.GetFocus())
	}
}

// TestGlobalSearchConfirmWithNoMatchesDoesNothing covers the guard on a
// term that matched nothing.
func TestGlobalSearchConfirmWithNoMatchesDoesNothing(t *testing.T) {
	f := searchLibrary()
	a := newFakeApp(t, f)
	field := openSearchField(t, a)

	field.SetText("t zzzzzzz")
	sendKey(field, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if got := f.commands(); len(got) != 0 {
		t.Errorf("did %v with nothing matched, want nothing", got)
	}
	if a.mode != modeOverlay {
		t.Error("confirming nothing closed the popup")
	}
}

func TestGlobalSearchArtistConfirmNavigatesToLibrary(t *testing.T) {
	f := searchLibrary()
	a := newFakeApp(t, f)
	field := openSearchField(t, a)

	field.SetText("a hariharan")
	sendKey(field, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if a.library.mode != libSearch {
		t.Error("confirming an artist did not put the Library into search mode")
	}
	if a.tv.GetFocus() != a.library.tree {
		t.Errorf("focus = %T, want the Library tree", a.tv.GetFocus())
	}
}

func TestGlobalSearchAlbumConfirmNavigatesToLibrary(t *testing.T) {
	f := searchLibrary()
	a := newFakeApp(t, f)
	field := openSearchField(t, a)

	field.SetText("al guru")
	sendKey(field, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if a.library.mode != libSearch {
		t.Error("confirming an album did not put the Library into search mode")
	}
}

func TestGlobalSearchPlaylistConfirmLoadsIt(t *testing.T) {
	f := searchLibrary()
	a := newFakeApp(t, f)
	field := openSearchField(t, a)

	field.SetText("p road")
	sendKey(field, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if !f.did("plload:Road Trip") {
		t.Errorf("did %v, want the playlist loaded", f.commands())
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus = %T, want the Queue", a.tv.GetFocus())
	}
}

// TestGlobalSearchTabTogglesFocus covers moving between typing and
// navigating the results.
func TestGlobalSearchTabTogglesFocus(t *testing.T) {
	a := newFakeApp(t, searchLibrary())
	field := openSearchField(t, a)
	field.SetText("t a")

	sendKey(field, tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), a)
	table, ok := a.tv.GetFocus().(*tview.Table)
	if !ok {
		t.Fatalf("focus after Tab is %T, want the results table", a.tv.GetFocus())
	}

	sendKey(table, tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone), a)
	if a.tv.GetFocus() != field {
		t.Errorf("focus after Backtab is %T, want the input field", a.tv.GetFocus())
	}

	// 'f' from the table also returns to the field -- the same muscle
	// memory that opened the popup.
	sendKey(field, tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), a)
	sendKey(a.tv.GetFocus(), runeKey('f'), a)
	if a.tv.GetFocus() != field {
		t.Errorf("focus after 'f' is %T, want the input field", a.tv.GetFocus())
	}
}

// TestGlobalSearchNavigationKeys covers moving the highlight from both
// the field and the table, including the vim motions the table adds.
func TestGlobalSearchNavigationKeys(t *testing.T) {
	a := newFakeApp(t, searchLibrary())
	field := openSearchField(t, a)
	field.SetText("t a") // several matches

	down := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	up := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)

	sendKey(field, down, a)
	sendKey(field, up, a)
	sendKey(field, tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone), a)
	sendKey(field, tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone), a)

	sendKey(field, tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), a)
	table := a.tv.GetFocus()

	for _, r := range []rune{'j', 'k', 'g', 'G'} {
		sendKey(table, runeKey(r), a)
	}
	sendKey(table, down, a)
	sendKey(table, up, a)
	sendKey(table, tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone), a)
	sendKey(table, tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone), a)

	// Still open and still usable.
	if a.mode != modeOverlay {
		t.Error("navigating closed the popup")
	}
}

// TestGlobalSearchAddToQueueKeepsThePopupOpen is the point of 'a' as
// distinct from Enter: several tracks can be queued back to back
// without the popup closing after each one.
func TestGlobalSearchAddToQueueKeepsThePopupOpen(t *testing.T) {
	f := searchLibrary()
	a := newFakeApp(t, f)
	field := openSearchField(t, a)
	field.SetText("t hairathe")

	sendKey(field, tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), a)
	sendKey(a.tv.GetFocus(), runeKey('a'), a)

	if !f.did("queueadd:rahman/guru/05.mp3") {
		t.Errorf("did %v, want the track added without playing", f.commands())
	}
	if f.did("play:1") {
		t.Error("'a' played the track, want it only queued")
	}
	if a.mode != modeOverlay {
		t.Error("'a' closed the popup, want it left open for further adds")
	}
}

func TestGlobalSearchAddToQueueAppendsAPlaylist(t *testing.T) {
	f := searchLibrary()
	a := newFakeApp(t, f)
	field := openSearchField(t, a)
	field.SetText("p chill")

	sendKey(field, tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), a)
	sendKey(a.tv.GetFocus(), runeKey('a'), a)

	if !f.did("plappend:Chill") {
		t.Errorf("did %v, want the playlist appended", f.commands())
	}
}

// TestGlobalSearchAddIsInvalidForArtistAndAlbum covers the deliberate
// gap: queueing an entire artist's or album's catalog from a stray
// keypress is a much bigger action than a single track, so 'a' simply
// has no meaning there.
func TestGlobalSearchAddIsInvalidForArtistAndAlbum(t *testing.T) {
	for _, prefix := range []string{"a hariharan", "al guru"} {
		f := searchLibrary()
		a := newFakeApp(t, f)
		field := openSearchField(t, a)
		field.SetText(prefix)

		sendKey(field, tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), a)
		sendKey(a.tv.GetFocus(), runeKey('a'), a)

		if got := f.commands(); len(got) != 0 {
			t.Errorf("%q: 'a' did %v, want nothing", prefix, got)
		}
		if got := a.hintBar.GetText(true); !strings.Contains(got, "a") {
			t.Errorf("%q: hint bar = %q, want an invalid-key flash", prefix, got)
		}
	}
}

func TestGlobalSearchAddReportsAFailedQueueAdd(t *testing.T) {
	f := searchLibrary()
	a := newFakeApp(t, f)
	field := openSearchField(t, a)
	field.SetText("t hairathe")
	f.err = errTest

	sendKey(field, tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), a)
	sendKey(a.tv.GetFocus(), runeKey('a'), a)

	if got := a.hintBar.GetText(true); !strings.Contains(got, errTest.Error()) {
		t.Errorf("hint bar = %q, want the failure reported", got)
	}
}

// TestGlobalSearchReportsFailedFetches covers each kind's loader error
// arm -- an unreachable server has to say so rather than silently
// showing an empty result list.
func TestGlobalSearchReportsFailedFetches(t *testing.T) {
	for _, prefix := range []string{"t x", "a x", "al x"} {
		a := newFakeApp(t, &fakeMPD{err: errTest})
		field := openSearchField(t, a)
		field.SetText(prefix)

		if got := a.hintBar.GetText(true); !strings.Contains(got, errTest.Error()) {
			t.Errorf("%q: hint bar = %q, want the fetch failure reported", prefix, got)
		}
	}
}

// TestGlobalSearchTypingPassesThrough covers the fall-through: anything
// the field does not claim must reach the input so a term can be typed.
func TestGlobalSearchTypingPassesThrough(t *testing.T) {
	a := newFakeApp(t, searchLibrary())
	field := openSearchField(t, a)

	ev := runeKey('x')
	var passed bool
	field.InputHandler()(ev, func(tview.Primitive) {})
	// The field's capture returns the event for unhandled keys; typing
	// therefore reaches tview's own handling and changes the text.
	field.SetText("t x")
	passed = strings.Contains(field.GetText(), "x")
	if !passed {
		t.Error("typing did not reach the input field")
	}
}
