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

// miniContentMinWidth/miniContentMaxWidth clamp the box's *content*
// width (borders excluded): never so narrow the content gets crushed,
// never so wide a long track title balloons the box. The box itself is
// sized to its actual content within that range (see contentWidthFor),
// not stretched to fill the terminal -- a short "N track(s)..." line
// otherwise left a lot of empty space on the right when the terminal
// was wide.
const (
	miniContentMinWidth = 30
	miniContentMaxWidth = 70
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

// playCountRearmElapsed mirrors internal/ui's own constant of the same
// name (trackmetadata.go) -- see its doc comment for why a SongID
// already counted re-arms once Elapsed is observed back near the start.
const playCountRearmElapsed = 3 * time.Second

// maybeTrackPlayCount mirrors internal/ui's own identical logic
// (App.maybeTrackPlayCount in trackmetadata.go) -- duplicated, not
// imported, same leaf-package reasoning as everywhere else this package
// avoids depending on internal/ui. Increments the currently playing
// track's local play count once it's played at least halfway through,
// at most once per distinct queue song id (*playCountedSongID, -1
// meaning none counted yet this session) -- unless that same SongID
// restarted from the beginning (repeat-mode loop, or replaying the same
// still-queued entry), which re-arms it.
func maybeTrackPlayCount(metaDB *metadata.DB, st mpdclient.Status, song mpdclient.Song, playCountedSongID *int) {
	if metaDB == nil || st.SongID < 0 || song.File == "" {
		return
	}
	if st.SongID == *playCountedSongID {
		if st.Elapsed >= playCountRearmElapsed {
			return
		}
		*playCountedSongID = -1
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

// contentWidthFor picks the box's content width (borders excluded) from
// the actual lines about to be shown: the widest one's own plain width,
// clamped to [miniContentMinWidth, miniContentMaxWidth] and never wider
// than the real terminal can show (queried fresh every render, since it
// can change -- a resized terminal, or a tmux pane growing/shrinking).
// Sizing to content rather than always maximizing to the terminal width
// is what keeps a short status line from leaving a lot of empty space
// on the right in a wide terminal.
func contentWidthFor(lines [][]segment) int {
	w := 0
	for _, segs := range lines {
		if pw := plainWidth(segs); pw > w {
			w = pw
		}
	}
	if w < miniContentMinWidth {
		w = miniContentMinWidth
	}
	if w > miniContentMaxWidth {
		w = miniContentMaxWidth
	}
	if termWidth, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && termWidth > 0 {
		if maxFromTerm := termWidth - 4; maxFromTerm < w {
			w = maxFromTerm
		}
	}
	if w < 1 {
		w = 1
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

	lineSegs := [][]segment{statsSegments(st, playlistCount), nowPlayingSegments(st, song), progressSegments(st)}
	if metaDB != nil {
		maybeTrackPlayCount(metaDB, st, song, playCountedSongID)
		if song.File != "" {
			// Get returns a zero-opinion Track (not an error) for a file
			// with no row yet -- errors here mean the database itself is
			// unavailable, in which case the row just shows the same
			// zero-opinion defaults rather than an error line breaking
			// the box.
			meta, _ := metaDB.Get(song.File)
			lineSegs = append(lineSegs, metaSegments(meta))
		}
	}

	width := contentWidthFor(lineSegs)
	lines := make([]string, len(lineSegs))
	for i, segs := range lineSegs {
		lines[i] = renderLine(segs, width)
	}
	out.print(box(lines, width))
}

// statsSegments is sky blue in full, matching the full panel UI's own
// use of tcell.ColorSkyblue elsewhere (e.g. the Queue's Lyr tick).
func statsSegments(st mpdclient.Status, playlistCount int) []segment {
	text := fmt.Sprintf("%d track(s) in queue  ·  %d playlist(s)", st.PlaylistLength, playlistCount)
	return []segment{{text: text, fg: ansiStatsSkyBlue}}
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

// nowPlayingSegments colors just the track title WhatsApp green
// (matching queueTitleColor), leaving the play-state glyph plain --
// mirrors the full UI, where only the Title cell itself is tinted.
func nowPlayingSegments(st mpdclient.Status, song mpdclient.Song) []segment {
	track := song.DisplayName()
	if track == "" {
		track = "(nothing playing)"
	}
	return []segment{
		{text: stateGlyph(st.State) + " "},
		{text: track, fg: ansiTrackGreen},
	}
}

// progressSegments colors just the bar itself, leaving the brackets and
// the elapsed/duration/volume text plain.
func progressSegments(st mpdclient.Status) []segment {
	bar := progressBar(st.Elapsed, st.Duration, 12)
	vol := "?"
	if st.Volume >= 0 {
		vol = fmt.Sprintf("%d", st.Volume)
	}
	return []segment{
		{text: "["},
		{text: bar, fg: ansiBarCyan},
		{text: fmt.Sprintf("] %s/%s  vol %s%%", formatDuration(st.Elapsed), formatDuration(st.Duration), vol)},
	}
}

// metaSegments shows the currently playing track's local rating (gold,
// matching queueRatingColor) and play count (and its mark, if any, both
// plain) -- only ever called when metaDB is active and something's
// actually playing (see render).
func metaSegments(meta metadata.Track) []segment {
	segs := []segment{
		{text: ratingStars(meta.Rating), fg: ansiRatingGold},
		{text: fmt.Sprintf("  played %dx", meta.PlayCount)},
	}
	if meta.Mark != nil {
		segs = append(segs, segment{text: "  marked: " + meta.Mark.Reason})
	}
	return segs
}
