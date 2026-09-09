package ui

import (
	"fmt"
	"strings"
	"time"

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
	return strings.Repeat(ratingStarFilled, rating) + strings.Repeat(ratingStarEmpty, 5-rating)
}

// ratingStarFilled and ratingStarEmpty are the rating glyphs, shared so
// the Queue's gutter star (see queueGutterCell) is literally the same
// character the Rating column fills in, not a lookalike.
const (
	ratingStarFilled = "★"
	ratingStarEmpty  = "☆"
)

// handleRateSelectedTrack is '1'-'5', scoped to the Queue panel (see
// globalInputCapture): rates the currently playing track, falling back
// to the Queue selection when nothing is playing -- see App.targetSong.
// A no-op (flashed) if the feature isn't active, or if nothing is
// playing and nothing is selected. The confirmation flash is immediate;
// the actual database write and the Queue panel's Rating cell repaint
// both happen in the background (see App.runAsync) so rating a track
// never makes the keypress wait on disk I/O.
func (a *App) handleRateSelectedTrack(rating int) {
	if a.metaDB == nil {
		a.metadataNotEnabled()
		return
	}
	song, ok := a.targetSong()
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

// playCountRearmElapsed is how close to the very start of a track
// Elapsed has to be, for a SongID already counted by maybeTrackPlayCount,
// to be treated as a fresh play rather than still the one already
// counted. Once counted, a SongID's Elapsed was already at or past the
// halfway point, so a later Elapsed this low for that same SongID can
// only mean the track restarted from the beginning -- a repeat-mode
// loop, or the user replaying the same still-queued entry -- both of
// which reuse the SongID MPD already assigned rather than getting a
// fresh one, unlike a genuine re-add. A few seconds of margin (rather
// than exactly 0) absorbs the ~500ms refresh tick's own polling slop.
const playCountRearmElapsed = 3 * time.Second

// maybeTrackPlayCount increments the currently playing track's local
// play count (internal/metadata) once it's been played at least halfway
// through, per explicit direction ("if 50% of a track is played marks
// that track played"). Counted at most once per distinct queue song id
// (a.playCountedSongID) -- ticking past the halfway point on every
// ~500ms refresh, or seeking back and forth across it, doesn't inflate
// the count, while a genuine repeat play counts again: either a fresh
// SongID (MPD re-added/replaced the track) or the same SongID restarted
// from the beginning (a repeat-mode loop, or replaying the same
// still-queued entry -- see playCountRearmElapsed). No-op if the
// feature isn't active, nothing is playing, or the duration is unknown
// (can't compute a halfway point). playCountedSongID is set immediately
// (before the database write, which runs in the background -- see
// App.runAsync) so a second refresh landing before that write finishes
// can't double-count. The Queue panel's Plays cell is repainted once
// the write lands, same as the Rating/Mark cells after a rate/mark
// action.
func (a *App) maybeTrackPlayCount(st mpdclient.Status, song mpdclient.Song) {
	if a.metaDB == nil || st.SongID < 0 || song.File == "" {
		return
	}
	if st.SongID == a.playCountedSongID {
		if st.Elapsed >= playCountRearmElapsed {
			return
		}
		a.playCountedSongID = -1
	}
	if st.Duration <= 0 || st.Elapsed*2 < st.Duration {
		return
	}
	a.playCountedSongID = st.SongID
	db := a.metaDB
	file := song.File
	a.runAsync(func() error {
		return db.IncrementPlayCount(file)
	}, func() {
		t := a.queue.metaCache[file]
		t.PlayCount++
		a.queue.applyTrackMeta(file, t)
	})
}

// markPicker toggles mark reasons on the track App.targetSong resolves
// to, from internal/metadata's mark_reason catalog. Built once (like
// trackInfoCard/lyricsViewer) and repopulated fresh from the catalog
// every time it's opened, in case reasons were added since (the catalog
// is meant to be edited by hand for now, see internal/metadata's own doc
// comment).
//
// A track can carry several marks at once, so this is a checklist rather
// than a one-of-N choice: Enter toggles the highlighted reason and the
// popup stays open, because the natural thing after adding one mark is
// to add another. Each row shows whether that reason is currently set,
// so the popup doubles as the answer to "what is this track marked
// with?".
type markPicker struct {
	*tview.List
	app     *App
	reasons []metadata.MarkReason

	// song is the track this popup was opened for, captured once by
	// render rather than re-resolved in apply. Transport controls stay
	// live while an overlay is up (see globalInputCapture's modeOverlay
	// branch) and a track can auto-advance on its own, so re-resolving
	// the target on Enter could mark a track other than the one the
	// popup's own title said it was for.
	song mpdclient.Song
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
// "(clear all marks)" entry -- still worth having when marks are a set,
// since clearing several one by one is tedious. song is the track the
// popup acts on for as long as it stays open (see the field's own
// comment), and names it in the title so there's no doubt which track is
// about to be marked.
func (m *markPicker) render(song mpdclient.Song, reasons []metadata.MarkReason) {
	m.song = song
	m.SetTitle(" Mark \"" + song.DisplayName() + "\" (Enter to toggle, Esc to close) ")
	m.reasons = reasons
	m.refresh()
}

// refresh redraws the list against the track's current marks, keeping
// the cursor where it was. Called after every toggle so the row the user
// just changed visibly reflects it without closing the popup.
func (m *markPicker) refresh() {
	current := m.GetCurrentItem()
	set := map[int64]bool{}
	for _, mk := range m.app.queue.metaCache[m.song.File].Marks {
		set[mk.ID] = true
	}

	m.Clear()
	m.AddItem("(clear all marks)", "", 0, nil)
	for _, r := range m.reasons {
		m.AddItem(markPickerLabel(r.Reason, set[r.ID]), "", 0, nil)
	}
	if current < m.GetItemCount() {
		m.SetCurrentItem(current)
	}
}

// markPickerLabel prefixes a reason with whether it is currently set, so
// the checklist reads at a glance. A box rather than a bare tick: an
// empty row still shows the slot, which is what makes the set ones
// obvious.
func markPickerLabel(reason string, on bool) string {
	if on {
		return "[x] " + reason
	}
	return "[ ] " + reason
}

// apply is the list's own SetSelectedFunc (Enter): index 0 is the
// synthetic "clear all marks" entry, index i>0 is m.reasons[i-1].
//
// Unlike the other metadata popups this does not close on Enter -- marks
// are a set, and closing after each one would make marking a track twice
// a two-popup job. Like handleRateSelectedTrack, the confirmation flash
// is immediate while the database write and the Queue panel's Mark cell
// repaint both happen in the background (see App.runAsync).
func (m *markPicker) apply(index int) {
	song := m.song
	if song.File == "" {
		m.app.closeOverlay()
		return
	}
	db := m.app.metaDB

	if index == 0 {
		m.app.showMessage("cleared marks: " + song.DisplayName())
		m.app.runAsync(func() error {
			return db.SetMarks(song.File, nil)
		}, func() {
			t := m.app.queue.metaCache[song.File]
			t.Marks = nil
			m.app.queue.applyTrackMeta(song.File, t)
			m.refresh()
		})
		return
	}
	if index-1 >= len(m.reasons) {
		return
	}
	reason := m.reasons[index-1]

	// The database decides whether this is an add or a remove, and
	// reports which -- rather than the UI predicting it from the cache
	// and risking a message that contradicts what was written.
	var added bool
	m.app.runAsync(func() error {
		var err error
		added, err = db.ToggleMark(song.File, reason.ID)
		return err
	}, func() {
		t := m.app.queue.metaCache[song.File]
		t.Marks = toggleMarkIn(t.Marks, reason, added)
		m.app.queue.applyTrackMeta(song.File, t)
		m.refresh()
		if added {
			m.app.showMessage(fmt.Sprintf("marked (%s): %s", reason.Reason, song.DisplayName()))
		} else {
			m.app.showMessage(fmt.Sprintf("unmarked (%s): %s", reason.Reason, song.DisplayName()))
		}
	})
}

// toggleMarkIn mirrors the database write in the cached Track: adds
// reason if added, removes it otherwise, keeping the catalog-id ordering
// marksForTrack returns so the cache and a fresh read agree.
func toggleMarkIn(marks []metadata.MarkReason, reason metadata.MarkReason, added bool) []metadata.MarkReason {
	out := make([]metadata.MarkReason, 0, len(marks)+1)
	inserted := false
	for _, m := range marks {
		if m.ID == reason.ID {
			continue // dropped; re-added below in order if still set
		}
		if added && !inserted && m.ID > reason.ID {
			out = append(out, reason)
			inserted = true
		}
		out = append(out, m)
	}
	if added && !inserted {
		out = append(out, reason)
	}
	return out
}

// handleOpenMarkPicker is 'm', scoped to the Queue panel like rating:
// opens the mark-reason popup for the currently playing track, falling
// back to the Queue selection when nothing is playing (see
// App.targetSong). j/k/g/G navigate, Enter toggles the highlighted mark
// and leaves the popup open, Esc closes;
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
	song, ok := a.targetSong()
	if !ok {
		return
	}
	reasons, err := a.metaDB.ListMarkReasons()
	if err != nil {
		a.showError(err)
		return
	}
	a.markPicker.render(song, reasons)
	// Height follows the item count (plus the list's own top/bottom
	// border), with a floor so the popup doesn't look cramped for just
	// the seeded "(clear all marks)"+"mark for deletion" pair.
	height := a.markPicker.GetItemCount() + 2
	if height < 8 {
		height = 8
	}
	a.showOverlay("mark", centered(a.markPicker, 50, height), a.markPicker)
}
