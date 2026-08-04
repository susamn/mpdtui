package mpdclient

import (
	"time"

	"github.com/fhs/gompd/v2/mpd"
)

// Status returns MPD's current playback status.
func (c *Client) Status() (Status, error) {
	a, err := call(c, func(conn *mpd.Client) (mpd.Attrs, error) { return conn.Status() })
	if err != nil {
		return Status{}, err
	}
	return parseStatus(a), nil
}

// CurrentSong returns the currently playing/selected song. Zero Song if
// the queue is empty.
func (c *Client) CurrentSong() (Song, error) {
	a, err := call(c, func(conn *mpd.Client) (mpd.Attrs, error) { return conn.CurrentSong() })
	if err != nil {
		return Song{}, err
	}
	return parseSong(a), nil
}

// PlayID starts playing the queue song identified by id.
func (c *Client) PlayID(id int) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.PlayID(id) })
}

// Resume resumes playback at the current queue position.
func (c *Client) Resume() error {
	return callErr(c, func(conn *mpd.Client) error { return conn.Play(-1) })
}

// Pause pauses playback if pause is true, resumes otherwise.
func (c *Client) Pause(pause bool) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.Pause(pause) })
}

// TogglePlayPause pauses if playing, resumes if paused, starts playback
// if stopped.
func (c *Client) TogglePlayPause() error {
	st, err := c.Status()
	if err != nil {
		return err
	}
	switch st.State {
	case StatePlay:
		return c.Pause(true)
	case StatePause:
		return c.Pause(false)
	default:
		return c.Resume()
	}
}

// Stop stops playback.
func (c *Client) Stop() error {
	return callErr(c, func(conn *mpd.Client) error { return conn.Stop() })
}

// Next skips to the next song in the queue.
func (c *Client) Next() error {
	return callErr(c, func(conn *mpd.Client) error { return conn.Next() })
}

// Previous skips to the previous song in the queue.
func (c *Client) Previous() error {
	return callErr(c, func(conn *mpd.Client) error { return conn.Previous() })
}

// SeekCur seeks within the current song by d. If relative is true, d is
// relative to the current position (may be negative).
func (c *Client) SeekCur(d time.Duration, relative bool) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.SeekCur(d, relative) })
}

// SetVolume sets absolute volume, clamped to [0, 100].
func (c *Client) SetVolume(v int) error {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return callErr(c, func(conn *mpd.Client) error { return conn.SetVolume(v) })
}

// ChangeVolume adjusts volume by delta (may be negative), clamped to
// [0, 100].
func (c *Client) ChangeVolume(delta int) error {
	st, err := c.Status()
	if err != nil {
		return err
	}
	return c.SetVolume(st.Volume + delta)
}

// SetRandom toggles shuffle/random playback order.
func (c *Client) SetRandom(on bool) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.Random(on) })
}

// SetRepeat toggles repeat mode.
func (c *Client) SetRepeat(on bool) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.Repeat(on) })
}

// SetSingle toggles single-song mode.
func (c *Client) SetSingle(on bool) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.Single(on) })
}

// SetConsume toggles consume mode (played songs are removed from queue).
func (c *Client) SetConsume(on bool) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.Consume(on) })
}
