// Package mini implements mpdtui's lightweight inline player: a single
// live-updating status line, no alt-screen, driven by raw terminal input.
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
	if w, err := client.Watch("player", "mixer", "options"); err == nil {
		events = w.Events()
		watchErrs = w.Errors()
		defer w.Close()
	}
	// A failed Watch isn't fatal for the mini mode: the ticker below
	// keeps the line refreshed either way, just without instant updates.

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	render(client)
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
			render(client)
		case _, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			render(client)
		case _, ok := <-watchErrs:
			if !ok {
				watchErrs = nil
				continue
			}
			events, watchErrs = nil, nil
		case <-ticker.C:
			render(client)
		}
	}
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

func render(client *mpdclient.Client) {
	st, err := client.Status()
	if err != nil {
		fmt.Print("\r\x1b[K" + "mpdtui: " + err.Error())
		return
	}
	song, err := client.CurrentSong()
	if err != nil {
		fmt.Print("\r\x1b[K" + "mpdtui: " + err.Error())
		return
	}
	fmt.Print("\r\x1b[K" + line(st, song))
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
