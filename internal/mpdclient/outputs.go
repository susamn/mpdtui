package mpdclient

import (
	"errors"
	"strconv"
	"strings"

	"github.com/fhs/gompd/v2/mpd"
)

// ErrNoHTTPDOutput is returned by HTTPDOutput when MPD has no httpd audio
// output configured -- the one thing casting (see internal/cast) can't
// work around, since it needs an HTTP stream for the target device to
// pull. errors.Is-able so callers can render their own "add this to
// mpd.conf" guidance rather than leaking a bare error string.
var ErrNoHTTPDOutput = errors.New("no httpd audio output configured in MPD")

// Output is one of MPD's configured audio outputs (an `outputs` row),
// with the raw protocol attributes flattened into typed fields. Attrs
// holds any per-plugin `attribute: key=value` lines verbatim (e.g. an
// httpd output's "port") for callers that need them.
type Output struct {
	ID      int
	Name    string
	Enabled bool
	Plugin  string
	Attrs   map[string]string
}

// Outputs lists MPD's configured audio outputs.
func (c *Client) Outputs() ([]Output, error) {
	rows, err := call(c, func(conn *mpd.Client) ([]mpd.Attrs, error) { return conn.ListOutputs() })
	if err != nil {
		return nil, err
	}
	return parseOutputs(rows), nil
}

// HTTPDOutput returns the first httpd-plugin output, or ErrNoHTTPDOutput
// if there is none. This is the output casting enables to produce the
// stream a Chromecast / DLNA / Home Assistant player pulls from.
func (c *Client) HTTPDOutput() (Output, error) {
	outs, err := c.Outputs()
	if err != nil {
		return Output{}, err
	}
	for _, o := range outs {
		if o.Plugin == "httpd" {
			return o, nil
		}
	}
	return Output{}, ErrNoHTTPDOutput
}

// EnableOutput enables the audio output with the given id.
func (c *Client) EnableOutput(id int) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.EnableOutput(id) })
}

// DisableOutput disables the audio output with the given id.
func (c *Client) DisableOutput(id int) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.DisableOutput(id) })
}

// parseOutputs turns MPD's `outputs` attribute rows (already split on
// outputid by gompd's AttrsList) into typed Output values. Rows without a
// parseable outputid are skipped. outputenabled is MPD's "0"/"1";
// anything other than "1" is treated as disabled.
func parseOutputs(rows []mpd.Attrs) []Output {
	outs := make([]Output, 0, len(rows))
	for _, a := range rows {
		id, err := strconv.Atoi(a["outputid"])
		if err != nil {
			continue
		}
		o := Output{
			ID:      id,
			Name:    a["outputname"],
			Enabled: a["outputenabled"] == "1",
			Plugin:  a["plugin"],
		}
		if raw, ok := a["attribute"]; ok {
			if k, v, found := strings.Cut(raw, "="); found {
				o.Attrs = map[string]string{strings.TrimSpace(k): strings.TrimSpace(v)}
			}
		}
		outs = append(outs, o)
	}
	return outs
}
