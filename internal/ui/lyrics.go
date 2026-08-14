package ui

import (
	"fmt"

	"github.com/rivo/tview"

	"mpdtui/internal/lyrics"
	"mpdtui/internal/mpdclient"
)

// lyricsIcon marks a Queue row whose track has a matching lyrics file
// (see internal/lyrics), prefixed to the Title cell rather than given its
// own column -- Queue's columns are already tight (verified at 220 cols
// per this app's own column-width convention), and a whole column for a
// single yes/no marker isn't worth that budget.
const lyricsIcon = "📝"

// lyricsViewer shows the currently playing track's lyrics. Unlike
// trackInfoCard's small floating quadrant card, this is a tall centered
// overlay: lyrics can run to many lines and need real vertical room, not
// just a glanceable summary -- hence "vertical" in openLyricsViewer's own
// naming. Scrolling is entirely tview.TextView's own native vim-style
// key handling (j/k line, g/G top/bottom, h/l horizontal, Ctrl-F/Ctrl-B
// page) once SetScrollable(true) is set -- the same free vim bindings
// Library's TreeView and Queue's Table already have built in, so no
// custom key handling is needed here at all.
type lyricsViewer struct {
	*tview.TextView
	app *App
}

func newLyricsViewer(app *App) *lyricsViewer {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetBorder(true).SetTitle(" Lyrics (j/k/g/G to scroll) ")
	v.SetBorderPadding(1, 1, 2, 2)
	v.SetScrollable(true)
	return &lyricsViewer{TextView: v, app: app}
}

// render loads and shows the lyrics for the currently playing song, fresh
// off disk every time (see internal/lyrics) -- so lyrics added after the
// track was queued still show up without needing a requeue or restart --
// or a placeholder if there's nothing playing, no music directory is
// configured, or there's no matching lyrics file.
func (v *lyricsViewer) render(song mpdclient.Song) {
	v.ScrollToBeginning()
	switch {
	case song.DisplayName() == "":
		v.SetText("[::d]Nothing playing[-:-:-]")
	case v.app.musicDir == "":
		// Hardcoded rather than computed via internal/config.ConfigFile:
		// internal/ui doesn't depend on internal/config (see
		// DEPENDENCY.md -- only cmd/mpdtui and mpdclient do), and adding
		// that edge just for this message's path string, which is
		// correct for the vast majority of setups (a custom
		// $XDG_CONFIG_HOME is the rare exception), isn't worth it.
		v.SetText("[::d]No music directory configured -- set music_dir in ~/.config/mpdtui/config[-:-:-]")
	default:
		text, ok := lyrics.Read(v.app.musicDir, song.File)
		if !ok {
			v.SetText(fmt.Sprintf("[::d]No lyrics found for %s[-:-:-]", song.DisplayName()))
			return
		}
		v.SetText(text)
	}
}

// openLyricsViewer is 'y': opens the lyrics viewer for whichever track is
// currently playing (not whatever's selected in Queue -- same convention
// trackInfoCard already uses), reachable from any panel. 'y' again or Esc
// (both handled globally in overlay mode, see globalInputCapture) close
// it and restore whichever panel was focused before 'y' was pressed, same
// as every other overlay.
//
// Uses a.currentSong (refreshNowPlaying's own last-fetched CurrentSong)
// rather than fetching it again here -- no extra MPD round-trip on open,
// and mirrors trackInfoCard, which relies on the exact same continuously-
// updated state instead of fetching on demand.
func (a *App) openLyricsViewer() {
	a.lyricsViewer.render(a.currentSong)
	a.showOverlay("lyrics", centered(a.lyricsViewer, 70, 34), a.lyricsViewer)
}
