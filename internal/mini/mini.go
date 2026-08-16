// Package mini implements mpdtui's lightweight inline player: a
// bordered block of live-updating status lines, no alt-screen, driven
// by raw terminal input.
package mini

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"

	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

const tickInterval = 500 * time.Millisecond

// miniBoxMinWidth/miniBoxMaxWidth clamp the box's total width (borders
// included): never so narrow the content gets crushed, never so wide it
// stretches across a huge terminal -- this mode is for a quick glance or
// a tmux status pane, not to fill the screen.
const (
	miniBoxMinWidth = 40
	miniBoxMaxWidth = 78
)

// Run starts the inline player and blocks until the user quits (q,
// Ctrl-C, or SIGINT/SIGTERM), restoring the terminal before returning.
// metaDB enables the local rating/play-count/mark row and the '1'-'5'
// rate-the-current-track keybinding when non-nil; pass nil to leave both
// entirely inactive, same "off means off" convention
// config.LoadTrackMetadataEnabled already establishes for the full panel
// UI.
func Run(client *mpdclient.Client, metaDB *metadata.DB) error {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return fmt.Errorf("mini mode requires an interactive terminal on stdin")
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("enable raw terminal mode: %w", err)
	}
	defer term.Restore(fd, oldState)
	defer fmt.Print("\r\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	keys := make(chan byte)
	go readKeys(keys)

	var events <-chan string
	var watchErrs <-chan error
	if w, err := client.Watch("player", "mixer", "options", "stored_playlist"); err == nil {
		events = w.Events()
		watchErrs = w.Errors()
		defer w.Close()
	}
	// A failed Watch isn't fatal for the mini mode: the ticker below
	// keeps the lines refreshed either way, just without instant updates.

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	out := &block{}
	playlistCount := fetchPlaylistCount(client)
	// playCountedSongID mirrors App.playCountedSongID's own -1-means-
	// none convention (internal/ui/trackmetadata.go): the queue song id
	// already counted this session, so ticking past the halfway point on
	// every redraw doesn't inflate the count.
	playCountedSongID := -1
	redraw := func() { render(out, client, metaDB, playlistCount, &playCountedSongID) }

	redraw()
	for {
		select {
		case <-sigCh:
			return nil
		case b, ok := <-keys:
			if !ok {
				return nil
			}
			if handleKey(client, metaDB, b) {
				return nil
			}
			redraw()
		case name, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if name == "stored_playlist" {
				playlistCount = fetchPlaylistCount(client)
			}
			redraw()
		case _, ok := <-watchErrs:
			if !ok {
				watchErrs = nil
				continue
			}
			events, watchErrs = nil, nil
		case <-ticker.C:
			redraw()
		}
	}
}

func fetchPlaylistCount(client *mpdclient.Client) int {
	pls, err := client.Playlists()
	if err != nil {
		return 0
	}
	return len(pls)
}

// readKeys reads raw bytes from stdin one at a time and forwards them,
// closing the channel on read error (e.g. stdin closed).
func readKeys(out chan<- byte) {
	defer close(out)
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			out <- buf[0]
		}
		if err != nil {
			return
		}
	}
}

// handleKey applies a keypress and reports whether it should quit.
func handleKey(client *mpdclient.Client, metaDB *metadata.DB, b byte) bool {
	switch b {
	case ' ':
		client.TogglePlayPause()
	case 's':
		client.Stop()
	case 'n':
		client.Next()
	case 'p':
		client.Previous()
	case '-':
		client.ChangeVolume(-5)
	case '=':
		// Same physical key as '+' on a US layout, without needing
		// shift -- matches '-' also needing no modifier.
		client.ChangeVolume(5)
	case '1', '2', '3', '4', '5':
		rateCurrentTrack(client, metaDB, int(b-'0'))
	case 'q', 3: // 3 = Ctrl-C
		return true
	}
	return false
}

// rateCurrentTrack rates whatever's currently playing -- mini mode has
// no separate "selected" track the way the full panel UI's Queue does
// (see App.handleRateSelectedTrack), so the only track it can sensibly
// act on is the one actually playing. A no-op if the track-metadata
// feature isn't active or nothing's currently playing; errors are
// swallowed rather than surfaced, same "keep the display line, don't
// interrupt playback for a bookkeeping write" spirit as the rest of this
// package's error handling.
func rateCurrentTrack(client *mpdclient.Client, metaDB *metadata.DB, rating int) {
	if metaDB == nil {
		return
	}
	song, err := client.CurrentSong()
	if err != nil || song.File == "" {
		return
	}
	_ = metaDB.Rate(song.File, rating)
}

// maybeTrackPlayCount mirrors internal/ui's own identical logic
// (App.maybeTrackPlayCount in trackmetadata.go) -- duplicated, not
// imported, same leaf-package reasoning as everywhere else this package
// avoids depending on internal/ui. Increments the currently playing
// track's local play count once it's played at least halfway through,
// at most once per distinct queue song id (*playCountedSongID, -1
// meaning none counted yet this session).
func maybeTrackPlayCount(metaDB *metadata.DB, st mpdclient.Status, song mpdclient.Song, playCountedSongID *int) {
	if metaDB == nil || st.SongID < 0 || song.File == "" {
		return
	}
	if st.SongID == *playCountedSongID {
		return
	}
	if st.Duration <= 0 || st.Elapsed*2 < st.Duration {
		return
	}
	*playCountedSongID = st.SongID
	_ = metaDB.IncrementPlayCount(song.File)
}

// block tracks how many lines were last drawn in place, so the next
// render can move the cursor back to the top of the block before
// overwriting it -- printing more or fewer lines than last time (e.g.
// an error line replacing the normal boxed output, or the metadata row
// appearing/disappearing) is handled correctly either way.
type block struct {
	lines int
}

func (b *block) print(lines []string) {
	if b.lines > 1 {
		fmt.Printf("\x1b[%dA", b.lines-1)
	}
	if b.lines > 0 {
		fmt.Print("\r")
	}
	for i, l := range lines {
		fmt.Print("\x1b[K" + l)
		if i < len(lines)-1 {
			fmt.Print("\r\n")
		}
	}
	b.lines = len(lines)
}

// boxTotalWidth picks the box's total width (borders included) from the
// current terminal width, queried fresh on every render since it can
// change (a resized terminal, or a tmux pane growing/shrinking).
// Falls back to the max when the size can't be determined (e.g. stdout
// redirected to a file/pipe while stdin is still a real terminal).
func boxTotalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		w = miniBoxMaxWidth
	}
	if w > miniBoxMaxWidth {
		w = miniBoxMaxWidth
	}
	if w < miniBoxMinWidth {
		w = miniBoxMinWidth
	}
	return w
}

func render(out *block, client *mpdclient.Client, metaDB *metadata.DB, playlistCount int, playCountedSongID *int) {
	st, err := client.Status()
	if err != nil {
		out.print([]string{"mpdtui: " + err.Error()})
		return
	}
	song, err := client.CurrentSong()
	if err != nil {
		out.print([]string{"mpdtui: " + err.Error()})
		return
	}

	lines := []string{statsLine(st, playlistCount), nowPlayingLine(st, song), progressLine(st)}
	if metaDB != nil {
		maybeTrackPlayCount(metaDB, st, song, playCountedSongID)
		if song.File != "" {
			// Get returns a zero-opinion Track (not an error) for a file
			// with no row yet -- errors here mean the database itself is
			// unavailable, in which case the row just shows the same
			// zero-opinion defaults rather than an error line breaking
			// the box.
			meta, _ := metaDB.Get(song.File)
			lines = append(lines, metaLine(meta))
		}
	}

	out.print(box(lines, boxTotalWidth()-4))
}

func statsLine(st mpdclient.Status, playlistCount int) string {
	return fmt.Sprintf("%d track(s) in queue  ·  %d playlist(s)", st.PlaylistLength, playlistCount)
}

func stateGlyph(state mpdclient.State) string {
	switch state {
	case mpdclient.StatePlay:
		return ">"
	case mpdclient.StatePause:
		return "||"
	case mpdclient.StateStop:
		return "[]"
	default:
		return "?"
	}
}

func nowPlayingLine(st mpdclient.Status, song mpdclient.Song) string {
	track := song.DisplayName()
	if track == "" {
		track = "(nothing playing)"
	}
	return stateGlyph(st.State) + " " + track
}

func progressLine(st mpdclient.Status) string {
	bar := progressBar(st.Elapsed, st.Duration, 12)
	vol := "?"
	if st.Volume >= 0 {
		vol = fmt.Sprintf("%d", st.Volume)
	}
	return fmt.Sprintf("[%s] %s/%s  vol %s%%", bar, formatDuration(st.Elapsed), formatDuration(st.Duration), vol)
}

// metaLine shows the currently playing track's local rating and play
// count (and its mark, if any) -- only ever called when metaDB is
// active and something's actually playing (see render).
func metaLine(meta metadata.Track) string {
	l := fmt.Sprintf("%s  played %dx", ratingStars(meta.Rating), meta.PlayCount)
	if meta.Mark != nil {
		l += "  marked: " + meta.Mark.Reason
	}
	return l
}
