// Command mpdtui is a lazygit-style terminal UI for MPD.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"mpdtui/internal/config"
	"mpdtui/internal/lyricsline"
	"mpdtui/internal/metadata"
	"mpdtui/internal/mini"
	"mpdtui/internal/mpdclient"
	"mpdtui/internal/picker"
	"mpdtui/internal/trackinfo"
	"mpdtui/internal/ui"
	"mpdtui/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main's body with the process boundary pulled out: it takes the
// arguments and streams rather than reaching for os.Args and os.Stdout,
// and returns an exit code rather than calling os.Exit. That makes every
// mode reachable from a test, which main itself never was.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mpdtui", flag.ContinueOnError)
	opts, err := parseFlags(fs, args, stderr)
	if err != nil {
		return 2 // the flag package has already reported it
	}

	if opts.showVersion {
		fmt.Fprintln(stdout, version.String)
		return 0
	}

	if err := opts.validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	// Mandatory on every run, every mode -- mirrors internal/metadata.
	// Open's own "always ensure the schema exists" spirit, just for
	// mpdtui's own settings file and default color file instead of a
	// database. Non-fatal: a permissions problem here shouldn't stop
	// mpdtui from running, just leave theme_file/music_dir/
	// track_metadata unresolvable from a config file that was never
	// written, same as before this existed.
	if err := config.EnsureConfigFiles(); err != nil {
		fmt.Fprintf(stderr, "mpdtui: %v -- continuing without it\n", err)
	}

	cfg := config.Load()
	client, err := mpdclient.Dial(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "mpdtui: connect to MPD at %s: %v\n", cfg.Addr(), err)
		return 1
	}
	defer client.Close()

	// Only opened for the modes that actually use it -- see
	// options.needsMetaDB for why the others must not.
	var metaDB *metadata.DB
	if opts.needsMetaDB() && config.LoadTrackMetadataEnabled() {
		metaDB, err = metadata.Open(config.DBFile())
		if err != nil {
			if opts.trackInfoUpdate {
				fmt.Fprintf(stderr, "mpdtui: track metadata database (%s): %v\n", config.DBFile(), err)
				return 1
			}
			// Non-fatal, unlike the MPD connection above: this is an
			// opt-in local bookkeeping feature, not something the rest of
			// the app depends on -- a broken local database shouldn't
			// stop the music from playing.
			fmt.Fprintf(stderr, "mpdtui: track metadata database (%s): %v -- continuing without it\n", config.DBFile(), err)
			metaDB = nil
		} else {
			defer metaDB.Close()
		}
	}

	switch {
	case opts.playlistPicker:
		err = picker.RunPlaylistPicker(client, config.LoadThemeFile())
	case opts.trackPicker:
		err = picker.RunTrackPicker(client, config.LoadThemeFile())
	case opts.miniMode:
		err = mini.Run(client, metaDB, config.LoadThemeFile())
	case opts.lyricsLine:
		err = lyricsline.Print(client, config.LoadMusicDir(), stdout)
	case opts.trackInfo:
		err = trackinfo.PrintInfo(client, config.LoadMusicDir(), metaDB, stdout)
	case opts.trackInfoUpdate:
		err = trackinfo.UpdateRating(client, metaDB, opts.rating, stdout)
	default:
		err = ui.Run(client, config.LoadMusicDir(), metaDB, summaryFrom(cfg))
	}
	if err != nil {
		fmt.Fprintf(stderr, "mpdtui: %v\n", err)
		return 1
	}
	return 0
}

// summaryFrom builds the read-only settings snapshot the full UI shows
// in its Settings overlay. Every value is resolved here, in the one
// place that owns internal/config, and handed over as plain data (see
// DEPENDENCY.md).
func summaryFrom(cfg config.Config) ui.ConfigSummary {
	return ui.ConfigSummary{
		MPDHost:              cfg.Host,
		MPDPort:              cfg.Port,
		MPDPasswordSet:       cfg.Password != "",
		MusicDir:             config.LoadMusicDir(),
		TrackMetadataEnabled: config.LoadTrackMetadataEnabled(),
		ConfigFilePath:       config.ConfigFile(),
		DBFilePath:           config.DBFile(),
		LyricsIndexPath:      config.LyricsIndexFile(),
		ThemeFile:            config.LoadThemeFile(),
		VisualizerFIFO:       config.LoadVisualizerFIFO(),
	}
}

func modeCount(flags ...bool) int {
	n := 0
	for _, f := range flags {
		if f {
			n++
		}
	}
	return n
}
