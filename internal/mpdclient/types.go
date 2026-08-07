package mpdclient

import (
	"strconv"
	"time"

	"github.com/fhs/gompd/v2/mpd"
)

// State is MPD's playback state.
type State string

const (
	StatePlay  State = "play"
	StatePause State = "pause"
	StateStop  State = "stop"
)

// Status is the subset of MPD's status response the UI needs.
type Status struct {
	State          State
	Volume         int // 0-100, or -1 if unknown
	Repeat         bool
	Random         bool
	Single         bool
	Consume        bool
	Elapsed        time.Duration
	Duration       time.Duration
	SongID         int // currently playing/selected queue song id, -1 if none
	PlaylistLength int // number of tracks in the queue
}

// Song is a single track, either from the queue, the library, or a
// stored playlist.
type Song struct {
	ID       int // queue id; -1 outside the queue
	Pos      int // queue position; -1 outside the queue
	File     string
	Artist   string
	Album    string
	Title    string
	Genre    string
	Date     string // MPD's "Date" tag: often just a year, sometimes a full date
	Duration time.Duration
}

// DisplayName is what panels show for a song: "Artist - Title", falling
// back to the bare filename when tags are missing.
func (s Song) DisplayName() string {
	switch {
	case s.Artist != "" && s.Title != "":
		return s.Artist + " - " + s.Title
	case s.Title != "":
		return s.Title
	default:
		return s.File
	}
}

// Playlist is a stored (saved) playlist.
type Playlist struct {
	Name string
	// LastModified is MPD's Last-Modified timestamp for this playlist
	// (when its file was last written), or the zero time if MPD didn't
	// report one.
	LastModified time.Time
}

func parseStatus(a mpd.Attrs) Status {
	vol, _ := strconv.Atoi(a["volume"])
	songID, err := strconv.Atoi(a["songid"])
	if err != nil {
		songID = -1
	}
	elapsed, _ := strconv.ParseFloat(a["elapsed"], 64)
	dur, _ := strconv.ParseFloat(a["duration"], 64)
	playlistLength, _ := strconv.Atoi(a["playlistlength"])

	return Status{
		State:          State(a["state"]),
		Volume:         vol,
		Repeat:         a["repeat"] == "1",
		Random:         a["random"] == "1",
		Single:         a["single"] == "1",
		Consume:        a["consume"] == "1",
		Elapsed:        time.Duration(elapsed * float64(time.Second)),
		Duration:       time.Duration(dur * float64(time.Second)),
		SongID:         songID,
		PlaylistLength: playlistLength,
	}
}

func parseSong(a mpd.Attrs) Song {
	id, err := strconv.Atoi(a["Id"])
	if err != nil {
		id = -1
	}
	pos, err := strconv.Atoi(a["Pos"])
	if err != nil {
		pos = -1
	}
	return Song{
		ID:       id,
		Pos:      pos,
		File:     a["file"],
		Artist:   a["Artist"],
		Album:    a["Album"],
		Title:    a["Title"],
		Genre:    a["Genre"],
		Date:     a["Date"],
		Duration: parseSongDuration(a),
	}
}

func parseSongDuration(a mpd.Attrs) time.Duration {
	if v, ok := a["duration"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(f * float64(time.Second))
		}
	}
	if v, ok := a["Time"]; ok {
		if i, err := strconv.Atoi(v); err == nil {
			return time.Duration(i) * time.Second
		}
	}
	return 0
}

func parseSongs(list []mpd.Attrs) []Song {
	songs := make([]Song, len(list))
	for i, a := range list {
		songs[i] = parseSong(a)
	}
	return songs
}
