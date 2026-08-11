package ui

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"mpdtui/internal/mpdclient"
)

// stripDiacritics decomposes accented runes into base+combining-mark form
// (NFD), drops the combining marks (unicode.Mn), then recomposes (NFC) --
// e.g. "Bublé" -> "Buble". transform.Chain never errors on well-formed
// UTF-8 input, which is all this app ever feeds it (MPD tag data, or text
// typed into a tview.InputField), so foldSearch below treats an error as
// unreachable rather than a case to handle.
var stripDiacritics = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// foldSearch lowercases s and strips its diacritics, so two strings that
// differ only by accent or case fold to the same value.
func foldSearch(s string) string {
	folded, _, err := transform.String(stripDiacritics, s)
	if err != nil {
		folded = s
	}
	return strings.ToLower(folded)
}

// containsFold reports whether haystack contains needle, ignoring case and
// diacritics (e.g. containsFold("Bublé", "buble") is true).
func containsFold(haystack, needle string) bool {
	return strings.Contains(foldSearch(haystack), foldSearch(needle))
}

// songMatchesQuery approximates MPD's "any" tag search across the tags
// this app actually keeps (see mpdclient.Song): Title, Artist, Album,
// Genre, Date, falling back to the bare filename so untagged tracks stay
// findable. Case- and diacritic-insensitive throughout (see containsFold).
func songMatchesQuery(s mpdclient.Song, query string) bool {
	return containsFold(s.Title, query) ||
		containsFold(s.Artist, query) ||
		containsFold(s.Album, query) ||
		containsFold(s.Genre, query) ||
		containsFold(s.Date, query) ||
		containsFold(baseName(s.File), query)
}
