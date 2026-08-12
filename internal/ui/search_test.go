package ui

import (
	"testing"

	"mpdtui/internal/mpdclient"
)

func TestContainsFoldIsAccentInsensitive(t *testing.T) {
	cases := []struct {
		haystack, needle string
		want             bool
	}{
		{"Bublé", "buble", true},
		{"Bublé", "BUBLE", true},
		{"Buble", "bublé", true}, // works both directions: fold applies to both sides
		{"Café del Mar", "cafe", true},
		{"Sigur Rós", "ros", true},
		{"Motörhead", "motorhead", true},
		{"Queen", "quee", true},
		{"Queen", "abba", false},
	}
	for _, tc := range cases {
		if got := containsFold(tc.haystack, tc.needle); got != tc.want {
			t.Errorf("containsFold(%q, %q) = %v, want %v", tc.haystack, tc.needle, got, tc.want)
		}
	}
}

func TestSongMatchesQueryChecksEveryTagAndFilenameFallback(t *testing.T) {
	s := mpdclient.Song{
		File:     "queen/night-at-the-opera/bohemian-rhapsody.mp3",
		Artist:   "Queen",
		Album:    "A Night at the Opera",
		Title:    "Bohemian Rhapsody",
		Genre:    "Rock",
		Composer: "Freddie Mercury",
		Date:     "1975",
	}
	for _, query := range []string{"queen", "opera", "rhapsody", "rock", "freddie mercury", "1975", "QUEEN"} {
		if !songMatchesQuery(s, query) {
			t.Errorf("songMatchesQuery(%+v, %q) = false, want true", s, query)
		}
	}
	if songMatchesQuery(s, "abba") {
		t.Error("songMatchesQuery matched an unrelated query")
	}

	untagged := mpdclient.Song{File: "misc/track-42.mp3"}
	if !songMatchesQuery(untagged, "track-42") {
		t.Error("songMatchesQuery should fall back to the filename for an untagged track")
	}
}
