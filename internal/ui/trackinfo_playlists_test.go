package ui

import (
	"fmt"
	"strings"
	"testing"

	"mpdtui/internal/mpdclient"
)

func testSong() mpdclient.Song {
	return mpdclient.Song{Title: "Kind of Blue", Album: "Milestones", Artist: "Miles Davis",
		Genre: "Jazz", Date: "1959", File: "a/1.mp3", Duration: 400}
}

// TestPlaylistsSectionDistinguishesUnknownFromEmpty is the point of the
// section: before the background scan lands there is no answer, and
// saying "none" then would be wrong in a way the user cannot tell apart
// from the truth.
func TestPlaylistsSectionDistinguishesUnknownFromEmpty(t *testing.T) {
	notFetched := playlistsSectionText(nil, "a/1.mp3")
	if !strings.Contains(notFetched, "loading") {
		t.Errorf("with no scan yet = %q, want it to say it is still loading", notFetched)
	}
	if strings.Contains(notFetched, "none") {
		t.Errorf("with no scan yet = %q, must not claim the track is in no playlist", notFetched)
	}

	fetched := playlistsSectionText(map[string][]string{"other.mp3": {"X"}}, "a/1.mp3")
	if !strings.Contains(fetched, "none") {
		t.Errorf("after a scan with no match = %q, want %q", fetched, "none")
	}
	if strings.Contains(fetched, "loading") {
		t.Errorf("after a scan = %q, must not still say loading", fetched)
	}
}

func TestPlaylistsSectionListsEveryName(t *testing.T) {
	got := playlistsSectionText(map[string][]string{
		"a/1.mp3": {"Jazz Classics", "Late Night", "Thumri"},
	}, "a/1.mp3")

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
	got := playlistsSectionText(map[string][]string{"a/1.mp3": names}, "a/1.mp3")

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
	got := playlistsSectionText(map[string][]string{"a/1.mp3": {long}}, "a/1.mp3")
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
	got := playlistsSectionText(map[string][]string{"a/1.mp3": {bengaliConjunct}}, "a/1.mp3")
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
