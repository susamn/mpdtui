package textutil

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// StripDiacritics decomposes accented runes into base+combining-mark form
// (NFD), drops the combining marks (unicode.Mn), then recomposes (NFC).
var StripDiacritics = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// FoldSearch lowercases s and strips its diacritics.
func FoldSearch(s string) string {
	folded, _, err := transform.String(StripDiacritics, s)
	if err != nil {
		folded = s
	}
	return strings.ToLower(folded)
}

// NormalizeSegment folds s to just its letters and digits, lowercased.
func NormalizeSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
