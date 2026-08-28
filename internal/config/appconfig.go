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

// LyricsIndexFile is the path to mpdtui's persistent lyrics search index
// (see internal/lyricsindex), a SQLite file inside ConfigDir next to
// ConfigFile and DBFile. Returns "" when ConfigDir can't be determined,
// same as the others -- the lyrics-index feature is then simply
// unavailable, not an error.
func LyricsIndexFile() string {
	dir := ConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "lyrics_index.db")
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

// DefaultColorsFile is the path to mpdtui's own default color file
// (see internal/theme.Serialize/Default), inside ConfigDir --
// EnsureConfigFiles creates it there the first time mpdtui runs, and
// LoadThemeFile points at it whenever theme_file isn't set to something
// else.
func DefaultColorsFile() string {
	dir := ConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "colors.toml")
}

// LoadThemeFile reads theme_file from ConfigFile -- which color file
// internal/theme's own LoadFrom reads from. Always resolves to *some*
// usable path: DefaultColorsFile() if theme_file isn't set (missing
// config file, or a config file with no such line) or ConfigDir() can't
// be determined either, otherwise the configured value resolved via
// resolveThemeFile (relative paths -- what EnsureConfigFiles itself
// writes, "./colors.toml" -- against ConfigDir(); "~/" against the home
// directory; an absolute path used as-is).
//
// This is how every desktop integration points at that path: on
// Omarchy, set theme_file to
// ~/.local/state/omarchy/current/theme/colors.toml (Omarchy's own live
// theme file); on a matugen-driven Hyprland setup (which has no fixed
// output location of its own -- see this repo's README for a matugen
// template that emits a file in DefaultColorsFile()'s own shape), set
// it to wherever that template's output_path writes. Unlike
// LoadMusicDir, the resolved path isn't checked for existence here --
// internal/theme's own LoadFrom already treats a missing/unreadable
// file as "no live theme" and falls back to Default() on its own.
func LoadThemeFile() string {
	if value, ok := loadConfigValues()["theme_file"]; ok && value != "" {
		return resolveThemeFile(value)
	}
	return DefaultColorsFile()
}

// resolveThemeFile resolves theme_file's raw configured value into an
// absolute path: "~/" expands against the home directory (same as
// expandHome); anything else not already absolute is resolved relative
// to ConfigDir() -- so the default "./colors.toml" EnsureConfigFiles
// writes keeps working if ConfigDir() itself ever moves (e.g.
// $XDG_CONFIG_HOME changes), rather than baking in whatever absolute
// path happened to be correct at config-creation time. An already-
// absolute path (leading "/") is returned unchanged.
func resolveThemeFile(value string) string {
	if strings.HasPrefix(value, "~/") {
		return expandHome(value)
	}
	if filepath.IsAbs(value) {
		return value
	}
	dir := ConfigDir()
	if dir == "" {
		return value
	}
	return filepath.Join(dir, value)
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
