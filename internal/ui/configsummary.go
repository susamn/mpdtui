package ui

// ConfigSummary is a read-only snapshot of the settings mpdtui resolved
// at startup (MPD connection, music_dir, track_metadata). The Settings
// overlay's Config tab ('e') displays it, but it is not that overlay's
// type -- it is Run's own parameter, and the global search, the lyrics
// reindex and the visualizer each read fields off it without the
// Settings overlay being involved at all, which is why it lives here
// rather than in settings.go. internal/ui doesn't depend on
// internal/config (see DEPENDENCY.md) -- cmd/mpdtui/main.go, which
// already resolves all of these via that package, builds this struct and
// passes it into Run, the same "plain already-resolved values, not the
// config system itself" pattern musicDir/metaDB already use. Deliberately
// excludes the MPD password's actual value (MPDPasswordSet is just
// whether one is configured) -- a settings view has no business
// displaying a credential.
type ConfigSummary struct {
	MPDHost              string
	MPDPort              string
	MPDPasswordSet       bool
	MusicDir             string
	TrackMetadataEnabled bool
	ConfigFilePath       string
	DBFilePath           string
	LyricsIndexPath      string

	// ThemeFile is theme_file's configured value (see
	// internal/config.LoadThemeFile), "" meaning internal/theme's own
	// Omarchy default path is in use instead. Shown here so a
	// misconfigured override (a typo'd path, e.g.) is visible without
	// needing to know that theme_file is a settings key at all.
	ThemeFile string

	// VisualizerFIFO is visualizer_fifo's resolved value (see
	// internal/config.LoadVisualizerFIFO) -- the named pipe the
	// visualizer reads live audio from, "" meaning the feature is
	// switched off and the visualizations run on playback state alone.
	// Passed through for the same reason as ThemeFile: internal/ui
	// takes settled values, never internal/config itself.
	VisualizerFIFO string
}
