package config

import (
	"os"
	"path/filepath"
	"strings"
)

// ConfigDir returns mpdtui's own local settings directory:
// $XDG_CONFIG_HOME/mpdtui if $XDG_CONFIG_HOME is set (the XDG Base
// Directory convention most Linux tools follow), otherwise
// ~/.config/mpdtui. Returns "" if neither $XDG_CONFIG_HOME nor the home
// directory can be determined.
func ConfigDir() string {
	if dir := envOr("XDG_CONFIG_HOME", ""); dir != "" {
		return filepath.Join(dir, "mpdtui")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "mpdtui")
}

// ConfigFile is the path to mpdtui's own local settings file (music_dir,
// track_metadata -- see LoadMusicDir/LoadTrackMetadataEnabled), inside
// ConfigDir.
func ConfigFile() string {
	dir := ConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config")
}

// DBFile is the path to mpdtui's local track-metadata SQLite database
// (see internal/metadata), inside ConfigDir -- next to ConfigFile, never
// outside it.
func DBFile() string {
	dir := ConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "mpdtui.db")
}

// loadConfigValues parses ConfigFile into a key->value map, or nil if it
// doesn't exist or can't be read. Shared by every LoadX function in this
// file so the file is only ever parsed once per key looked up, not once
// per setting.
//
// The file format is deliberately minimal ("key = value" lines, "#"
// comments, blank lines ignored) rather than TOML/YAML/JSON: pulling in a
// config-format dependency for a handful of settings felt like the wrong
// tradeoff for a tool that otherwise has none.
func loadConfigValues() map[string]string {
	path := ConfigFile()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}

// LoadMusicDir reads music_dir from ConfigFile -- the local filesystem
// path that mirrors MPD's own music_directory, needed by internal/lyrics
// to find lyrics files (MPD's protocol has no command to serve an
// arbitrary sidecar file's content, unlike album art). Returns "" --
// meaning the lyrics feature stays inactive, not an error -- for any of:
// the config file doesn't exist or can't be read, it has no music_dir
// line, or music_dir names a path that doesn't exist or isn't a
// directory (a stale config, a typo, an unmounted drive). That last check
// is deliberate: every other consumer of this value (the Queue's Lyr
// column, the lyrics viewer) treats "" as the single source of truth for
// "inactive", so a configured-but-broken path has to resolve to "" here
// rather than leaking a path nothing can actually read into the rest of
// the app.
//
// A leading "~/" in the value is expanded against the home directory,
// since path config values are commonly written that way.
func LoadMusicDir() string {
	value, ok := loadConfigValues()["music_dir"]
	if !ok {
		return ""
	}
	dir := expandHome(value)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// LoadTrackMetadataEnabled reads track_metadata from ConfigFile -- the
// activation flag for internal/metadata's local play-count/rating/mark/
// tags database. Off (false) unless the config file has a line reading
// exactly "track_metadata = true": missing file, missing key, or any
// other value all mean inactive, matching LoadMusicDir's own "absent
// config is a normal, supported state" convention. Deliberately opt-in
// rather than on-by-default -- this feature writes a local database file
// on every play/pause tick once active, which isn't something to turn on
// without the user asking for it.
func LoadTrackMetadataEnabled() bool {
	return loadConfigValues()["track_metadata"] == "true"
}

// expandHome replaces a leading "~/" in path with the user's home
// directory, unchanged if there's no such prefix or the home directory
// can't be determined.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}
