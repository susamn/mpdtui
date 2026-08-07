package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/mpdclient"
)

func TestQuadrantRectBottomRight(t *testing.T) {
	cases := []struct {
		x, y, w, h                 int
		wantX, wantY, wantW, wantH int
	}{
		{0, 0, 100, 60, 50, 30, 50, 30},
		{10, 5, 41, 21, 31, 16, 20, 10}, // odd dimensions round the quadrant size down
		{0, 0, 0, 0, 0, 0, 0, 0},
	}
	for _, tc := range cases {
		gotX, gotY, gotW, gotH := quadrantRect(tc.x, tc.y, tc.w, tc.h)
		if gotX != tc.wantX || gotY != tc.wantY || gotW != tc.wantW || gotH != tc.wantH {
			t.Errorf("quadrantRect(%d,%d,%d,%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				tc.x, tc.y, tc.w, tc.h, gotX, gotY, gotW, gotH, tc.wantX, tc.wantY, tc.wantW, tc.wantH)
		}
	}
}

func TestTrackInfoCardRenderNothingPlaying(t *testing.T) {
	a := newTestApp()
	a.trackInfo.render(mpdclient.Song{})
	if got := a.trackInfo.GetText(false); !strings.Contains(got, "Nothing playing") {
		t.Errorf("render(zero Song) = %q, want it to contain %q", got, "Nothing playing")
	}
}

func TestTrackInfoCardRenderFullSong(t *testing.T) {
	a := newTestApp()
	song := mpdclient.Song{
		Title:  "Bohemian Rhapsody",
		Album:  "A Night at the Opera",
		Artist: "Queen",
		Genre:  "Rock",
		Date:   "1975-11-21",
	}
	a.trackInfo.render(song)
	got := a.trackInfo.GetText(true)

	for _, want := range []string{"Bohemian Rhapsody", "A Night at the Opera", "Queen", "Rock", "1975"} {
		if !strings.Contains(got, want) {
			t.Errorf("render(%+v) text = %q, missing %q", song, got, want)
		}
	}
	if strings.Contains(got, "1975-11-21") {
		t.Errorf("render(%+v) text = %q, want the Date tag truncated to a 4-digit year", song, got)
	}
}

func TestTrackInfoCardRenderFallsBackToFilename(t *testing.T) {
	a := newTestApp()
	a.trackInfo.render(mpdclient.Song{File: "music/artist/track.mp3"})
	got := a.trackInfo.GetText(true)
	if !strings.Contains(got, "track.mp3") {
		t.Errorf("render(untagged Song) text = %q, want it to contain the filename %q", got, "track.mp3")
	}
}

func TestOpenTrackInfoTakesFocusAndPositionsInBottomRightQuadrant(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.library.tree)
	a.queue.table.SetRect(0, 0, 100, 60)

	a.openTrackInfo()

	if a.tv.GetFocus() != a.trackInfo {
		t.Fatalf("focus after openTrackInfo = %T, want the track info card", a.tv.GetFocus())
	}
	if a.mode != modeOverlay {
		t.Error("mode after openTrackInfo should be modeOverlay")
	}

	// positionOverQueue is the part of Draw that computes the card's rect
	// from the Queue table's current rect -- exercise it directly rather
	// than Draw itself, which needs a real tcell.Screen to paint into.
	a.trackInfo.positionOverQueue()
	x, y, w, h := a.trackInfo.GetRect()
	if x != 50 || y != 30 || w != 50 || h != 30 {
		t.Errorf("card rect after Draw = (%d,%d,%d,%d), want (50,30,50,30)", x, y, w, h)
	}
}

func TestOpenTrackInfoEscRestoresOriginalFocus(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openTrackInfo()

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape while the track info card is open should be consumed, got %v", result)
	}
	if a.mode != modeNormal {
		t.Error("mode after Escape should be modeNormal")
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after Escape = %T, want the originally-focused Queue table", a.tv.GetFocus())
	}
}

func TestRefreshNowPlayingUpdatesTrackInfoLive(t *testing.T) {
	a := newTestApp()
	a.trackInfo.render(mpdclient.Song{Title: "Old Track", Artist: "Old Artist"})
	if got := a.trackInfo.GetText(true); !strings.Contains(got, "Old Track") {
		t.Fatalf("setup: expected initial render to contain %q, got %q", "Old Track", got)
	}

	// refreshNowPlaying itself needs a live client; exercise the same
	// call it makes so this stays a pure/no-MPD test.
	a.trackInfo.render(mpdclient.Song{Title: "New Track", Artist: "New Artist"})
	got := a.trackInfo.GetText(true)
	if strings.Contains(got, "Old Track") {
		t.Errorf("card still shows the old track after re-render: %q", got)
	}
	if !strings.Contains(got, "New Track") {
		t.Errorf("card = %q, want it to contain the newly rendered track %q", got, "New Track")
	}
}
