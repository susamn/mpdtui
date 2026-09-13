package ui

import (
	"mpdtui/internal/settingsview"
	"mpdtui/internal/uitheme"
)

// configRows describes ConfigSummary for the Settings overlay's Config
// tab. It lives here, not in internal/settingsview, because it is this
// package that owns ConfigSummary: adding a setting means adding a
// field and a line here, and never touching the view. The view renders
// label/value pairs and knows nothing about what a setting is.
//
// Theme Status is the one row not read off ConfigSummary -- whether the
// configured theme file was actually loadable is uitheme's answer to
// give, and it is shown here so a misconfigured theme_file is visible
// without having to know theme_file is a settings key at all.
func configRows(cfg ConfigSummary) []settingsview.Row {
	password := "not set"
	if cfg.MPDPasswordSet {
		password = "set"
	}
	musicDir := cfg.MusicDir
	if musicDir == "" {
		musicDir = "(not configured -- lyrics feature inactive)"
	}
	trackMetadata := "no"
	if cfg.TrackMetadataEnabled {
		trackMetadata = "yes"
	}
	themeStatus := "not found -- built-in default colors in use"
	if uitheme.Found() {
		themeStatus = "found -- active"
	}

	return []settingsview.Row{
		{Label: "MPD Host", Value: cfg.MPDHost},
		{Label: "MPD Port", Value: cfg.MPDPort},
		{Label: "MPD Password", Value: password},
		{Label: "Music Directory", Value: musicDir},
		{Label: "Track Metadata", Value: trackMetadata},
		{Label: "Config File", Value: orPlaceholder(cfg.ConfigFilePath)},
		{Label: "Database File", Value: orPlaceholder(cfg.DBFilePath)},
		{Label: "Lyrics Index File", Value: orPlaceholder(cfg.LyricsIndexPath)},
		{Label: "Theme File", Value: orPlaceholder(cfg.ThemeFile)},
		{Label: "Theme Status", Value: themeStatus},
	}
}

// orPlaceholder keeps an empty path from rendering as a blank cell,
// which reads as "this row failed" rather than "this isn't set".
func orPlaceholder(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

// openSettings is 'e'. Reset before showOverlay, never after: Reset
// deliberately doesn't move focus, so that showOverlay captures
// whatever was focused before 'e' and can restore it on close.
func (a *App) openSettings() {
	a.settings.Reset()
	a.showOverlay("settings", centered(a.settings.Root(), 76, 22), a.settings.InitialFocus())
}
