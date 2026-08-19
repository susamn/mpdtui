// Package lyricsline implements mpdtui's non-interactive "-lyrics-line"
// and "-lyrics-line-exact" modes: a single MPD round-trip that prints
// the current synced (.lrc) lyrics, then exits. "-lyrics-line" prints a
// four-line window -- one line of context above the current line, the
// current line itself, and two lines of context below. "-lyrics-line-
// exact" prints just the current line, no context. Meant to be invoked
// repeatedly by an external tool (e.g. a conky "${execi N ...}" block,
// one call per line via "sed -n 'Np'" for the windowed mode) rather
// than run interactively.
package lyricsline

import (
	"fmt"
	"io"

	"mpdtui/internal/lyrics"
	"mpdtui/internal/mpdclient"
)

// linesAbove/linesBelow fix the window's shape around the current line:
// 1 line of context above, the current line, then 2 lines below. The
// current line always lands at window index linesAbove by construction
// (see Window) -- slots 0..linesAbove-1 are above-context, linesAbove
// is current, the rest are below-context.
const (
	linesAbove = 1
	linesBelow = 2
	windowSize = linesAbove + 1 + linesBelow
)

// noSyncedLyrics is written in place of the current line when the
// playing track (or nothing playing at all) has no matching .lrc
// sidecar -- both Print and PrintExact use it, so an external tool
// gets a self-explanatory value instead of having to tell "no synced
// lyrics" apart from a genuinely blank line (e.g. an instrumental
// intro before the first timestamp, which stays blank since synced
// lyrics do exist for that track).
const noSyncedLyrics = "No synced lyrics available"

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

// currentWindow performs one MPD round-trip -- CurrentSong, and Status
// only if a track with a matching .lrc is actually playing -- resolves
// and parses that track's .lrc sidecar under musicDir (if any), and
// returns the windowSize-line window (see Window) using elapsed to pick
// the current line. When nothing is playing, musicDir isn't configured,
// or the track has no .lrc sidecar, the current-line slot (window index
// linesAbove) is noSyncedLyrics instead of blank and every other slot
// stays blank -- still exactly windowSize lines either way, so callers
// never have to special-case those states.
func currentWindow(client *mpdclient.Client, musicDir string) ([windowSize]string, error) {
	var window [windowSize]string
	song, err := client.CurrentSong()
	if err != nil {
		return window, err
	}
	var hasLRC bool
	if song.File != "" && musicDir != "" {
		if lines, ok := lyrics.ReadLRC(musicDir, song.File); ok {
			hasLRC = true
			status, err := client.Status()
			if err != nil {
				return window, err
			}
			window = Window(lines, lyrics.CurrentLineIndex(lines, status.Elapsed))
		}
	}
	if !hasLRC {
		window[linesAbove] = noSyncedLyrics
	}
	return window, nil
}

// Print writes currentWindow's full windowSize-line window to w, one
// line per slot -- for external tools that want fixed above/below
// context around the current line (e.g. a multi-line conky block).
func Print(client *mpdclient.Client, musicDir string, w io.Writer) error {
	window, err := currentWindow(client, musicDir)
	if err != nil {
		return err
	}
	for _, line := range window {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// PrintExact writes only the current line from currentWindow's window
// (window index linesAbove) to w -- no above/below context -- for
// external tools that just want "what's playing right now" as a single
// line (e.g. a single-line status bar segment).
func PrintExact(client *mpdclient.Client, musicDir string, w io.Writer) error {
	window, err := currentWindow(client, musicDir)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, window[linesAbove])
	return err
}
