package config

import (
	"fmt"
	"os"

	"mpdtui/internal/theme"
)

// configFileTemplate is what EnsureConfigFiles writes as a fresh
// ConfigFile when none exists yet -- just enough to make theme_file's
// default explicit and point at a real, already-created file, plus
// commented examples for the other settings LoadMusicDir/
// LoadTrackMetadataEnabled read, so a first-time user finds every
// available setting in one place rather than needing this package's
// own doc comments to discover them.
const configFileTemplate = `# mpdtui settings -- see the README for the full list of what each of
# these does. Blank lines and lines starting with # are ignored.

# Which color file mpdtui reads its palette from. A relative path (like
# the default below) resolves against this same directory; "~/..." and
# absolute paths both work too. Points at mpdtui's own default file
# (created alongside this one) unless you change it -- to follow a live
# desktop theme instead, point this at:
#   - Omarchy: ~/.local/state/omarchy/current/theme/colors.toml
#   - matugen/Hyprland: wherever your own matugen template's output_path
#     writes (see the README for a template that matches this file's
#     own shape)
theme_file = ./colors.toml

# Local filesystem path mirroring MPD's own music_directory, needed for
# the lyrics feature (Y). Leave commented out to keep it inactive.
# music_dir = ~/Music

# Local play-count/rating/mark/tags tracking, in a SQLite database next
# to this file. Off by default.
# track_metadata = true
`

// EnsureConfigFiles makes sure mpdtui's own settings file (ConfigFile)
// and default color file (DefaultColorsFile) both exist on disk,
// creating either one that's missing -- called unconditionally at the
// start of every run (cmd/mpdtui's main.go), the same "always ensure
// this exists, every time, not gated behind a feature flag" spirit as
// internal/metadata.Open's own schema/seed-table creation. Never
// touches a file that's already there, so hand-edited settings or a
// hand-edited color file are never overwritten.
//
// Errors here are non-fatal to the caller by design (see main.go): a
// permissions problem creating these shouldn't stop mpdtui from
// running, just leave it reading Default() straight from memory the
// same as it always could.
func EnsureConfigFiles() error {
	dir := ConfigDir()
	if dir == "" {
		return fmt.Errorf("could not determine mpdtui's config directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory %s: %w", dir, err)
	}

	colorsPath := DefaultColorsFile()
	if _, err := os.Stat(colorsPath); os.IsNotExist(err) {
		content := theme.Serialize(theme.Default())
		if err := os.WriteFile(colorsPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write default color file %s: %w", colorsPath, err)
		}
	}

	configPath := ConfigFile()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(configFileTemplate), 0o644); err != nil {
			return fmt.Errorf("write config file %s: %w", configPath, err)
		}
	}

	return nil
}
