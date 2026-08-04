package mpdclient

import "github.com/fhs/gompd/v2/mpd"

// Watcher reports MPD subsystem changes as they happen, via MPD's idle
// command. Subsystem names: "player", "mixer", "playlist", "options",
// "stored_playlist" (see the MPD protocol docs for the full list).
type Watcher struct {
	w *mpd.Watcher
}

// Watch opens a dedicated idle connection watching the given subsystems
// (all subsystems if none given).
func (c *Client) Watch(subsystems ...string) (*Watcher, error) {
	w, err := mpd.NewWatcher(network, c.addr, c.password, subsystems...)
	if err != nil {
		return nil, err
	}
	return &Watcher{w: w}, nil
}

// Events yields the subsystem name for each change as it happens.
func (w *Watcher) Events() <-chan string { return w.w.Event }

// Errors yields errors from the underlying idle connection (e.g. if it
// drops); the watcher is no longer usable after one arrives.
func (w *Watcher) Errors() <-chan error { return w.w.Error }

// Close stops watching and closes the idle connection.
func (w *Watcher) Close() error { return w.w.Close() }
