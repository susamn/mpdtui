// Package mini implements mpdtui's lightweight inline player: a couple
// of live-updating status lines, no alt-screen, driven by raw terminal
// input.
package mini

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"

	"mpdtui/internal/mpdclient"
)

const tickInterval = 500 * time.Millisecond

// Run starts the inline player and blocks until the user quits (q,
// Ctrl-C, or SIGINT/SIGTERM), restoring the terminal before returning.
func Run(client *mpdclient.Client) error {
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
	redraw := func() { render(out, client, playlistCount) }

	redraw()
	for {
		select {
		case <-sigCh:
			return nil
		case b, ok := <-keys:
			if !ok {
				return nil
			}
			if handleKey(client, b) {
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
func handleKey(client *mpdclient.Client, b byte) bool {
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
	case 'q', 3: // 3 = Ctrl-C
		return true
	}
	return false
}

// block tracks how many lines were last drawn in place, so the next
// render can move the cursor back to the top of the block before
// overwriting it -- printing more or fewer lines than last time (e.g.
// an error line replacing the normal two) is handled correctly either
// way.
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

func render(out *block, client *mpdclient.Client, playlistCount int) {
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
	out.print([]string{statsLine(st, playlistCount), line(st, song)})
}

func statsLine(st mpdclient.Status, playlistCount int) string {
	return fmt.Sprintf("%d track(s) in queue  ·  %d playlist(s)", st.PlaylistLength, playlistCount)
}

func line(st mpdclient.Status, song mpdclient.Song) string {
	track := song.DisplayName()
	if track == "" {
		track = "(nothing playing)"
	}
	glyph := "?"
	switch st.State {
	case mpdclient.StatePlay:
		glyph = ">"
	case mpdclient.StatePause:
		glyph = "||"
	case mpdclient.StateStop:
		glyph = "[]"
	}
	bar := progressBar(st.Elapsed, st.Duration, 12)
	vol := "?"
	if st.Volume >= 0 {
		vol = fmt.Sprintf("%d", st.Volume)
	}
	return fmt.Sprintf("%s %s  [%s] %s/%s  vol %s%%",
		glyph, track, bar, formatDuration(st.Elapsed), formatDuration(st.Duration), vol)
}
