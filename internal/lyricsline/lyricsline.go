// Package lyricsline implements mpdtui's non-interactive "-lyrics-line"
// mode: a single MPD round-trip that prints the current synced (.lrc)
// lyrics window -- one line of context above the current line, the
// current line itself, and two lines of context below -- as four plain
// lines to stdout, then exits. Meant to be invoked repeatedly by an
// external tool (e.g. a conky "${execi N ...}" block, one call per
// line via "sed -n 'Np'") rather than run interactively.
package lyricsline

import (
	"fmt"
	"io"

	"mpdtui/internal/lyrics"
	"mpdtui/internal/mpdclient"
)

// linesAbove/linesBelow fix the window's shape around the current line:
// 1 line of context above, the current line, then 2 lines below.
const (
	linesAbove = 1
	linesBelow = 2
	windowSize = linesAbove + 1 + linesBelow
)

// Window returns exactly windowSize lines of text centered on idx (as
// returned by lyrics.CurrentLineIndex): linesAbove lines of context,
// idx's own text, then linesBelow lines of context. Any slot that falls
// outside lines -- idx == -1 (before the first timestamp, e.g. during an
// instrumental intro), or past the end of the song -- is the empty
// string rather than omitted, so the output is always exactly windowSize
// lines regardless of position in the track: callers (e.g. a
// fixed-line-count conky layout) can rely on that instead of having to
// handle a variable number of lines.
func Window(lines []lyrics.LyricLine, idx int) [windowSize]string {
	var out [windowSize]string
	for slot, i := 0, idx-linesAbove; slot < windowSize; slot, i = slot+1, i+1 {
		if i >= 0 && i < len(lines) {
			out[slot] = lines[i].Text
		}
	}
	return out
}

// Print performs one MPD round-trip -- CurrentSong, and Status only if a
// track with a matching .lrc is actually playing -- resolves and parses
// that track's .lrc sidecar under musicDir (if any), and writes exactly
// windowSize lines to w (see Window), using elapsed to pick the current
// line. Writes windowSize blank lines rather than returning an error
// when nothing is playing or the track has no synced lyrics -- the whole
// point of the fixed-line-count output is that an external layout
// (conky, a status bar) never has to special-case those states.
func Print(client *mpdclient.Client, musicDir string, w io.Writer) error {
	var window [windowSize]string
	song, err := client.CurrentSong()
	if err != nil {
		return err
	}
	if song.File != "" && musicDir != "" {
		if lines, ok := lyrics.ReadLRC(musicDir, song.File); ok {
			status, err := client.Status()
			if err != nil {
				return err
			}
			window = Window(lines, lyrics.CurrentLineIndex(lines, status.Elapsed))
		}
	}
	for _, line := range window {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}
