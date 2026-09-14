package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// openAddPicker opens the add-to-playlist popup over a queued track and
// returns its filter field.
func openAddPicker(t *testing.T, a *App, f *fakeMPD, playlists ...string) *tview.InputField {
	t.Helper()
	seedQueue(a, f, mpdclient.Song{ID: 1, Pos: 0, Title: "Ay Hairathe", Artist: "Hariharan", File: "a/b.mp3"})
	setPlaylistsForTest(a.playlists, playlists)
	a.tv.SetFocus(a.queue.table)
	a.queue.table.Select(queueHeaderRows, 0)

	a.openAddToPlaylistPicker()

	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus after opening the picker is %T, want the filter field", a.tv.GetFocus())
	}
	return field
}

func TestAddToPlaylistConfirmAdds(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	field := openAddPicker(t, a, f, "Road Trip", "Chill")

	field.SetText("chill")
	sendKey(field, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if !f.did("pladd:Chill:a/b.mp3") {
		t.Errorf("did %v, want the track added to the matched playlist", f.commands())
	}
	if a.mode != modeNormal {
		t.Error("confirming left the popup open")
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "Chill") {
		t.Errorf("hint bar = %q, want it to name the playlist added to", got)
	}
}

// TestAddToPlaylistReportsADuplicate covers the one error this popup
// treats specially: MPD reporting the track is already there is normal
// enough to be a flash rather than an error.
func TestAddToPlaylistReportsADuplicate(t *testing.T) {
	f := &fakeMPD{err: mpdclient.ErrTrackAlreadyInPlaylist}
	a := newFakeApp(t, f)
	field := openAddPicker(t, a, f, "Road Trip")

	field.SetText("road")
	sendKey(field, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if got := a.hintBar.GetText(true); !strings.Contains(got, "already in playlist") {
		t.Errorf("hint bar = %q, want the duplicate reported as such", got)
	}
}

func TestAddToPlaylistReportsOtherFailures(t *testing.T) {
	f := &fakeMPD{err: errTest}
	a := newFakeApp(t, f)
	field := openAddPicker(t, a, f, "Road Trip")

	field.SetText("road")
	sendKey(field, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if got := a.hintBar.GetText(true); !strings.Contains(got, errTest.Error()) {
		t.Errorf("hint bar = %q, want the failure reported", got)
	}
}

// TestAddToPlaylistWithNoPlaylistsSaysWhatToDo covers the empty case,
// which points at the key that creates one rather than just showing an
// empty list.
func TestAddToPlaylistWithNoPlaylistsSaysWhatToDo(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	field := openAddPicker(t, a, f)

	sendKey(field, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if got := f.commands(); len(got) != 0 {
		t.Errorf("did %v with no playlists, want nothing", got)
	}
	if a.mode != modeOverlay {
		t.Error("confirming nothing closed the popup")
	}
}

func TestAddToPlaylistFilterAndNavigation(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	field := openAddPicker(t, a, f, "Road Trip", "Chill", "Bengali")

	field.SetText("") // everything
	down := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	up := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)

	sendKey(field, down, a)
	sendKey(field, up, a)
	sendKey(field, tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone), a)
	sendKey(field, tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone), a)

	// Tab into the list and use its own motions, then back.
	sendKey(field, tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), a)
	list, ok := a.tv.GetFocus().(*tview.List)
	if !ok {
		t.Fatalf("focus after Tab is %T, want the playlist list", a.tv.GetFocus())
	}
	for _, r := range []rune{'j', 'k', 'g', 'G'} {
		sendKey(list, runeKey(r), a)
	}
	sendKey(list, down, a)
	sendKey(list, up, a)
	sendKey(list, tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone), a)
	sendKey(list, tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone), a)

	sendKey(list, tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone), a)
	if a.tv.GetFocus() != field {
		t.Errorf("focus after Backtab is %T, want the filter field", a.tv.GetFocus())
	}

	// A filter matching nothing leaves nothing to confirm.
	field.SetText("zzzzz")
	sendKey(field, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)
	if got := f.commands(); len(got) != 0 {
		t.Errorf("did %v with no match, want nothing", got)
	}
}

// TestAddToPlaylistConfirmFromTheList covers Enter with the list
// focused rather than the field.
func TestAddToPlaylistConfirmFromTheList(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	field := openAddPicker(t, a, f, "Road Trip")

	sendKey(field, tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), a)
	sendKey(a.tv.GetFocus(), tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if !f.did("pladd:Road Trip:a/b.mp3") {
		t.Errorf("did %v, want the track added from the list", f.commands())
	}
}
