package ui

import (
	"strings"

	"mpdtui/internal/mpdclient"
	"mpdtui/internal/textutil"
)

// containsFold reports whether haystack contains needle, ignoring case and
// diacritics (e.g. containsFold("Bublé", "buble") is true).
func containsFold(haystack, needle string) bool {
	return strings.Contains(textutil.FoldSearch(haystack), textutil.FoldSearch(needle))
}

// songMatchesQuery approximates MPD's "any" tag search across the tags
// this app actually keeps (see mpdclient.Song): Title, Artist, Album,
// Genre, Composer, Date, falling back to the bare filename so untagged
// tracks stay findable. Case- and diacritic-insensitive throughout (see
// containsFold).
func songMatchesQuery(s mpdclient.Song, query string) bool {
	return containsFold(s.Title, query) ||
		containsFold(s.Artist, query) ||
		containsFold(s.Album, query) ||
		containsFold(s.Genre, query) ||
		containsFold(s.Composer, query) ||
		containsFold(s.Date, query) ||
		containsFold(baseName(s.File), query)
}
