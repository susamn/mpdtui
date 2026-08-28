package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rivo/tview"

	"mpdtui/internal/lyricsindex"
)

// handleReindexLyrics is 'I': rebuild the persistent lyrics search index
// (see internal/lyricsindex) that the "l" global search reads from. The
// scan of every track's .txt/.lrc sidecar runs on a background goroutine
// so it never blocks the UI; a small overlay shows live progress and the
// final tally, and Esc cancels a run in flight (the previous index is
// left untouched -- the rebuild commits in one transaction).
//
// Needs music_dir configured; without it there's nothing to scan, so it
// just flashes a hint instead of opening the overlay.
func (a *App) handleReindexLyrics() {
	if a.musicDir == "" {
		a.showMessage("lyrics index needs music_dir set in ~/.config/mpdtui/config")
		return
	}
	if a.cfg.LyricsIndexPath == "" {
		a.showMessage("lyrics index unavailable: can't locate a config directory")
		return
	}

	status := tview.NewTextView().SetDynamicColors(true)
	status.SetBorder(true).SetTitle(" Lyrics index (Esc to close) ")

	prevInfo, _ := lyricsindex.ReadInfo(a.cfg.LyricsIndexPath)
	if prevInfo.Exists {
		status.SetText(fmt.Sprintf(
			"Rebuilding lyrics index...\n\nCurrent: %d tracks, last built %s",
			prevInfo.Count, prevInfo.IndexedAt.Format("2006-01-02 15:04")))
	} else {
		status.SetText("Building lyrics index for the first time...")
	}

	a.showOverlay("lyrics-reindex", centered(status, 60, 8), status)

	ctx, cancel := context.WithCancel(context.Background())
	closed := false
	prevClose := a.closeOverlay
	a.closeOverlay = func() {
		closed = true
		cancel()
		prevClose()
	}

	// setText only paints while the overlay is still up -- once cancelled
	// or closed the goroutine may still deliver a straggler update.
	setText := func(s string) {
		if !closed {
			status.SetText(s)
		}
	}

	go func() {
		songs, err := a.client.AllSongs()
		if err != nil {
			a.tv.QueueUpdateDraw(func() { setText("[red]failed to list tracks: " + err.Error() + "[-]") })
			return
		}
		tracks := make([]lyricsindex.Track, len(songs))
		for i, s := range songs {
			tracks[i] = lyricsindex.Track{File: s.File, Display: s.DisplayName()}
		}

		lastPaint := time.Now()
		progress := func(done, total int) {
			// Throttle the cross-goroutine repaint: the callback itself
			// already fires only every few hundred tracks, this keeps a
			// very large library from still queueing hundreds of draws.
			if time.Since(lastPaint) < 100*time.Millisecond && done < total {
				return
			}
			lastPaint = time.Now()
			a.tv.QueueUpdateDraw(func() {
				setText(fmt.Sprintf("Scanning sidecars...\n\n%d / %d tracks", done, total))
			})
		}

		stats, err := lyricsindex.Reindex(ctx, a.cfg.LyricsIndexPath, a.musicDir, tracks, progress)
		a.tv.QueueUpdateDraw(func() {
			switch {
			case errors.Is(err, context.Canceled):
				// Overlay is already gone (Esc triggered the cancel); nothing to show.
			case err != nil:
				setText("[red]reindex failed: " + err.Error() + "[-]")
			default:
				setText(fmt.Sprintf(
					"[green]Done.[-]  %d tracks with lyrics indexed\n\n"+
						"read %d  •  unchanged %d  •  removed %d\nfinished in %s\n\nEsc to close",
					stats.Indexed, stats.Read, stats.Unchanged, stats.Removed,
					stats.Elapsed.Round(time.Millisecond)))
			}
		})
	}()
}
