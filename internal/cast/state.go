package cast

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// staleSessionAge is how old a persisted session may be before a
// re-attach ignores it outright -- a device left casting for a day is
// almost certainly not something the user still wants mpdtui to adopt,
// and the risk of a stale (Kind,ID) matching a now-different device
// isn't worth it.
const staleSessionAge = 24 * time.Hour

// Session is the active cast: which device, what stream URL it was given,
// and the full pre-cast output state so Stop can restore MPD exactly.
// Persisted to disk (Config.StatePath) so `-cast-stop` and the next
// mpdtui launch can find a cast started by a since-closed process.
type Session struct {
	Target        Target        `json:"target"`
	StreamURL     string        `json:"stream_url"`
	HTTPDOutputID int           `json:"httpd_output_id"`
	PriorOutputs  []OutputState `json:"prior_outputs"`
	StartedAt     time.Time     `json:"started_at"`
	PID           int           `json:"pid"`
}

// OutputState is one MPD output's enabled flag at cast-start time.
type OutputState struct {
	ID      int  `json:"id"`
	Enabled bool `json:"enabled"`
}

func readState(path string) (*Session, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// writeState persists s atomically (temp file + rename) so a crash mid-
// write can't leave a truncated session file.
func writeState(path string, s *Session) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func clearState(path string) error {
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Session) stale() bool {
	return s == nil || now().Sub(s.StartedAt) > staleSessionAge
}
