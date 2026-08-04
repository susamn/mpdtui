// Package mpdclient wraps gompd/v2 behind domain types and typed methods
// so callers never see raw MPD protocol attributes.
package mpdclient

import (
	"sync"

	"github.com/fhs/gompd/v2/mpd"

	"mpdtui/internal/config"
)

const network = "tcp"

// Client is a reconnecting MPD command connection.
type Client struct {
	addr     string
	password string

	mu   sync.Mutex
	conn *mpd.Client
}

// Dial connects to the MPD server described by cfg.
func Dial(cfg config.Config) (*Client, error) {
	c := &Client{addr: cfg.Addr(), password: cfg.Password}
	if err := c.connect(); err != nil {
		return nil, err
	}
	return c, nil
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) connect() error {
	conn, err := mpd.DialAuthenticated(network, c.addr, c.password)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

// call runs fn against the live connection, reconnecting and retrying once
// on failure. A long-lived TUI session can outlive a single TCP connection
// (MPD or the network in between can drop it), so a single retry is worth
// the complexity; anything past that surfaces to the caller as an error.
func call[T any](c *Client, fn func(*mpd.Client) (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	v, err := fn(c.conn)
	if err == nil {
		return v, nil
	}
	if cerr := c.connect(); cerr != nil {
		var zero T
		return zero, err
	}
	return fn(c.conn)
}

func callErr(c *Client, fn func(*mpd.Client) error) error {
	_, err := call(c, func(conn *mpd.Client) (struct{}, error) {
		return struct{}{}, fn(conn)
	})
	return err
}
