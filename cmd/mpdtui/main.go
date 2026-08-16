// Command mpdtui is a lazygit-style terminal UI for MPD.
package main

import (
	"flag"
	"fmt"
	"os"

	"mpdtui/internal/config"
	"mpdtui/internal/metadata"
	"mpdtui/internal/mini"
	"mpdtui/internal/mpdclient"
	"mpdtui/internal/picker"
	"mpdtui/internal/ui"
)

func main() {
	miniMode := flag.Bool("mini", false, "run the lightweight inline player instead of the full panel UI")
	playlistPicker := flag.Bool("p", false, "fuzzy-search playlists; Enter clears the queue and plays the selection")
	trackPicker := flag.Bool("t", false, "fuzzy-search tracks; Enter adds the selection to the queue and plays it")
	flag.Parse()

	if modeCount(*miniMode, *playlistPicker, *trackPicker) > 1 {
		fmt.Fprintln(os.Stderr, "mpdtui: -mini, -p, and -t are mutually exclusive")
		os.Exit(1)
	}

	cfg := config.Load()
	client, err := mpdclient.Dial(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mpdtui: connect to MPD at %s: %v\n", cfg.Addr(), err)
		os.Exit(1)
	}
	defer client.Close()

	var metaDB *metadata.DB
	if config.LoadTrackMetadataEnabled() {
		metaDB, err = metadata.Open(config.DBFile())
		if err != nil {
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

	switch {
	case *playlistPicker:
		err = picker.RunPlaylistPicker(client)
	case *trackPicker:
		err = picker.RunTrackPicker(client)
	case *miniMode:
		err = mini.Run(client, metaDB)
	default:
		err = ui.Run(client, config.LoadMusicDir(), metaDB)
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
