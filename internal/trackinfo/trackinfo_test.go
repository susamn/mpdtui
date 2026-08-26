package trackinfo

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mpdtui/internal/config"
	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

func TestFormatInfoNothingPlaying(t *testing.T) {
	got := FormatInfo(mpdclient.Song{}, mpdclient.Status{}, "", nil)
	if got != "Nothing playing\n" {
		t.Errorf("FormatInfo(zero Song) = %q, want %q", got, "Nothing playing\n")
	}
}

func TestFormatInfoFullSong(t *testing.T) {
	song := mpdclient.Song{
		Title:    "Bohemian Rhapsody",
		Artist:   "Queen",
		Album:    "A Night at the Opera",
		Genre:    "Rock",
		Composer: "Freddie Mercury",
		Date:     "1975-11-21",
		File:     "Queen/A Night at the Opera/01 Bohemian Rhapsody.flac",
		Duration: 355 * time.Second, // 5:55
	}
	st := mpdclient.Status{
		State:       mpdclient.StatePlay,
		Elapsed:     83 * time.Second, // 1:23
		Duration:    355 * time.Second,
		Bitrate:     920,
		AudioFormat: "44100:16:2",
	}

	got := FormatInfo(song, st, "", nil)

	expectedFields := []struct {
		key string
		val string
	}{
		{"Title:", "Bohemian Rhapsody"},
		{"Artist:", "Queen"},
		{"Album:", "A Night at the Opera"},
		{"Genre:", "Rock"},
		{"Composer:", "Freddie Mercury"},
		{"Date:", "1975"},
		{"File:", "Queen/A Night at the Opera/01 Bohemian Rhapsody.flac"},
		{"Duration:", "5:55"},
		{"Elapsed:", "1:23"},
		{"State:", "play"},
		{"Audio:", "920kbps 44.1kHz/16-bit/2ch"},
	}

	for _, ef := range expectedFields {
		if !strings.Contains(got, ef.key) || !strings.Contains(got, ef.val) {
			t.Errorf("FormatInfo output missing %s %s. Output:\n%s", ef.key, ef.val, got)
		}
	}
}

func TestFormatInfoFallbackToFilename(t *testing.T) {
	song := mpdclient.Song{
		File: "Queen/01 Bohemian Rhapsody.mp3",
	}
	got := FormatInfo(song, mpdclient.Status{}, "", nil)
	if !strings.Contains(got, "01 Bohemian Rhapsody.mp3") {
		t.Errorf("FormatInfo without Title did not fallback to filename: %s", got)
	}
}

func TestFormatInfoWithLyrics(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "Queen")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.lrc"), []byte("[00:01.00]lyrics"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.txt"), []byte("plain text"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	song := mpdclient.Song{
		Title: "Track",
		File:  "Queen/Track.mp3",
	}
	got := FormatInfo(song, mpdclient.Status{}, dir, nil)
	if !strings.Contains(got, "Lyrics:") || !strings.Contains(got, "LRC, TXT") {
		t.Errorf("FormatInfo missing expected lyrics availability: %s", got)
	}

	// Test LRC only
	lrcOnlySong := mpdclient.Song{
		Title: "LRC Track",
		File:  "Queen/LRCTrack.mp3",
	}
	if err := os.WriteFile(filepath.Join(trackDir, "LRCTrack.lrc"), []byte("[00:01.00]lyrics"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	gotLRC := FormatInfo(lrcOnlySong, mpdclient.Status{}, dir, nil)
	if !strings.Contains(gotLRC, "Lyrics:") || !strings.Contains(gotLRC, "LRC") || strings.Contains(gotLRC, "TXT") {
		t.Errorf("FormatInfo expected LRC only: %s", gotLRC)
	}

	// Test TXT only
	txtOnlySong := mpdclient.Song{
		Title: "TXT Track",
		File:  "Queen/TXTTrack.mp3",
	}
	if err := os.WriteFile(filepath.Join(trackDir, "TXTTrack.txt"), []byte("plain text"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	gotTXT := FormatInfo(txtOnlySong, mpdclient.Status{}, dir, nil)
	if !strings.Contains(gotTXT, "Lyrics:") || !strings.Contains(gotTXT, "TXT") || strings.Contains(gotTXT, "LRC") {
		t.Errorf("FormatInfo expected TXT only: %s", gotTXT)
	}

	// Test none
	noneSong := mpdclient.Song{
		Title: "None Track",
		File:  "Queen/NoneTrack.mp3",
	}
	gotNone := FormatInfo(noneSong, mpdclient.Status{}, dir, nil)
	if !strings.Contains(gotNone, "Lyrics:") || !strings.Contains(gotNone, "-") {
		t.Errorf("FormatInfo expected '-' lyrics: %s", gotNone)
	}
}

func TestFormatInfoWithMetadata(t *testing.T) {
	song := mpdclient.Song{
		Title: "Track",
		File:  "Queen/Track.mp3",
	}
	meta := &metadata.Track{
		Rating:    4,
		PlayCount: 7,
		Mark:      &metadata.MarkReason{ID: 1, Reason: "mark for deletion"},
		Tags: []metadata.Tag{
			{ID: 1, Tagname: "classic"},
			{ID: 2, Tagname: "rock"},
		},
	}

	got := FormatInfo(song, mpdclient.Status{}, "", meta)

	expected := []string{
		"Rating:", "4",
		"Play Count:", "7",
		"Mark:", "mark for deletion",
		"Tags:", "classic, rock",
	}
	for _, exp := range expected {
		if !strings.Contains(got, exp) {
			t.Errorf("FormatInfo output missing metadata item %q:\n%s", exp, got)
		}
	}

	// Test with zero metadata
	metaZero := &metadata.Track{}
	gotZero := FormatInfo(song, mpdclient.Status{}, "", metaZero)
	if !strings.Contains(gotZero, "Rating:") || !strings.Contains(gotZero, "0") {
		t.Errorf("FormatInfo expected 0 rating: %s", gotZero)
	}
	if !strings.Contains(gotZero, "Mark:") || !strings.Contains(gotZero, "-") {
		t.Errorf("FormatInfo expected '-' mark: %s", gotZero)
	}
	if !strings.Contains(gotZero, "Tags:") || !strings.Contains(gotZero, "-") {
		t.Errorf("FormatInfo expected '-' tags: %s", gotZero)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0:00"},
		{-5 * time.Second, "0:00"},
		{9 * time.Second, "0:09"},
		{65 * time.Second, "1:05"},
		{355 * time.Second, "5:55"},
		{3600 * time.Second, "60:00"},
	}
	for _, tc := range cases {
		if got := formatDuration(tc.in); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatAudioQuality(t *testing.T) {
	cases := []struct {
		bitrate int
		format  string
		want    string
	}{
		{320, "44100:16:2", "320kbps 44.1kHz/16-bit/2ch"},
		{0, "48000:24:2", "48kHz/24-bit/2ch"},
		{128, "", "128kbps"},
		{0, "", "-"},
		{0, "96000:f:6", "96kHz/float/6ch"},
		{0, "invalid", "-"},
	}
	for _, tc := range cases {
		if got := formatAudioQuality(tc.bitrate, tc.format); got != tc.want {
			t.Errorf("formatAudioQuality(%d, %q) = %q, want %q", tc.bitrate, tc.format, got, tc.want)
		}
	}
}

func TestYearFromDate(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1975-11-21", "1975"},
		{"2024", "2024"},
		{"99", "99"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := yearFromDate(tc.in); got != tc.want {
			t.Errorf("yearFromDate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatInfoEmptyFieldsShowDash(t *testing.T) {
	song := mpdclient.Song{
		Title: "Minimal Song",
		File:  "music/min.mp3",
	}
	st := mpdclient.Status{
		State: mpdclient.StateStop,
	}
	got := FormatInfo(song, st, "", nil)

	for _, ef := range []string{"Artist:", "Album:", "Genre:", "Composer:", "Date:", "Audio:"} {
		if !strings.Contains(got, ef) {
			t.Errorf("FormatInfo output missing line %s:\n%s", ef, got)
		}
	}
}

func TestFormatInfoDurationFallbackToStatus(t *testing.T) {
	song := mpdclient.Song{
		Title:    "Stream Track",
		File:     "stream.mp3",
		Duration: 0,
	}
	st := mpdclient.Status{
		State:    mpdclient.StatePlay,
		Duration: 180 * time.Second,
	}
	got := FormatInfo(song, st, "", nil)
	if !strings.Contains(got, "Duration:") || !strings.Contains(got, "3:00") {
		t.Errorf("FormatInfo expected duration 3:00 from status fallback: %s", got)
	}
}

func TestFormatSampleRate(t *testing.T) {
	cases := []struct {
		hz   int
		want string
	}{
		{44100, "44.1kHz"},
		{48000, "48kHz"},
		{88200, "88.2kHz"},
		{96000, "96kHz"},
		{192000, "192kHz"},
	}
	for _, tc := range cases {
		if got := formatSampleRate(tc.hz); got != tc.want {
			t.Errorf("formatSampleRate(%d) = %q, want %q", tc.hz, got, tc.want)
		}
	}
}

func TestFormatAudioFormatTriplet(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"44100:16:2", "44.1kHz/16-bit/2ch"},
		{"48000:24:2", "48kHz/24-bit/2ch"},
		{"96000:f:6", "96kHz/float/6ch"},
		{"invalid", ""},
		{"44100:16", ""},
		{"44100:16:2:extra", ""},
	}
	for _, tc := range cases {
		if got := formatAudioFormatTriplet(tc.in); got != tc.want {
			t.Errorf("formatAudioFormatTriplet(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBaseName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"a/b/c.flac", "c.flac"},
		{"c.flac", "c.flac"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := baseName(tc.in); got != tc.want {
			t.Errorf("baseName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestUpdateRatingValidation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := metadata.Open(dbPath)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer db.Close()

	if err := UpdateRating(nil, nil, 3, nil); err == nil || !strings.Contains(err.Error(), "track metadata is not enabled") {
		t.Errorf("UpdateRating with nil metaDB want error, got %v", err)
	}

	if err := UpdateRating(nil, db, 0, nil); err == nil || !strings.Contains(err.Error(), "between 1 and 5") {
		t.Errorf("UpdateRating with rating 0 want error, got %v", err)
	}

	if err := UpdateRating(nil, db, 6, nil); err == nil || !strings.Contains(err.Error(), "between 1 and 5") {
		t.Errorf("UpdateRating with rating 6 want error, got %v", err)
	}
}

func dialOrSkip(t *testing.T) *mpdclient.Client {
	t.Helper()
	c, err := mpdclient.Dial(config.Load())
	if err != nil {
		t.Skipf("no MPD server reachable, skipping live MPD test: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestPrintInfoLiveOrSkip(t *testing.T) {
	c := dialOrSkip(t)
	var buf bytes.Buffer
	if err := PrintInfo(c, "", nil, &buf); err != nil {
		t.Fatalf("PrintInfo: %v", err)
	}
	if buf.Len() == 0 {
		t.Errorf("PrintInfo wrote nothing to output buffer")
	}
}

func TestUpdateRatingLiveOrSkip(t *testing.T) {
	c := dialOrSkip(t)
	song, err := c.CurrentSong()
	if err != nil || song.File == "" {
		t.Skip("no track currently playing on MPD server, skipping live UpdateRating test")
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := metadata.Open(dbPath)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer db.Close()

	var buf bytes.Buffer
	if err := UpdateRating(c, db, 5, &buf); err != nil {
		t.Fatalf("UpdateRating: %v", err)
	}

	if !strings.Contains(buf.String(), "Updated rating to 5") {
		t.Errorf("UpdateRating output %q does not contain confirmation", buf.String())
	}

	track, err := db.Get(song.File)
	if err != nil {
		t.Fatalf("db.Get: %v", err)
	}
	if track.Rating != 5 {
		t.Errorf("db.Get rating = %d, want 5", track.Rating)
	}
}
