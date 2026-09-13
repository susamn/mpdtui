package main

import (
	"errors"
	"flag"
	"io"
)

// options is the parsed command line. Splitting it out of main keeps the
// decisions the flags imply -- which of them conflict, whether the
// track-metadata database is needed at all -- separate from acting on
// them, so they can be checked without running the program.
type options struct {
	showVersion     bool
	miniMode        bool
	playlistPicker  bool
	trackPicker     bool
	lyricsLine      bool
	trackInfo       bool
	trackInfoUpdate bool
	rating          int
}

// parseFlags registers every flag on fs and parses args into an options.
func parseFlags(fs *flag.FlagSet, args []string, out io.Writer) (options, error) {
	var o options
	fs.SetOutput(out)
	fs.BoolVar(&o.showVersion, "v", false, "print version and exit")
	fs.BoolVar(&o.miniMode, "mini", false, "run the lightweight inline player instead of the full panel UI")
	fs.BoolVar(&o.playlistPicker, "p", false, "fuzzy-search playlists; Enter clears the queue and plays the selection")
	fs.BoolVar(&o.trackPicker, "t", false, "fuzzy-search tracks; Enter adds the selection to the queue and plays it")
	fs.BoolVar(&o.lyricsLine, "lyrics-line", false, "print the current synced (.lrc) lyrics window (1 line above, the current line, 2 lines below) and exit -- for embedding in an external tool like conky")
	fs.BoolVar(&o.trackInfo, "i", false, "print info for the currently playing track and exit")
	fs.BoolVar(&o.trackInfoUpdate, "iu", false, "update metadata for the currently playing track and exit")
	fs.IntVar(&o.rating, "r", 0, "rating value (1-5) to update when used with -iu")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	return o, nil
}

// Errors reported by validate. Named so the tests can assert on which
// rule was broken rather than on message wording.
var (
	errExclusiveModes = errors.New("mpdtui: -mini, -p, -t, -lyrics-line, -i, and -iu are mutually exclusive")
	errRatingNeedsIU  = errors.New("mpdtui: -r must be used with -iu")
	errIUNeedsUpdate  = errors.New("mpdtui: -iu requires an update flag (e.g. -r 1-5)")
	errRatingRange    = errors.New("mpdtui: -r rating must be between 1 and 5")
)

// validate reports the first rule the flag combination breaks. Each mode
// takes over the terminal or prints and exits, so two at once has no
// sensible meaning -- better to say so than to silently pick one.
func (o options) validate() error {
	if o.modeCount() > 1 {
		return errExclusiveModes
	}
	if o.rating != 0 && !o.trackInfoUpdate {
		return errRatingNeedsIU
	}
	if o.trackInfoUpdate {
		if o.rating == 0 {
			return errIUNeedsUpdate
		}
		if o.rating < 1 || o.rating > 5 {
			return errRatingRange
		}
	}
	return nil
}

// modeCount is how many of the mutually exclusive modes were asked for.
func (o options) modeCount() int {
	return modeCount(o.miniMode, o.playlistPicker, o.trackPicker, o.lyricsLine, o.trackInfo, o.trackInfoUpdate)
}

// fullUI reports whether no alternative mode was chosen, so the full
// panel UI is what runs.
func (o options) fullUI() bool {
	return o.modeCount() == 0
}

// needsMetaDB reports whether this mode reads or writes the local
// track-metadata database.
//
// The distinction matters beyond saving an open: -lyrics-line is polled
// once a second by external tools like conky, one process per line, and
// having those open a database they never touch produced a steady
// stream of "database is locked" warnings from four short-lived
// processes contending over the same sqlite file.
func (o options) needsMetaDB() bool {
	return o.miniMode || o.fullUI() || o.trackInfo || o.trackInfoUpdate
}
