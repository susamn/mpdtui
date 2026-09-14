package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// The mark and tag pickers ('m' and 't') are the same widget over two
// different catalogs, so these drive one of each where the behavior
// differs and the mark picker alone where it does not.

func markPickerApp(t *testing.T) (*App, *fakeMPD, mpdclient.Song) {
	t.Helper()
	f := &fakeMPD{}
	a := newFakeAppWithMetaDB(t, f)
	song := mpdclient.Song{ID: 1, Pos: 0, File: "a/b.mp3", Title: "Track"}
	seedQueue(a, f, song)
	// Both pickers are gated on the Queue having focus, since the track
	// they act on is the selected row.
	a.tv.SetFocus(a.queue.table)
	a.queue.table.Select(queueHeaderRows, 0)
	return a, f, song
}

// TestCatalogPickerVimKeysWrapBothWays covers the j/k/g/G bindings the
// picker adds itself: tview.List, unlike Table and TreeView, has no
// native vim motions.
func TestCatalogPickerVimKeysWrapBothWays(t *testing.T) {
	a, _, song := markPickerApp(t)
	// Only one mark reason is seeded, which is not enough rows to see a
	// wrap; add a couple more.
	for _, r := range []string{"needs retag", "wrong album art"} {
		if _, err := a.metaDB.AddMarkReason(r); err != nil {
			t.Fatalf("AddMarkReason: %v", err)
		}
	}
	a.handleOpenMarkPicker()

	list := a.markPicker.List
	n := list.GetItemCount()
	if n < 3 {
		t.Fatalf("the mark picker shows %d items, want at least 3 (a clear entry plus the reasons)", n)
	}

	press := func(r rune) {
		list.InputHandler()(runeKey(r), func(tview.Primitive) {})
	}

	list.SetCurrentItem(0)
	press('j')
	if got := list.GetCurrentItem(); got != 1 {
		t.Errorf("after 'j' = %d, want 1", got)
	}

	list.SetCurrentItem(0)
	press('k')
	if got := list.GetCurrentItem(); got != n-1 {
		t.Errorf("'k' from the top = %d, want it wrapped to the last item %d", got, n-1)
	}

	press('g')
	if got := list.GetCurrentItem(); got != 0 {
		t.Errorf("'g' = %d, want the first item", got)
	}
	press('G')
	if got := list.GetCurrentItem(); got != n-1 {
		t.Errorf("'G' = %d, want the last item %d", got, n-1)
	}

	// Anything else falls through to the list's own handling.
	ev := runeKey('z')
	if got := a.markPicker.List.GetInputCapture()(ev); got != ev {
		t.Error("an unrelated key was captured, want it passed through")
	}

	_ = song
}

// TestMarkPickerTogglesAMark covers the whole round trip: choosing an
// entry writes it, and the Queue's cached row picks it up so the Mark
// column redraws without a separate refresh.
func TestMarkPickerTogglesAMark(t *testing.T) {
	a, _, song := markPickerApp(t)
	a.handleOpenMarkPicker()

	reasons, err := a.metaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) == 0 {
		t.Skip("no seeded mark reasons to choose")
	}

	// Index 0 is the synthetic "clear all" entry, so the first real
	// reason is at 1.
	a.markPicker.apply(1)

	track, err := a.metaDB.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(track.Marks) != 1 {
		t.Fatalf("track has %d marks after choosing one, want 1", len(track.Marks))
	}
	if got := a.queue.metaCache[song.File]; len(got.Marks) != 1 {
		t.Errorf("the Queue's cached row has %d marks, want 1", len(got.Marks))
	}

	// Choosing the same entry again toggles it back off.
	a.markPicker.apply(1)
	track, _ = a.metaDB.Get(song.File)
	if len(track.Marks) != 0 {
		t.Errorf("track has %d marks after toggling off, want 0", len(track.Marks))
	}
}

// TestCatalogPickerClearAll covers the synthetic leading entry, which
// exists because clearing several set members one by one is tedious.
// Driven through the tag picker, which seeds three entries, so two can
// be set without adding any.
func TestCatalogPickerClearAll(t *testing.T) {
	a, _, song := markPickerApp(t)
	a.handleOpenTagPicker()

	if n := a.tagPicker.GetItemCount(); n < 3 {
		t.Fatalf("the tag picker shows %d items, want the clear entry plus the seeded tags", n)
	}
	a.tagPicker.apply(1)
	a.tagPicker.apply(2)
	if track, _ := a.metaDB.Get(song.File); len(track.Tags) != 2 {
		t.Fatalf("setup: %d tags, want 2", len(track.Tags))
	}

	a.tagPicker.apply(0) // "(clear all ...)"

	if track, _ := a.metaDB.Get(song.File); len(track.Tags) != 0 {
		t.Errorf("tags remain after clearing all: %v", track.Tags)
	}
}

func TestTagPickerTogglesATag(t *testing.T) {
	a, _, song := markPickerApp(t)
	a.handleOpenTagPicker()

	tags, err := a.metaDB.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) == 0 {
		t.Skip("no seeded tags to choose")
	}

	a.tagPicker.apply(1)

	track, err := a.metaDB.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(track.Tags) != 1 {
		t.Errorf("track has %d tags after choosing one, want 1", len(track.Tags))
	}
}

// TestCatalogPickersNeedMetadata covers the gate on both: with the
// feature off there is no catalog to show.
func TestCatalogPickersNeedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(*App)
	}{
		{"marks", (*App).handleOpenMarkPicker},
		{"tags", (*App).handleOpenTagPicker},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMPD{}
			a := newFakeApp(t, f) // no metaDB
			seedQueue(a, f, mpdclient.Song{ID: 1, Pos: 0, File: "a/b.mp3"})
			a.tv.SetFocus(a.queue.table)
			a.queue.table.Select(queueHeaderRows, 0)

			tc.open(a)

			if a.mode == modeOverlay {
				t.Error("the picker opened without a metadata database")
			}
			if got := a.hintBar.GetText(true); !strings.Contains(got, "track metadata") {
				t.Errorf("hint bar = %q, want it to explain the feature is off", got)
			}
		})
	}
}

func TestTitleCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"mark", "Mark"},
		{"tag", "Tag"},
		{"", ""},
		{"a", "A"},
	}
	for _, tc := range cases {
		if got := titleCase(tc.in); got != tc.want {
			t.Errorf("titleCase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
