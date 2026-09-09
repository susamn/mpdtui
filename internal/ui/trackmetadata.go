package ui

import (
	"fmt"
	"strings"
	"time"

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

// handleOpenMarkPicker is 'm', scoped to the Queue panel like rating:
// opens the mark-reason popup for the currently playing track, falling
// back to the Queue selection when nothing is playing (see
// App.targetSong). j/k/g/G navigate, Enter toggles the highlighted mark
// and leaves the popup open, Esc closes;
// transport controls stay live while it's open (see
// globalInputCapture's modeOverlay branch), same reasoning as the lyrics
// viewer -- explicitly requested regardless of which overlay is up.
func (a *App) handleOpenMarkPicker() {
	a.openCatalogPicker("m", "mark", a.markPicker)
}

// handleOpenTagPicker is 't', the tags counterpart of 'm'. Tags have
// been a many-to-many relation in the database since before marks were,
// but nothing ever called SetTags -- Settings could edit the tag catalog
// and the Track Info card could display a track's tags, with no way in
// between to actually put one on a track. This is that way.
func (a *App) handleOpenTagPicker() {
	a.openCatalogPicker("t", "tag", a.tagPicker)
}

// openCatalogPicker is the shared body of 'm' and 't': same gating
// (Queue focus, metadata enabled, a resolvable target), same sizing,
// same overlay handling.
func (a *App) openCatalogPicker(key, name string, picker *catalogPicker) {
	if a.tv.GetFocus() != a.queue.table {
		a.invalidKey(key)
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
	items, err := picker.kind.entries(a.metaDB)
	if err != nil {
		a.showError(err)
		return
	}
	// The popup reads the Queue's cache to know what is already set, so
	// make sure it holds this track before rendering -- otherwise the
	// first open after startup shows everything as unset.
	if _, ok := a.queue.metaCache[song.File]; !ok {
		if track, err := a.metaDB.Get(song.File); err == nil {
			a.queue.metaCache[song.File] = track
		}
	}
	picker.render(song, items)
	// Height follows the item count (plus the list's own top/bottom
	// border), with a floor so the popup doesn't look cramped for just
	// the seeded "(clear all ...)"+one-entry pair.
	height := picker.GetItemCount() + 2
	if height < 8 {
		height = 8
	}
	a.showOverlay(name, centered(picker, 50, height), picker)
}
