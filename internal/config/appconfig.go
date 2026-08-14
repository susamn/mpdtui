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

// ConfigFile is the path to mpdtui's own local settings file (currently
// just music_dir, see LoadMusicDir), inside ConfigDir.
func ConfigFile() string {
	dir := ConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config")
}

// LoadMusicDir reads music_dir from ConfigFile -- the local filesystem
// path that mirrors MPD's own music_directory, needed by internal/lyrics
// to find lyrics files (MPD's protocol has no command to serve an
// arbitrary sidecar file's content, unlike album art). Returns "" if the
// file doesn't exist, can't be read, or has no music_dir line -- the
// lyrics feature just stays inactive in that case, not an error, since
// most users won't have this set up.
//
// The file format is deliberately minimal ("key = value" lines, "#"
// comments, blank lines ignored) rather than TOML/YAML/JSON: this is a
// single-setting file today, and pulling in a config-format dependency
// for that felt like the wrong tradeoff for a tool that has zero such
// dependencies otherwise. A leading "~/" in the value is expanded against
// the home directory, since path config values are commonly written that
// way.
func LoadMusicDir() string {
	path := ConfigFile()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "music_dir" {
			continue
		}
		return expandHome(strings.TrimSpace(value))
	}
	return ""
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
