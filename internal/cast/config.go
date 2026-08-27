package cast

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultDiscoveryTimeout = 3 * time.Second

// Config is everything casting reads from mpdtui's own settings file plus
// a few values passed in by the caller. This package owns its config
// keys -- internal/config has no getters for them; LoadConfig parses the
// same "key = value" file directly, matching that package's deliberately
// minimal format.
type Config struct {
	// Home Assistant. Both must be set for the HA provider to exist.
	HABaseURL string
	HAToken   string

	// Stream-URL derivation. Only needed when the automatic guess
	// (MPD host + httpd port) is wrong -- e.g. MPD on localhost with a
	// LAN device, or httpd behind a different address.
	StreamHost string // host/IP the device should connect to
	StreamURL  string // full override; wins over everything
	HTTPDPort  string // port when the httpd output doesn't report one

	// Exclusive disables MPD's other (local) outputs while casting, so
	// audio comes out only the cast device. Default false: local
	// speakers keep working, at the cost of playing in both places.
	Exclusive bool

	DiscoveryTimeout time.Duration

	// MPDHost is the host mpdtui dialed MPD on, the default stream host.
	// Passed in by the caller rather than re-resolved here.
	MPDHost string

	// StatePath is where the active-cast session is persisted. Defaults
	// to <cache>/mpdtui/cast-session.json.
	StatePath string
}

// LoadConfig reads casting settings from ~/.config/mpdtui/config (via the
// same XDG resolution and minimal parser internal/config uses) and fills
// in defaults. mpdHost is the address mpdtui dialed MPD on.
func LoadConfig(mpdHost string) Config {
	v := loadConfigValues()
	cfg := Config{
		HABaseURL:        strings.TrimRight(v["ha_url"], "/"),
		HAToken:          v["ha_token"],
		StreamHost:       v["cast_stream_host"],
		StreamURL:        strings.TrimRight(v["cast_stream_url"], "/"),
		HTTPDPort:        v["httpd_port"],
		Exclusive:        v["cast_exclusive"] == "true",
		DiscoveryTimeout: defaultDiscoveryTimeout,
		MPDHost:          mpdHost,
		StatePath:        defaultStatePath(),
	}
	if secs, err := strconv.Atoi(v["cast_discovery_timeout"]); err == nil && secs > 0 {
		cfg.DiscoveryTimeout = time.Duration(secs) * time.Second
	}
	return cfg
}

// configValues parses the mpdtui config file into a key->value map. A
// copy of internal/config.loadConfigValues' logic (not exported there);
// duplicated rather than widening that package's API for keys it doesn't
// own.
func loadConfigValues() map[string]string {
	values := map[string]string{}
	path := configFilePath()
	if path == "" {
		return values
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return values
}

func configFilePath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "mpdtui", "config")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "mpdtui", "config")
}

func defaultStatePath() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".cache")
		}
	}
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "mpdtui", "cast-session.json")
}
