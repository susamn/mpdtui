package mpdclient

import "github.com/fhs/gompd/v2/mpd"

// Queue returns the current playback queue in order.
func (c *Client) Queue() ([]Song, error) {
	list, err := call(c, func(conn *mpd.Client) ([]mpd.Attrs, error) { return conn.PlaylistInfo(-1, -1) })
	if err != nil {
		return nil, err
	}
	return parseSongs(list), nil
}

// QueueAdd appends uri (a file or directory) to the queue.
func (c *Client) QueueAdd(uri string) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.Add(uri) })
}

// QueueAddID appends uri to the queue and returns its new queue id.
func (c *Client) QueueAddID(uri string) (int, error) {
	return call(c, func(conn *mpd.Client) (int, error) { return conn.AddID(uri, -1) })
}

// QueueRemoveID removes the song identified by id from the queue.
func (c *Client) QueueRemoveID(id int) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.DeleteID(id) })
}

// QueueMoveID moves the song identified by id to queue position pos.
func (c *Client) QueueMoveID(id, pos int) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.MoveID(id, pos) })
}

// QueueClear removes every song from the queue.
func (c *Client) QueueClear() error {
	return callErr(c, func(conn *mpd.Client) error { return conn.Clear() })
}
