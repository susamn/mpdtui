package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/mpdclient"
)

// firstOf drops playlistsSection's row count, for the tests that only
// care about the rendered text.
func firstOf(text string, _ int) string { return text }

func testSong() mpdclient.Song {
	return mpdclient.Song{Title: "Kind of Blue", Album: "Milestones", Artist: "Miles Davis",
		Genre: "Jazz", Date: "1959", File: "a/1.mp3", Duration: 400}
}

// TestPlaylistsSectionDistinguishesUnknownFromEmpty is the point of the
// section: before the background scan lands there is no answer, and
// saying "none" then would be wrong in a way the user cannot tell apart
// from the truth.
func TestPlaylistsSectionDistinguishesUnknownFromEmpty(t *testing.T) {
	notFetched := firstOf(playlistsSection(nil, "a/1.mp3", trackInfoPlaylistLines))
	if !strings.Contains(notFetched, "loading") {
		t.Errorf("with no scan yet = %q, want it to say it is still loading", notFetched)
	}
	if strings.Contains(notFetched, "none") {
		t.Errorf("with no scan yet = %q, must not claim the track is in no playlist", notFetched)
	}

	fetched := firstOf(playlistsSection(map[string][]string{"other.mp3": {"X"}}, "a/1.mp3", trackInfoPlaylistLines))
	if !strings.Contains(fetched, "none") {
		t.Errorf("after a scan with no match = %q, want %q", fetched, "none")
	}
	if strings.Contains(fetched, "loading") {
		t.Errorf("after a scan = %q, must not still say loading", fetched)
	}
}

func TestPlaylistsSectionListsEveryName(t *testing.T) {
	got := firstOf(playlistsSection(map[string][]string{
		"a/1.mp3": {"Jazz Classics", "Late Night", "Thumri"},
	}, "a/1.mp3", trackInfoPlaylistLines))

	for _, want := range []string{"Jazz Classics", "Late Night", "Thumri"} {
		if !strings.Contains(got, want) {
			t.Errorf("section %q is missing %q", got, want)
		}
	}
	if strings.Contains(got, "more") {
		t.Errorf("section %q summarised an overflow it does not have", got)
	}
}

func TestPlaylistsSectionCapsTheListAndCountsTheRest(t *testing.T) {
	// A track in thirty playlists must not push the card past the Queue
	// panel it floats inside.
	var names []string
	for i := 0; i < trackInfoPlaylistLines+6; i++ {
		names = append(names, fmt.Sprintf("Playlist %02d", i))
	}
	got := firstOf(playlistsSection(map[string][]string{"a/1.mp3": names}, "a/1.mp3", trackInfoPlaylistLines))

	if lines := strings.Count(got, "•"); lines != trackInfoPlaylistLines {
		t.Errorf("listed %d names, want the cap of %d", lines, trackInfoPlaylistLines)
	}
	if !strings.Contains(got, "+6 more") {
		t.Errorf("section %q, want it to summarise the remaining 6", got)
	}
	if n := strings.Count(got, "\n") + 1; n > trackInfoPlaylistSectionLines {
		t.Errorf("section is %d lines, more than the %d rows reserved for it", n, trackInfoPlaylistSectionLines)
	}
}

func TestPlaylistsSectionTruncatesLongNames(t *testing.T) {
	long := strings.Repeat("x", 200)
	got := firstOf(playlistsSection(map[string][]string{"a/1.mp3": {long}}, "a/1.mp3", trackInfoPlaylistLines))
	for _, line := range strings.Split(got, "\n") {
		if len([]rune(line)) > trackInfoCardWidth {
			t.Errorf("line %q is wider than the card (%d)", line, trackInfoCardWidth)
		}
	}
}

// TestPlaylistsSectionSplitsConjuncts matters because the card floats
// over the Queue: a name that mis-measures corrupts the rows behind it
// (see conjuncts.go).
func TestPlaylistsSectionSplitsConjuncts(t *testing.T) {
	got := firstOf(playlistsSection(map[string][]string{"a/1.mp3": {bengaliConjunct}}, "a/1.mp3", trackInfoPlaylistLines))
	if !strings.ContainsRune(got, conjunctJoiner) {
		t.Errorf("section %q lists a conjunct name untreated", got)
	}
}

func TestTrackInfoCardShowsPlaylists(t *testing.T) {
	a := newTestApp()
	a.playlistMembership = map[string][]string{"a/1.mp3": {"Jazz Classics", "Late Night"}}
	a.trackInfo.render(testSong(), mpdclient.Status{})

	got := a.trackInfo.playlists.GetText(true)
	if !strings.Contains(got, "Jazz Classics") || !strings.Contains(got, "Late Night") {
		t.Errorf("card playlist section = %q, want both playlists", got)
	}
}

func TestTrackInfoCardClearsPlaylistsWhenNothingPlaying(t *testing.T) {
	a := newTestApp()
	a.playlistMembership = map[string][]string{"a/1.mp3": {"Jazz Classics"}}
	a.trackInfo.render(testSong(), mpdclient.Status{})
	a.trackInfo.render(mpdclient.Song{}, mpdclient.Status{})

	if got := a.trackInfo.playlists.GetText(true); strings.TrimSpace(got) != "" {
		t.Errorf("playlist section with nothing playing = %q, want it cleared", got)
	}
}

// TestTrackInfoCardHeightMatchesItsSections guards the card growing a
// section without growing to fit it, which would silently clip the
// bottom of the list.
func TestTrackInfoCardHeightMatchesItsSections(t *testing.T) {
	withoutMeta := newTestApp().trackInfo
	if withoutMeta.meta != nil {
		t.Fatal("setup: test app unexpectedly has a metadata database")
	}
	want := trackInfoCardBorderLines + trackInfoIdentityLines + trackInfoPlaylistSectionLines
	if got := withoutMeta.height(); got != want {
		t.Errorf("height without the metadata table = %d, want %d", got, want)
	}

	withMeta := newTestAppWithMetaDB(t).trackInfo
	if withMeta.meta == nil {
		t.Fatal("setup: metadata-enabled test app has no metadata table")
	}
	if got := withMeta.height(); got != want+trackInfoMetaLines {
		t.Errorf("height with the metadata table = %d, want %d", got, want+trackInfoMetaLines)
	}
}

// TestTrackInfoCardPlacementUnchangedByHeight is the explicit ask: the
// card got taller, and that must not move it. It is anchored at the
// quadrant's top-left corner, so extra rows extend downwards.
func TestTrackInfoCardPlacementUnchangedByHeight(t *testing.T) {
	a := newTestApp()
	a.queue.table.SetRect(0, 0, 120, 44)
	a.trackInfo.positionOverQueue()
	x, y, _, h := a.trackInfo.GetRect()

	// The anchor is the Queue panel's midpoint, whatever the card's
	// height happens to be.
	wantX, wantY, _, _ := quadrantRect(0, 0, 120, 44)
	if x != wantX || y != wantY {
		t.Errorf("card anchored at (%d,%d), want the quadrant's top-left (%d,%d)", x, y, wantX, wantY)
	}
	if h <= trackInfoIdentityLines {
		t.Errorf("card height %d leaves no room for the playlist section", h)
	}
}

func TestTrackInfoCardNeverSpillsPastTheQueuePanel(t *testing.T) {
	// A short terminal must clip the card rather than let it overhang
	// the Now Playing bar below the Queue.
	a := newTestApp()
	for _, queueH := range []int{10, 18, 24, 44, 80} {
		a.queue.table.SetRect(0, 0, 120, queueH)
		a.trackInfo.positionOverQueue()
		x, y, w, h := a.trackInfo.GetRect()
		if y+h > queueH {
			t.Errorf("queue height %d: card bottom at %d, past the panel's %d", queueH, y+h, queueH)
		}
		if x+w > 120 {
			t.Errorf("queue height %d: card right edge at %d, past the panel's 120", queueH, x+w)
		}
	}
}

// --- Tab: expand / collapse ---

func TestTabTogglesExpandedWhileCardIsOpen(t *testing.T) {
	a := newTestApp()
	a.queue.table.SetRect(0, 0, 120, 44)
	a.openTrackInfo()

	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	if got := a.globalInputCapture(tab); got != nil {
		t.Errorf("Tab should be consumed by the card, got %v", got)
	}
	if !a.trackInfo.expanded {
		t.Error("first Tab did not expand the card")
	}
	if got := a.globalInputCapture(tab); got != nil {
		t.Errorf("Tab should be consumed by the card, got %v", got)
	}
	if a.trackInfo.expanded {
		t.Error("second Tab did not collapse the card again")
	}
}

// TestTabStillCyclesPanelsWhenTheCardIsClosed guards the obvious way to
// break this: claiming Tab globally rather than only while the card owns
// the screen.
func TestTabStillCyclesPanelsWhenTheCardIsClosed(t *testing.T) {
	a := newTestApp()
	a.focusPanel(libraryPanelIdx)
	before := a.panelIdx

	a.globalInputCapture(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))

	if a.panelIdx == before {
		t.Error("Tab did not cycle panels with no overlay open")
	}
	if a.trackInfo.expanded {
		t.Error("Tab expanded the card while it was closed")
	}
}

func TestExpandingShowsEveryPlaylistAndCollapsingRestores(t *testing.T) {
	a := newTestApp()
	a.queue.table.SetRect(0, 0, 120, 44)
	// toggleExpanded re-renders through targetSong, so the card's track
	// has to be reachable from the Queue, not just handed to render.
	a.queue.songs = []mpdclient.Song{testSong()}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows, 0)
	names := []string{"One", "Two", "Three", "Four", "Five", "Six", "Seven"}
	a.playlistMembership = map[string][]string{"a/1.mp3": names}

	a.renderTrackInfo()
	collapsed := a.trackInfo.playlists.GetText(true)
	collapsedRows := a.trackInfo.playlistRows
	if !strings.Contains(collapsed, "more") {
		t.Fatalf("collapsed section %q should summarise the remainder", collapsed)
	}

	a.trackInfo.toggleExpanded()
	expanded := a.trackInfo.playlists.GetText(true)
	for _, n := range names {
		if !strings.Contains(expanded, n) {
			t.Errorf("expanded section is missing %q: %q", n, expanded)
		}
	}
	if strings.Contains(expanded, "more") {
		t.Errorf("expanded section %q still summarises a remainder", expanded)
	}
	if a.trackInfo.playlistRows <= collapsedRows {
		t.Errorf("expanded section is %d rows, not more than the collapsed %d", a.trackInfo.playlistRows, collapsedRows)
	}

	a.trackInfo.toggleExpanded()
	if got := a.trackInfo.playlists.GetText(true); got != collapsed {
		t.Errorf("collapsing did not restore the summary:\n%q\n%q", got, collapsed)
	}
	if a.trackInfo.playlistRows != collapsedRows {
		t.Errorf("collapsed row count = %d, want the original %d", a.trackInfo.playlistRows, collapsedRows)
	}
}

// TestExpandingNeverMovesTheCardDownOrSideways pins how the card is
// allowed to grow: it fills the quadrant downwards first, and only once
// there is no room left there does it take space above. Either way the
// left edge never moves and the card stays in its corner.
func TestExpandingNeverMovesTheCardDownOrSideways(t *testing.T) {
	var many []string
	for i := 0; i < 30; i++ {
		many = append(many, fmt.Sprintf("Playlist %02d", i))
	}

	for _, queueH := range []int{20, 30, 44, 70} {
		a := newTestApp()
		a.queue.table.SetRect(0, 0, 120, queueH)
		a.playlistMembership = map[string][]string{"a/1.mp3": many}

		a.trackInfo.render(testSong(), mpdclient.Status{})
		a.trackInfo.positionOverQueue()
		cx, cy, _, ch := a.trackInfo.GetRect()

		a.trackInfo.expanded = true
		a.trackInfo.render(testSong(), mpdclient.Status{})
		a.trackInfo.positionOverQueue()
		ex, ey, _, eh := a.trackInfo.GetRect()

		if ex != cx {
			t.Errorf("queue height %d: left edge moved %d -> %d", queueH, cx, ex)
		}
		if ey > cy {
			t.Errorf("queue height %d: expanded card top moved down %d -> %d", queueH, cy, ey)
		}
		if eh < ch {
			t.Errorf("queue height %d: expanded card is shorter (%d) than collapsed (%d)", queueH, eh, ch)
		}
		if ey < 0 || ey+eh > queueH {
			t.Errorf("queue height %d: expanded card spans %d..%d, outside the panel", queueH, ey, ey+eh)
		}
	}
}

// TestExpandedCardKeepsItsBottomEdgeOnceTheQuadrantIsFull is the other
// half of that: with no room left below, growth has to come from above,
// so the bottom edge stays where it is.
func TestExpandedCardKeepsItsBottomEdgeOnceTheQuadrantIsFull(t *testing.T) {
	a := newTestAppWithMetaDB(t) // the taller card, with the metadata table
	// A panel height whose quadrant the collapsed card already fills, so
	// there is no room left below it to grow into.
	a.queue.table.SetRect(0, 0, 120, 40)
	var many []string
	for i := 0; i < 30; i++ {
		many = append(many, fmt.Sprintf("Playlist %02d", i))
	}
	a.playlistMembership = map[string][]string{"a/1.mp3": many}

	_, _, _, qh := quadrantRect(0, 0, 120, 40)
	if a.trackInfo.fixedHeight() < qh {
		t.Fatalf("setup: collapsed card (%d) does not fill the quadrant (%d)", a.trackInfo.fixedHeight(), qh)
	}

	a.trackInfo.render(testSong(), mpdclient.Status{})
	a.trackInfo.positionOverQueue()
	_, cy, _, ch := a.trackInfo.GetRect()
	bottom := cy + ch

	a.trackInfo.expanded = true
	a.trackInfo.render(testSong(), mpdclient.Status{})
	a.trackInfo.positionOverQueue()
	_, ey, _, eh := a.trackInfo.GetRect()

	if ey >= cy {
		t.Errorf("expanded card top at %d, want it above the collapsed %d", ey, cy)
	}
	if ey+eh != bottom {
		t.Errorf("expanded card bottom at %d, want it unchanged at %d", ey+eh, bottom)
	}
}

func TestExpandedCardNeverLeavesTheQueuePanel(t *testing.T) {
	a := newTestApp()
	var many []string
	for i := 0; i < 60; i++ {
		many = append(many, fmt.Sprintf("Playlist %02d", i))
	}
	a.playlistMembership = map[string][]string{"a/1.mp3": many}
	a.trackInfo.expanded = true

	for _, queueH := range []int{8, 14, 22, 44, 90} {
		a.queue.table.SetRect(0, 0, 120, queueH)
		a.trackInfo.render(testSong(), mpdclient.Status{})
		a.trackInfo.positionOverQueue()
		_, y, _, h := a.trackInfo.GetRect()
		if y < 0 || y+h > queueH {
			t.Errorf("queue height %d: expanded card spans %d..%d, outside the panel", queueH, y, y+h)
		}
	}
}

// TestExpandedStillSummarisesWhatCannotFit: expanding is bounded by the
// panel, so a track in more playlists than there are rows must say how
// many were left out rather than silently clipping them.
func TestExpandedStillSummarisesWhatCannotFit(t *testing.T) {
	a := newTestApp()
	a.queue.table.SetRect(0, 0, 120, 24)
	var many []string
	for i := 0; i < 80; i++ {
		many = append(many, fmt.Sprintf("Playlist %02d", i))
	}
	a.playlistMembership = map[string][]string{"a/1.mp3": many}
	a.trackInfo.expanded = true
	a.trackInfo.render(testSong(), mpdclient.Status{})

	if got := a.trackInfo.playlists.GetText(true); !strings.Contains(got, "more") {
		t.Errorf("expanded section on a short panel = %q, want it to say how many were left out", got)
	}
}

// --- which track the card is about ---

// TestTrackInfoFollowsCursorWhilePaused covers the reported bug: paused
// is not playing, so scrolling to another row and pressing 'i' must show
// that row, not the track MPD still has loaded.
func TestTrackInfoFollowsCursorWhilePaused(t *testing.T) {
	a := newTestApp()
	loaded := mpdclient.Song{ID: 1, Title: "Loaded", File: "a/loaded.mp3"}
	other := mpdclient.Song{ID: 2, Title: "Other", File: "a/other.mp3"}
	a.queue.songs = []mpdclient.Song{loaded, other}
	a.queue.render(1)
	a.queue.table.Select(queueHeaderRows+1, 0)
	a.currentSong = loaded
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePause, SongID: loaded.ID}

	a.renderTrackInfo()

	if got := a.trackInfo.identity.GetText(true); !strings.Contains(got, "Other") {
		t.Errorf("card while paused = %q, want the selected track %q", got, "Other")
	}
}

func TestTrackInfoShowsPlayingTrackRegardlessOfCursor(t *testing.T) {
	a := newTestApp()
	playing := mpdclient.Song{ID: 1, Title: "Playing", File: "a/playing.mp3"}
	other := mpdclient.Song{ID: 2, Title: "Other", File: "a/other.mp3"}
	a.queue.songs = []mpdclient.Song{playing, other}
	a.queue.render(1)
	a.queue.table.Select(queueHeaderRows+1, 0)
	a.currentSong = playing
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay, SongID: playing.ID}

	a.renderTrackInfo()

	if got := a.trackInfo.identity.GetText(true); !strings.Contains(got, "Playing") {
		t.Errorf("card while playing = %q, want the playing track", got)
	}
}

// TestAudioQualityOnlyShownForTheTrackItDescribes: bitrate and sample
// format come from the running decoder, so attributing them to a
// merely-selected track would be a lie -- but a paused track the cursor
// is sitting on is that same track, and keeps them.
func TestAudioQualityOnlyShownForTheTrackItDescribes(t *testing.T) {
	a := newTestApp()
	loaded := mpdclient.Song{ID: 1, Title: "Loaded", File: "a/loaded.mp3"}
	other := mpdclient.Song{ID: 2, Title: "Other", File: "a/other.mp3"}
	a.queue.songs = []mpdclient.Song{loaded, other}
	a.queue.render(1)
	a.currentSong = loaded
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePause, SongID: loaded.ID, Bitrate: 320}

	a.queue.table.Select(queueHeaderRows, 0) // cursor on the loaded track
	a.renderTrackInfo()
	if got := a.trackInfo.identity.GetText(true); !strings.Contains(got, "320") {
		t.Errorf("card on the paused track = %q, want its bitrate", got)
	}

	a.queue.table.Select(queueHeaderRows+1, 0) // cursor elsewhere
	a.renderTrackInfo()
	if got := a.trackInfo.identity.GetText(true); strings.Contains(got, "320") {
		t.Errorf("card on another track = %q, must not claim the decoder's bitrate", got)
	}
}
