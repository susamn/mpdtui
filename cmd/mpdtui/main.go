// Command mpdtui is a lazygit-style terminal UI for MPD.
package main

import (
	"flag"
	"fmt"
	"os"

	"mpdtui/internal/cast"
	"mpdtui/internal/config"
	"mpdtui/internal/lyricsline"
	"mpdtui/internal/metadata"
	"mpdtui/internal/mini"
	"mpdtui/internal/mpdclient"
	"mpdtui/internal/picker"
	"mpdtui/internal/trackinfo"
	"mpdtui/internal/ui"
)

func main() {
	miniMode := flag.Bool("mini", false, "run the lightweight inline player instead of the full panel UI")
	playlistPicker := flag.Bool("p", false, "fuzzy-search playlists; Enter clears the queue and plays the selection")
	trackPicker := flag.Bool("t", false, "fuzzy-search tracks; Enter adds the selection to the queue and plays it")
	lyricsLine := flag.Bool("lyrics-line", false, "print the current synced (.lrc) lyrics window (1 line above, the current line, 2 lines below) and exit -- for embedding in an external tool like conky")
	trackInfo := flag.Bool("i", false, "print info for the currently playing track and exit")
	trackInfoUpdate := flag.Bool("iu", false, "update metadata for the currently playing track and exit")
	ratingFlag := flag.Int("r", 0, "rating value (1-5) to update when used with -iu")
	stopFlag := flag.Bool("stop", false, "stop MPD playback and exit")
	castList := flag.Bool("cast-list", false, "list discovered cast targets and exit")
	castTo := flag.String("cast-to", "", "start casting MPD's audio to the named (or id'd) cast target and exit")
	castStop := flag.Bool("cast-stop", false, "tear down the active cast (stop the device, restore MPD outputs) and exit")
	flag.Parse()

	if modeCount(*miniMode, *playlistPicker, *trackPicker, *lyricsLine, *trackInfo, *trackInfoUpdate) > 1 {
		fmt.Fprintln(os.Stderr, "mpdtui: -mini, -p, -t, -lyrics-line, -i, and -iu are mutually exclusive")
		os.Exit(1)
	}

	// -stop and the -cast-* actions are one-shot: do the thing, exit. They
	// are mutually exclusive with each other and with any of the UI modes
	// above -- checked separately so the message above (and its tests)
	// stays untouched.
	actionCount := modeCount(*stopFlag, *castList, *castTo != "", *castStop)
	if actionCount > 1 {
		fmt.Fprintln(os.Stderr, "mpdtui: -stop, -cast-list, -cast-to, and -cast-stop are mutually exclusive")
		os.Exit(1)
	}
	if actionCount == 1 && modeCount(*miniMode, *playlistPicker, *trackPicker, *lyricsLine, *trackInfo, *trackInfoUpdate) > 0 {
		fmt.Fprintln(os.Stderr, "mpdtui: -stop and -cast-* cannot be combined with -mini, -p, -t, -lyrics-line, -i, or -iu")
		os.Exit(1)
	}

	if *ratingFlag != 0 && !*trackInfoUpdate {
		fmt.Fprintln(os.Stderr, "mpdtui: -r must be used with -iu")
		os.Exit(1)
	}

	if *trackInfoUpdate {
		if *ratingFlag == 0 {
			fmt.Fprintln(os.Stderr, "mpdtui: -iu requires an update flag (e.g. -r 1-5)")
			os.Exit(1)
		}
		if *ratingFlag < 1 || *ratingFlag > 5 {
			fmt.Fprintln(os.Stderr, "mpdtui: -r rating must be between 1 and 5")
			os.Exit(1)
		}
	}

	// Mandatory on every run, every mode -- mirrors internal/metadata.
	// Open's own "always ensure the schema exists" spirit, just for
	// mpdtui's own settings file and default color file instead of a
	// database. Non-fatal: a permissions problem here shouldn't stop
	// mpdtui from running, just leave theme_file/music_dir/
	// track_metadata unresolvable from a config file that was never
	// written, same as before this existed.
	if err := config.EnsureConfigFiles(); err != nil {
		fmt.Fprintf(os.Stderr, "mpdtui: %v -- continuing without it\n", err)
	}

	cfg := config.Load()
	client, err := mpdclient.Dial(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mpdtui: connect to MPD at %s: %v\n", cfg.Addr(), err)
		os.Exit(1)
	}
	defer client.Close()

	// Only opened for the modes that actually use it (miniMode, the
	// default full UI, trackInfo, and trackInfoUpdate) -- picker.Run*Picker
	// and lyricsline.Print don't take a metaDB at all, so opening it for
	// them was pure waste. That waste turned into real contention once
	// -lyrics-line started being polled every second by an external tool
	// (e.g. conky, one process per line it prints): four concurrent
	// short-lived processes each opening the same sqlite file produced a
	// steady stream of "database is locked" (SQLITE_BUSY) warnings on
	// stderr, even though none of them needed the database in the first place.
	headlessAction := *stopFlag || *castList || *castTo != "" || *castStop
	fullUIMode := !*miniMode && !*playlistPicker && !*trackPicker && !*lyricsLine && !*trackInfo && !*trackInfoUpdate && !headlessAction
	var metaDB *metadata.DB
	if (*miniMode || fullUIMode || *trackInfo || *trackInfoUpdate) && config.LoadTrackMetadataEnabled() {
		metaDB, err = metadata.Open(config.DBFile())
		if err != nil {
			if *trackInfoUpdate {
				fmt.Fprintf(os.Stderr, "mpdtui: track metadata database (%s): %v\n", config.DBFile(), err)
				os.Exit(1)
			}
			// Non-fatal, unlike the MPD connection above: this is an
			// opt-in local bookkeeping feature, not something the rest of
			// the app depends on -- a broken local database shouldn't
			// stop the music from playing.
			fmt.Fprintf(os.Stderr, "mpdtui: track metadata database (%s): %v -- continuing without it\n", config.DBFile(), err)
			metaDB = nil
		} else {
			defer metaDB.Close()
		}
	}

	// Built once, shared by the headless -cast-* actions and the full UI.
	var castMgr *cast.Manager
	if fullUIMode || *castList || *castTo != "" || *castStop {
		castMgr = cast.NewManager(cast.LoadConfig(cfg.Host), client)
	}

	switch {
	case *stopFlag:
		err = client.Stop()
	case *castList:
		err = cast.List(os.Stdout, castMgr)
	case *castTo != "":
		err = cast.Start(os.Stdout, castMgr, *castTo)
	case *castStop:
		err = cast.Stop(os.Stdout, castMgr)
	case *playlistPicker:
		err = picker.RunPlaylistPicker(client, config.LoadThemeFile())
	case *trackPicker:
		err = picker.RunTrackPicker(client, config.LoadThemeFile())
	case *miniMode:
		err = mini.Run(client, metaDB, config.LoadThemeFile())
	case *lyricsLine:
		err = lyricsline.Print(client, config.LoadMusicDir(), os.Stdout)
	case *trackInfo:
		err = trackinfo.PrintInfo(client, config.LoadMusicDir(), metaDB, os.Stdout)
	case *trackInfoUpdate:
		if *ratingFlag != 0 {
			err = trackinfo.UpdateRating(client, metaDB, *ratingFlag, os.Stdout)
		}
	default:
		summary := ui.ConfigSummary{
			MPDHost:              cfg.Host,
			MPDPort:              cfg.Port,
			MPDPasswordSet:       cfg.Password != "",
			MusicDir:             config.LoadMusicDir(),
			TrackMetadataEnabled: config.LoadTrackMetadataEnabled(),
			ConfigFilePath:       config.ConfigFile(),
			DBFilePath:           config.DBFile(),
			ThemeFile:            config.LoadThemeFile(),
		}
		err = ui.Run(client, config.LoadMusicDir(), metaDB, summary, castMgr)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "mpdtui: %v\n", err)
		os.Exit(1)
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
