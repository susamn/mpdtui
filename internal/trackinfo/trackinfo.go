// Package trackinfo implements mpdtui's non-interactive "-i" and "-iu"
// CLI modes: querying information about the currently playing track and
// updating its metadata (such as ratings) from external scripts and clients.
package trackinfo

import (
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"mpdtui/internal/lyrics"
	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

// FormatInfo formats track information, playback status, lyrics availability,
// and optional local metadata into human-readable key-value lines.
func FormatInfo(song mpdclient.Song, st mpdclient.Status, musicDir string, meta *metadata.Track) string {
	if song.DisplayName() == "" && song.File == "" {
		return "Nothing playing\n"
	}

	title := song.Title
	if title == "" {
		title = baseName(song.File)
	}

	var b strings.Builder
	writeField := func(key, val string) {
		if val == "" {
			val = "-"
		}
		fmt.Fprintf(&b, "%-13s%s\n", key+":", val)
	}

	writeField("Title", title)
	writeField("Artist", song.Artist)
	writeField("Album", song.Album)
	writeField("Genre", song.Genre)
	writeField("Composer", song.Composer)
	writeField("Date", yearFromDate(song.Date))
	writeField("File", song.File)

	dur := song.Duration
	if dur <= 0 && st.Duration > 0 {
		dur = st.Duration
	}
	writeField("Duration", formatDuration(dur))
	writeField("Elapsed", formatDuration(st.Elapsed))
	writeField("State", string(st.State))
	writeField("Audio", formatAudioQuality(st.Bitrate, st.AudioFormat))

	if musicDir != "" && song.File != "" {
		writeField("Lyrics", lyricsAvailability(musicDir, song.File))
	}

	if meta != nil {
		writeField("Rating", strconv.Itoa(meta.Rating))
		writeField("Play Count", strconv.Itoa(meta.PlayCount))
		if meta.Mark != nil {
			writeField("Mark", meta.Mark.Reason)
		} else {
			writeField("Mark", "-")
		}
		if len(meta.Tags) > 0 {
			tagNames := make([]string, len(meta.Tags))
			for i, t := range meta.Tags {
				tagNames[i] = t.Tagname
			}
			writeField("Tags", strings.Join(tagNames, ", "))
		} else {
			writeField("Tags", "-")
		}
	}

	return b.String()
}

// PrintInfo performs an MPD query for the currently playing track and
// writes its details to w. If metaDB is non-nil, local track metadata is
// included; if musicDir is non-empty, sidecar lyrics availability is checked.
func PrintInfo(client *mpdclient.Client, musicDir string, metaDB *metadata.DB, w io.Writer) error {
	song, err := client.CurrentSong()
	if err != nil {
		return err
	}
	if song.DisplayName() == "" && song.File == "" {
		_, err := fmt.Fprintln(w, "Nothing playing")
		return err
	}

	st, err := client.Status()
	if err != nil {
		return err
	}

	var meta *metadata.Track
	if metaDB != nil && song.File != "" {
		t, err := metaDB.Get(song.File)
		if err == nil {
			meta = &t
		}
	}

	text := FormatInfo(song, st, musicDir, meta)
	_, err = fmt.Fprint(w, text)
	return err
}

// UpdateRating sets the rating (1-5) for the currently playing track in metaDB.
func UpdateRating(client *mpdclient.Client, metaDB *metadata.DB, rating int, w io.Writer) error {
	if rating < 1 || rating > 5 {
		return fmt.Errorf("rating must be between 1 and 5")
	}
	if metaDB == nil {
		return fmt.Errorf("track metadata is not enabled (set track_metadata = true in config)")
	}

	song, err := client.CurrentSong()
	if err != nil {
		return err
	}
	if song.File == "" {
		return fmt.Errorf("no track is currently playing")
	}

	if err := metaDB.Rate(song.File, rating); err != nil {
		return fmt.Errorf("update rating: %w", err)
	}

	name := song.DisplayName()
	if name == "" {
		name = song.File
	}
	if w != nil {
		fmt.Fprintf(w, "Updated rating to %d for: %s\n", rating, name)
	}
	return nil
}

func baseName(filePath string) string {
	if filePath == "" {
		return ""
	}
	return path.Base(filePath)
}

func yearFromDate(date string) string {
	if len(date) > 4 {
		return date[:4]
	}
	return date
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	total := int(d.Seconds())
	sec := total % 60
	sSec := strconv.Itoa(sec)
	if sec < 10 {
		sSec = "0" + sSec
	}
	return strconv.Itoa(total/60) + ":" + sSec
}

func formatAudioQuality(bitrate int, audioFormat string) string {
	var segs []string
	if bitrate > 0 {
		segs = append(segs, strconv.Itoa(bitrate)+"kbps")
	}
	if triplet := formatAudioFormatTriplet(audioFormat); triplet != "" {
		segs = append(segs, triplet)
	}
	if len(segs) == 0 {
		return "-"
	}
	return strings.Join(segs, " ")
}

func formatAudioFormatTriplet(audioFormat string) string {
	parts := strings.Split(audioFormat, ":")
	if len(parts) != 3 {
		return ""
	}
	var segs []string
	if hz, err := strconv.Atoi(parts[0]); err == nil && hz > 0 {
		segs = append(segs, formatSampleRate(hz))
	}
	if parts[1] != "" {
		bits := parts[1] + "-bit"
		if parts[1] == "f" {
			bits = "float"
		}
		segs = append(segs, bits)
	}
	if parts[2] != "" {
		segs = append(segs, parts[2]+"ch")
	}
	return strings.Join(segs, "/")
}

func formatSampleRate(hz int) string {
	return strconv.FormatFloat(float64(hz)/1000, 'f', -1, 64) + "kHz"
}

func lyricsAvailability(musicDir, file string) string {
	hasLRC := false
	hasTxt := false

	if _, ok := lyrics.FindLRC(musicDir, file); ok {
		hasLRC = true
	}
	if _, ok := lyrics.Find(musicDir, file); ok {
		hasTxt = true
	}

	switch {
	case hasLRC && hasTxt:
		return "LRC, TXT"
	case hasLRC:
		return "LRC"
	case hasTxt:
		return "TXT"
	default:
		return "-"
	}
}
