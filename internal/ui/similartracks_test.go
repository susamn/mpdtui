package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
	"mpdtui/internal/uitheme"
)

func TestGroupTrackKeyWhenNotEnabled(t *testing.T) {
	a := newTestApp()
	uKey := tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone)
	if res := a.globalInputCapture(uKey); res != nil {
		t.Errorf("'u' should be consumed, got %v", res)
	}
	if !strings.Contains(a.hintBar.GetText(false), "no track is currently playing") {
		t.Errorf("hint bar = %q, want 'no track is currently playing'", a.hintBar.GetText(false))
	}

	capUKey := tcell.NewEventKey(tcell.KeyRune, 'U', tcell.ModNone)
	if res := a.globalInputCapture(capUKey); res != nil {
		t.Errorf("'U' should be consumed, got %v", res)
	}
	if !strings.Contains(a.hintBar.GetText(false), "no track is currently playing") {
		t.Errorf("hint bar = %q, want 'no track is currently playing'", a.hintBar.GetText(false))
	}
}

func TestGroupTrackKeyWhenNothingPlaying(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.currentSong = mpdclient.Song{}

	uKey := tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone)
	if res := a.globalInputCapture(uKey); res != nil {
		t.Errorf("'u' should be consumed, got %v", res)
	}
	if !strings.Contains(a.hintBar.GetText(false), "no track is currently playing") {
		t.Errorf("hint bar = %q, want 'no track is currently playing'", a.hintBar.GetText(false))
	}

	capUKey := tcell.NewEventKey(tcell.KeyRune, 'U', tcell.ModNone)
	if res := a.globalInputCapture(capUKey); res != nil {
		t.Errorf("'U' should be consumed, got %v", res)
	}
	if !strings.Contains(a.hintBar.GetText(false), "no track is currently playing") {
		t.Errorf("hint bar = %q, want 'no track is currently playing'", a.hintBar.GetText(false))
	}
}

func TestTrackGroupHintsFuzzyFiltering(t *testing.T) {
	songs := []mpdclient.Song{
		{File: "a.mp3", Artist: "The Beatles", Title: "Yesterday"},
		{File: "b.mp3", Artist: "Boyz II Men", Title: "Yesterday"},
		{File: "c.mp3", Artist: "Queen", Title: "Bohemian Rhapsody"},
	}
	labels := []string{
		"The Beatles - Yesterday",
		"Boyz II Men - Yesterday",
		"Queen - Bohemian Rhapsody",
	}

	hints := &trackGroupHints{}
	hints.rebuild("yester", songs, labels)

	if hints.total != 2 {
		t.Fatalf("total = %d, want 2", hints.total)
	}
	if hints.highlight != 0 {
		t.Fatalf("highlight = %d, want 0", hints.highlight)
	}

	song, idx := hints.current()
	if idx < 0 || song.Title != "Yesterday" {
		t.Fatalf("current() = %v, %d", song, idx)
	}

	hints.move(1)
	if hints.highlight != 1 {
		t.Fatalf("after move(1), highlight = %d, want 1", hints.highlight)
	}

	hints.move(1)
	if hints.highlight != 0 {
		t.Fatalf("after wrapping move(1), highlight = %d, want 0", hints.highlight)
	}
}

func TestGroupTrackCurrentSongNotListed(t *testing.T) {
	fake := &fakeMPD{
		songs: []mpdclient.Song{
			{File: "queen/yesterday.mp3", Artist: "Queen", Title: "Yesterday"},
			{File: "beatles/yesterday.mp3", Artist: "Beatles", Title: "Yesterday"},
		},
	}
	a := newFakeAppWithMetaDB(t, fake)
	a.currentSong = fake.songs[0]

	uKey := tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone)
	a.globalInputCapture(uKey)

	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus = %T, want *tview.InputField", a.tv.GetFocus())
	}

	// Press Tab to focus the list
	tabKey := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	field.InputHandler()(tabKey, nil)

	itemsList, ok := a.tv.GetFocus().(*tview.List)
	if !ok {
		t.Fatalf("after Tab, focus = %T, want *tview.List", a.tv.GetFocus())
	}

	if itemsList.GetItemCount() != 1 {
		t.Fatalf("item count = %d, want 1 (playing song must not be listed)", itemsList.GetItemCount())
	}
	mainText, _ := itemsList.GetItemText(0)
	if strings.Contains(mainText, "Queen") {
		t.Fatalf("listed item %q contains playing artist Queen", mainText)
	}
}

func TestGroupTrackListingShowsGreenTickForAlreadyGrouped(t *testing.T) {
	fake := &fakeMPD{
		songs: []mpdclient.Song{
			{File: "queen/yesterday.mp3", Artist: "Queen", Title: "Yesterday"},
			{File: "beatles/yesterday.mp3", Artist: "Beatles", Title: "Yesterday"},
			{File: "acoustic/yesterday.mp3", Artist: "Acoustic", Title: "Yesterday"},
		},
	}
	a := newFakeAppWithMetaDB(t, fake)
	a.currentSong = fake.songs[0]

	// Group Queen and Beatles
	if err := a.metaDB.GroupTracks("queen/yesterday.mp3", "beatles/yesterday.mp3"); err != nil {
		t.Fatalf("GroupTracks: %v", err)
	}

	uKey := tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone)
	a.globalInputCapture(uKey)

	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus = %T, want *tview.InputField", a.tv.GetFocus())
	}

	tabKey := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	field.InputHandler()(tabKey, nil)

	itemsList, ok := a.tv.GetFocus().(*tview.List)
	if !ok {
		t.Fatalf("after Tab, focus = %T, want *tview.List", a.tv.GetFocus())
	}

	if itemsList.GetItemCount() != 2 {
		t.Fatalf("item count = %d, want 2", itemsList.GetItemCount())
	}

	text0, _ := itemsList.GetItemText(0)
	text1, _ := itemsList.GetItemText(1)

	var beatlesText, acousticText string
	if strings.Contains(text0, "Beatles") {
		beatlesText, acousticText = text0, text1
	} else {
		beatlesText, acousticText = text1, text0
	}

	// Beatles is in the group -> should have green tick
	if !strings.Contains(beatlesText, "[green]✓[-]") {
		t.Fatalf("grouped track text = %q, want green tick [green]✓[-]", beatlesText)
	}
	// Acoustic is not in the group -> should NOT have tick
	if strings.Contains(acousticText, "✓") {
		t.Fatalf("ungrouped track text = %q, want no tick", acousticText)
	}
	// Both must have uniform leading padding
	if !strings.HasPrefix(beatlesText, "  ") || !strings.HasPrefix(acousticText, "     ") {
		t.Fatalf("uniform padding check failed: beatles=%q, acoustic=%q", beatlesText, acousticText)
	}
}

func TestGroupTrackBordersAndTabNavigation(t *testing.T) {
	fake := &fakeMPD{
		songs: []mpdclient.Song{
			{File: "queen/yesterday.mp3", Artist: "Queen", Title: "Yesterday"},
			{File: "beatles/yesterday.mp3", Artist: "Beatles", Title: "Yesterday"},
			{File: "acoustic/yesterday.mp3", Artist: "Acoustic", Title: "Yesterday"},
		},
	}
	a := newFakeAppWithMetaDB(t, fake)
	a.currentSong = fake.songs[0]

	uKey := tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone)
	a.globalInputCapture(uKey)

	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus = %T, want *tview.InputField", a.tv.GetFocus())
	}

	// Initially, field is focused with active border
	if field.GetBorderColor() != uitheme.ActiveBorder() {
		t.Errorf("field border color = %v, want ActiveBorder %v", field.GetBorderColor(), uitheme.ActiveBorder())
	}

	// Press Tab in field: should focus list and flip borders
	tabKey := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	field.InputHandler()(tabKey, nil)

	list, ok := a.tv.GetFocus().(*tview.List)
	if !ok {
		t.Fatalf("after Tab, focus = %T, want *tview.List", a.tv.GetFocus())
	}
	if list.GetBorderColor() != uitheme.ActiveBorder() {
		t.Errorf("after Tab, list border color = %v, want ActiveBorder", list.GetBorderColor())
	}
	if field.GetBorderColor() != uitheme.InactiveBorder() {
		t.Errorf("after Tab, field border color = %v, want InactiveBorder", field.GetBorderColor())
	}

	// In list, press 'j' to navigate down
	if list.GetCurrentItem() != 0 {
		t.Fatalf("initial list current item = %d, want 0", list.GetCurrentItem())
	}
	jKey := tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone)
	list.InputHandler()(jKey, nil)
	if list.GetCurrentItem() != 1 {
		t.Fatalf("after 'j', list current item = %d, want 1", list.GetCurrentItem())
	}

	// In list, press 'k' to navigate up
	kKey := tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone)
	list.InputHandler()(kKey, nil)
	if list.GetCurrentItem() != 0 {
		t.Fatalf("after 'k', list current item = %d, want 0", list.GetCurrentItem())
	}

	// Press Tab in list: should focus field again
	list.InputHandler()(tabKey, nil)
	if a.tv.GetFocus() != field {
		t.Fatalf("after Tab in list, focus = %T, want *tview.InputField", a.tv.GetFocus())
	}
	if field.GetBorderColor() != uitheme.ActiveBorder() {
		t.Errorf("after Tab back to field, field border color = %v, want ActiveBorder", field.GetBorderColor())
	}
}

func TestGroupTrackOverlayAndExecution(t *testing.T) {
	fake := &fakeMPD{
		songs: []mpdclient.Song{
			{File: "queen/yesterday.mp3", Artist: "Queen", Title: "Yesterday"},
			{File: "beatles/yesterday.mp3", Artist: "Beatles", Title: "Yesterday"},
			{File: "cover/yesterday.mp3", Artist: "Acoustic", Title: "Yesterday"},
		},
	}
	a := newFakeAppWithMetaDB(t, fake)

	a.currentSong = fake.songs[0]

	uKey := tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone)
	if res := a.globalInputCapture(uKey); res != nil {
		t.Fatalf("'u' should be consumed, got %v", res)
	}

	if a.mode != modeOverlay {
		t.Fatalf("mode = %d, want modeOverlay", a.mode)
	}
	if !a.pages.HasPage("group-track") {
		t.Fatal("page 'group-track' not found")
	}

	// Focus is on the input field
	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focused widget = %T, want *tview.InputField", a.tv.GetFocus())
	}

	// Type "beatles"
	field.SetText("beatles")

	// Trigger Enter on the field
	enterKey := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	field.InputHandler()(enterKey, nil)

	// Overlay should close
	if a.mode != modeNormal {
		t.Fatalf("after grouping, mode = %d, want modeNormal", a.mode)
	}

	// Verify in DB that Queen and Beatles are grouped
	similar, err := a.metaDB.GetSimilarTracks("queen/yesterday.mp3")
	if err != nil {
		t.Fatalf("GetSimilarTracks: %v", err)
	}
	if len(similar) != 1 || similar[0].RealPath != "beatles/yesterday.mp3" {
		t.Fatalf("GetSimilarTracks = %+v, want beatles/yesterday.mp3", similar)
	}
}

func TestPlaySimilarTracksOverlayAndPlay(t *testing.T) {
	fake := &fakeMPD{
		songs: []mpdclient.Song{
			{File: "queen/yesterday.mp3", Artist: "Queen", Title: "Yesterday"},
			{File: "beatles/yesterday.mp3", Artist: "Beatles", Title: "Yesterday"},
		},
	}
	a := newFakeAppWithMetaDB(t, fake)

	// Group them in DB
	if err := a.metaDB.GroupTracks("queen/yesterday.mp3", "beatles/yesterday.mp3"); err != nil {
		t.Fatalf("GroupTracks: %v", err)
	}

	a.currentSong = fake.songs[0]

	// Press 'U' to view similar tracks
	capUKey := tcell.NewEventKey(tcell.KeyRune, 'U', tcell.ModNone)
	if res := a.globalInputCapture(capUKey); res != nil {
		t.Fatalf("'U' should be consumed, got %v", res)
	}

	if a.mode != modeOverlay {
		t.Fatalf("mode = %d, want modeOverlay", a.mode)
	}
	if !a.pages.HasPage("similar-tracks") {
		t.Fatal("page 'similar-tracks' not found")
	}

	list, ok := a.tv.GetFocus().(*tview.List)
	if !ok {
		t.Fatalf("focused widget = %T, want *tview.List", a.tv.GetFocus())
	}
	if list.GetBorderColor() != uitheme.ActiveBorder() {
		t.Errorf("similar tracks list border = %v, want ActiveBorder %v", list.GetBorderColor(), uitheme.ActiveBorder())
	}
	if list.GetItemCount() != 1 {
		t.Fatalf("item count = %d, want 1", list.GetItemCount())
	}

	// Press Enter to play
	enterKey := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	list.InputHandler()(enterKey, nil)

	if a.mode != modeNormal {
		t.Fatalf("after selection, mode = %d, want modeNormal", a.mode)
	}

	// Verify fakeMPD has record of queueadd and play
	history := fake.commands()
	foundAdd := false
	foundPlay := false
	for _, h := range history {
		if strings.Contains(h, "queueaddid:beatles/yesterday.mp3") {
			foundAdd = true
		}
		if strings.Contains(h, "play:") {
			foundPlay = true
		}
	}
	if !foundAdd || !foundPlay {
		t.Fatalf("commands = %v, want queueaddid and play for beatles/yesterday.mp3", history)
	}
}

func TestSimilarTracksOverlayUnlink(t *testing.T) {
	fake := &fakeMPD{
		songs: []mpdclient.Song{
			{File: "queen/yesterday.mp3", Artist: "Queen", Title: "Yesterday"},
			{File: "beatles/yesterday.mp3", Artist: "Beatles", Title: "Yesterday"},
			{File: "acoustic/yesterday.mp3", Artist: "Acoustic", Title: "Yesterday"},
		},
	}
	a := newFakeAppWithMetaDB(t, fake)

	if err := a.metaDB.GroupTracks("queen/yesterday.mp3", "beatles/yesterday.mp3"); err != nil {
		t.Fatalf("GroupTracks: %v", err)
	}
	if err := a.metaDB.GroupTracks("queen/yesterday.mp3", "acoustic/yesterday.mp3"); err != nil {
		t.Fatalf("GroupTracks: %v", err)
	}

	a.currentSong = fake.songs[0]

	// Press 'U'
	capUKey := tcell.NewEventKey(tcell.KeyRune, 'U', tcell.ModNone)
	a.globalInputCapture(capUKey)

	list, ok := a.tv.GetFocus().(*tview.List)
	if !ok {
		t.Fatalf("focused widget = %T, want *tview.List", a.tv.GetFocus())
	}
	if list.GetItemCount() != 2 {
		t.Fatalf("item count = %d, want 2", list.GetItemCount())
	}

	// Test 'j' and 'k' navigation
	jKey := tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone)
	list.InputHandler()(jKey, nil)
	if list.GetCurrentItem() != 1 {
		t.Fatalf("after 'j', current item = %d, want 1", list.GetCurrentItem())
	}

	kKey := tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone)
	list.InputHandler()(kKey, nil)
	if list.GetCurrentItem() != 0 {
		t.Fatalf("after 'k', current item = %d, want 0", list.GetCurrentItem())
	}

	// Press 'd' to unlink the first item
	dKey := tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone)
	list.InputHandler()(dKey, nil)

	// List should now have 1 item
	if list.GetItemCount() != 1 {
		t.Fatalf("after unlinking one track, item count = %d, want 1", list.GetItemCount())
	}

	// Press 'd' to unlink the remaining item -> group dissolves, overlay closes
	list.InputHandler()(dKey, nil)

	if a.mode != modeNormal {
		t.Fatalf("after unlinking all tracks, mode = %d, want modeNormal", a.mode)
	}
}
