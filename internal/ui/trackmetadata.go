package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

// metadataNotEnabled flashes a consistent message for every track-
// metadata action (rating, marking) attempted while the feature isn't
// active (App.metaDB == nil) -- explains what to do about it, unlike a
// plain invalidKey flash, since this is a real, bound feature that's
// just off (the default) rather than unbound.
func (a *App) metadataNotEnabled() {
	a.flash("[red]track metadata not enabled -- set track_metadata = true in ~/.config/mpdtui/config[-]")
}

// ratingStars renders rating (0-5) as filled/empty star glyphs.
func ratingStars(rating int) string {
	return strings.Repeat("★", rating) + strings.Repeat("☆", 5-rating)
}

// handleRateSelectedTrack is '1'-'5', scoped to the Queue panel (see
// globalInputCapture): rates whichever track is currently selected there
// -- not necessarily the one playing. A no-op (flashed) if the feature
// isn't active or nothing is selected. The confirmation flash is
// immediate; the actual database write and the Queue panel's Rating
// cell repaint both happen in the background (see App.runAsync) so
// rating a track never makes the keypress wait on disk I/O.
func (a *App) handleRateSelectedTrack(rating int) {
	if a.metaDB == nil {
		a.metadataNotEnabled()
		return
	}
	song, ok := a.queue.selectedSong()
	if !ok {
		return
	}
	a.showMessage(fmt.Sprintf("rated %s: %s", ratingStars(rating), song.DisplayName()))
	db := a.metaDB
	a.runAsync(func() error {
		return db.Rate(song.File, rating)
	}, func() {
		t := a.queue.metaCache[song.File]
		t.Rating = rating
		a.queue.applyTrackMeta(song.File, t)
	})
}

// maybeTrackPlayCount increments the currently playing track's local
// play count (internal/metadata) once it's been played at least halfway
// through, per explicit direction ("if 50% of a track is played marks
// that track played"). Counted at most once per distinct queue song id
// (a.playCountedSongID) -- ticking past the halfway point on every
// ~500ms refresh, or seeking back and forth across it, doesn't inflate
// the count, while a genuine repeat play (a fresh SongID once MPD
// re-adds/replays it) counts again. No-op if the feature isn't active,
// nothing is playing, or the duration is unknown (can't compute a
// halfway point). playCountedSongID is set immediately (before the
// database write, which runs in the background -- see App.runAsync) so
// a second refresh landing before that write finishes can't double-count.
func (a *App) maybeTrackPlayCount(st mpdclient.Status, song mpdclient.Song) {
	if a.metaDB == nil || st.SongID < 0 || song.File == "" {
		return
	}
	if st.SongID == a.playCountedSongID {
		return
	}
	if st.Duration <= 0 || st.Elapsed*2 < st.Duration {
		return
	}
	a.playCountedSongID = st.SongID
	db := a.metaDB
	file := song.File
	a.runAsync(func() error {
		return db.IncrementPlayCount(file)
	}, func() {})
}

// markPicker lets you assign (or clear) a mark reason on the Queue-
// selected track, from internal/metadata's mark_reason catalog. Built
// once (like trackInfoCard/lyricsViewer) and repopulated fresh from the
// catalog every time it's opened, in case reasons were added since (the
// catalog is meant to be edited by hand for now, see
// internal/metadata's own doc comment).
type markPicker struct {
	*tview.List
	app     *App
	reasons []metadata.MarkReason
}

func newMarkPicker(app *App) *markPicker {
	m := &markPicker{app: app}
	l := tview.NewList()
	l.ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetSelectedTextColor(colorSelectedFg)
	l.SetSelectedBackgroundColor(colorSelectedBg)
	l.SetBorder(true).SetTitle(" Mark (Enter to apply, Esc to cancel) ")
	// j/k/g/G: List has no native vim bindings (unlike Table/TreeView --
	// see the same note on internal/ui/globalsearch.go's list). Reuses
	// moveHintHighlight, the exact same wrap-around arithmetic the
	// global-search hint list already uses, rather than a second copy of
	// it.
	l.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 'j':
				l.SetCurrentItem(moveHintHighlight(l.GetCurrentItem(), l.GetItemCount(), 1))
				return nil
			case 'k':
				l.SetCurrentItem(moveHintHighlight(l.GetCurrentItem(), l.GetItemCount(), -1))
				return nil
			case 'g':
				if l.GetItemCount() > 0 {
					l.SetCurrentItem(0)
				}
				return nil
			case 'G':
				if n := l.GetItemCount(); n > 0 {
					l.SetCurrentItem(n - 1)
				}
				return nil
			}
		}
		return event
	})
	// Enter: List's own native SetSelectedFunc, no custom handling needed
	// (unlike global search's hint list, which is driven from an
	// InputField rather than the list itself holding focus).
	l.SetSelectedFunc(func(index int, _, _ string, _ rune) {
		m.apply(index)
	})
	m.List = l
	return m
}

// render repopulates the list from reasons, plus a synthetic leading
// "(clear mark)" entry so an already-marked track can be unmarked from
// the same popup rather than needing a separate mechanism.
func (m *markPicker) render(reasons []metadata.MarkReason) {
	m.reasons = reasons
	m.Clear()
	m.AddItem("(clear mark)", "", 0, nil)
	for _, r := range reasons {
		m.AddItem(r.Reason, "", 0, nil)
	}
	if m.GetItemCount() > 0 {
		m.SetCurrentItem(0)
	}
}

// apply is the list's own SetSelectedFunc (Enter): index 0 is the
// synthetic "clear mark" entry, index i>0 is m.reasons[i-1]. Like
// handleRateSelectedTrack, the confirmation flash is immediate while the
// database write and the Queue panel's Mark cell repaint both happen in
// the background (see App.runAsync).
func (m *markPicker) apply(index int) {
	song, ok := m.app.queue.selectedSong()
	if !ok {
		m.app.closeOverlay()
		return
	}
	m.app.closeOverlay()
	db := m.app.metaDB

	if index == 0 {
		m.app.showMessage("unmarked: " + song.DisplayName())
		m.app.runAsync(func() error {
			return db.SetMark(song.File, nil)
		}, func() {
			t := m.app.queue.metaCache[song.File]
			t.Mark = nil
			m.app.queue.applyTrackMeta(song.File, t)
		})
		return
	}
	if index-1 >= len(m.reasons) {
		return
	}
	reason := m.reasons[index-1]
	m.app.showMessage(fmt.Sprintf("marked (%s): %s", reason.Reason, song.DisplayName()))
	m.app.runAsync(func() error {
		return db.SetMark(song.File, &reason.ID)
	}, func() {
		t := m.app.queue.metaCache[song.File]
		t.Mark = &reason
		m.app.queue.applyTrackMeta(song.File, t)
	})
}

// handleOpenMarkPicker is 'm', scoped to the Queue panel like rating:
// opens the mark-reason popup for whichever track is currently selected
// there. j/k/g/G navigate, Enter applies and closes, Esc cancels;
// transport controls stay live while it's open (see
// globalInputCapture's modeOverlay branch), same reasoning as the lyrics
// viewer -- explicitly requested regardless of which overlay is up.
func (a *App) handleOpenMarkPicker() {
	if a.tv.GetFocus() != a.queue.table {
		a.invalidKey("m")
		return
	}
	if a.metaDB == nil {
		a.metadataNotEnabled()
		return
	}
	if _, ok := a.queue.selectedSong(); !ok {
		return
	}
	reasons, err := a.metaDB.ListMarkReasons()
	if err != nil {
		a.showError(err)
		return
	}
	a.markPicker.render(reasons)
	// Height follows the item count (plus the list's own top/bottom
	// border), with a floor so the popup doesn't look cramped for just
	// the seeded "(clear mark)"+"mark for deletion" pair.
	height := a.markPicker.GetItemCount() + 2
	if height < 8 {
		height = 8
	}
	a.showOverlay("mark", centered(a.markPicker, 50, height), a.markPicker)
}
