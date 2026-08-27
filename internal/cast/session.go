package cast

import (
	"context"
	"fmt"
	"os"

	"mpdtui/internal/mpdclient"
)

// StartCast points t at MPD's httpd stream and records the session.
//
// Order matters: the output state is snapshotted and the httpd output
// enabled (and, with Exclusive, the others disabled) and the session
// persisted BEFORE the device is told to play, so a crash between the two
// still leaves a state file `-cast-stop` can clean up. If the device
// rejects the stream, the output changes are rolled back.
func (m *Manager) StartCast(ctx context.Context, t Target, meta MediaMeta) (*Session, error) {
	provider := m.providerFor(t.Kind)
	if provider == nil {
		return nil, fmt.Errorf("no provider for %s", t.Kind)
	}

	// If already casting elsewhere, tear that down first (restores outputs).
	if cur := m.Session(); cur != nil {
		if cur.Target.Kind == t.Kind && cur.Target.ID == t.ID {
			return cur, nil
		}
		if err := m.StopCast(ctx); err != nil {
			return nil, fmt.Errorf("stopping previous cast: %w", err)
		}
	}

	httpd, err := m.mpd.HTTPDOutput()
	if err != nil {
		return nil, err
	}
	streamURL, err := deriveStreamURL(m.cfg, httpd)
	if err != nil {
		return nil, err
	}

	outs, err := m.mpd.Outputs()
	if err != nil {
		return nil, err
	}
	prior := make([]OutputState, len(outs))
	for i, o := range outs {
		prior[i] = OutputState{ID: o.ID, Enabled: o.Enabled}
	}

	sess := &Session{
		Target:        t,
		StreamURL:     streamURL,
		HTTPDOutputID: httpd.ID,
		PriorOutputs:  prior,
		StartedAt:     now(),
		PID:           os.Getpid(),
	}

	applied := m.applyOutputs(httpd.ID, outs)
	if err := writeState(m.cfg.StatePath, sess); err != nil {
		m.restoreOutputs(prior, applied)
		return nil, fmt.Errorf("persisting cast session: %w", err)
	}

	if err := provider.Play(ctx, t, streamURL, meta); err != nil {
		m.restoreOutputs(prior, applied)
		_ = clearState(m.cfg.StatePath)
		return nil, fmt.Errorf("starting playback on %s: %w", t.Name, err)
	}

	m.setSession(sess)
	return sess, nil
}

// applyOutputs enables the httpd output and, when Exclusive, disables
// every other currently-enabled output. Returns the set of output IDs it
// changed, for rollback.
func (m *Manager) applyOutputs(httpdID int, outs []mpdclient.Output) map[int]bool {
	changed := map[int]bool{}
	for _, o := range outs {
		switch {
		case o.ID == httpdID && !o.Enabled:
			if m.mpd.EnableOutput(o.ID) == nil {
				changed[o.ID] = true
			}
		case o.ID != httpdID && o.Enabled && m.cfg.Exclusive:
			if m.mpd.DisableOutput(o.ID) == nil {
				changed[o.ID] = true
			}
		}
	}
	return changed
}

// restoreOutputs puts every output listed in prior back to its recorded
// state, but only those the cast actually changed (or, on a best-effort
// stop where changed is nil, all of them).
func (m *Manager) restoreOutputs(prior []OutputState, changed map[int]bool) {
	for _, p := range prior {
		if changed != nil && !changed[p.ID] {
			continue
		}
		if p.Enabled {
			_ = m.mpd.EnableOutput(p.ID)
		} else {
			_ = m.mpd.DisableOutput(p.ID)
		}
	}
}

// StopCast stops the device and restores MPD's outputs. Works whether the
// session is in memory (this process started it) or only on disk (a
// since-closed process did). Returns ErrNotCasting if neither exists and
// no device is discoverably playing our stream.
func (m *Manager) StopCast(ctx context.Context) error {
	sess := m.Session()
	if sess == nil {
		if s, _ := readState(m.cfg.StatePath); s != nil {
			sess = s
		}
	}
	if sess == nil {
		var err error
		sess, err = m.discoverActiveSession(ctx)
		if err != nil {
			return err
		}
	}

	if provider := m.providerFor(sess.Target.Kind); provider != nil {
		_ = provider.Stop(ctx, sess.Target)
	}
	m.restoreOutputs(sess.PriorOutputs, nil)
	m.setSession(nil)
	return clearState(m.cfg.StatePath)
}

// discoverActiveSession is StopCast's last resort when there's no state
// file: discover devices, and if exactly one is playing what looks like
// our httpd stream, synthesize a session to stop it. PriorOutputs is
// unknown here, so restore is "disable httpd, leave the rest".
func (m *Manager) discoverActiveSession(ctx context.Context) (*Session, error) {
	httpd, err := m.mpd.HTTPDOutput()
	if err != nil {
		return nil, ErrNotCasting
	}
	streamURL, err := deriveStreamURL(m.cfg, httpd)
	if err != nil {
		return nil, ErrNotCasting
	}

	var matches []Target
	for _, t := range m.Discover(ctx) {
		p := m.providerFor(t.Kind)
		if p == nil {
			continue
		}
		if nc, err := p.Status(ctx, t); err == nil && nc.Active && nc.MediaURL == streamURL {
			matches = append(matches, t)
		}
	}
	if len(matches) != 1 {
		return nil, ErrNotCasting
	}
	return &Session{
		Target:        matches[0],
		StreamURL:     streamURL,
		HTTPDOutputID: httpd.ID,
		PriorOutputs:  []OutputState{{ID: httpd.ID, Enabled: false}},
		StartedAt:     now(),
	}, nil
}

// Reattach is called at startup: if a persisted session's device is still
// playing our stream, adopt it so the indicator and "Stop casting" work.
// Anything stale, mismatched, or gone is cleared.
func (m *Manager) Reattach(ctx context.Context) (*Session, error) {
	sess, err := readState(m.cfg.StatePath)
	if err != nil || sess == nil {
		return nil, err
	}
	if sess.stale() {
		_ = clearState(m.cfg.StatePath)
		return nil, nil
	}

	provider := m.providerFor(sess.Target.Kind)
	if provider == nil {
		_ = clearState(m.cfg.StatePath)
		return nil, nil
	}

	nc, err := provider.Status(ctx, sess.Target)
	if err != nil || !nc.Active || nc.MediaURL != sess.StreamURL {
		_ = clearState(m.cfg.StatePath)
		return nil, nil
	}

	m.setSession(sess)
	return sess, nil
}
