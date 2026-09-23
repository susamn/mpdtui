package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/mpdclient"
	"mpdtui/internal/uitheme"
)

func TestOverlayCardsUseActiveBorderAndDefocusQueue(t *testing.T) {
	cases := []struct {
		name string
		open func(*App)
		card func(*App) borderTitler
	}{
		{"track info", (*App).openTrackInfo, func(a *App) borderTitler { return a.trackInfo }},
		{"settings", (*App).openSettings, func(a *App) borderTitler { return a.settings }},
		{"library card", (*App).openLibraryCard, func(a *App) borderTitler { return a.libraryCard }},
		{"bookmark manager", func(a *App) { a.openBookmarkManager(mpdclient.Song{File: "a.mp3"}) }, func(a *App) borderTitler { return a.bookmarkPicker }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newFakeAppWithMetaDB(t, &fakeMPD{})
			a.focusPanel(queuePanelIdx)

			tc.open(a)

			if got := tc.card(a).(interface{ GetBorderColor() tcell.Color }).GetBorderColor(); got != uitheme.ActiveBorder() {
				t.Errorf("overlay border = %v, want active border %v", got, uitheme.ActiveBorder())
			}
			if got := a.queue.table.GetBorderColor(); got != uitheme.InactiveBorder() {
				t.Errorf("queue border while overlay is open = %v, want inactive border %v", got, uitheme.InactiveBorder())
			}
		})
	}
}
