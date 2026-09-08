package ui

import "strings"

// Indic conjuncts and terminal cell width.
//
// Terminals that implement Unicode 15.1 grapheme segmentation (kitty
// does; its own width function was used to measure all of this) treat a
// consonant + virama + consonant sequence -- an Indic conjunct -- as a
// single grapheme cluster occupying one cell. tview, via uniseg, does
// not: it predates that rule (GB9c) and splits such a sequence into two
// clusters, laying out and reserving two cells for it.
//
// That disagreement corrupts far more than the text itself. tview writes
// each of its clusters into its own tcell cell; tcell then emits those
// cells back to back as one contiguous run, only repositioning the
// cursor when its *own* model says the column jumped. The terminal
// re-merges the two halves of the conjunct into one cell, its cursor
// ends up a column short of where tcell believes it is, and every
// character drawn afterwards on that terminal line lands too far left.
// Because mpdtui's panels sit side by side, a Bengali playlist name in
// the Playlists panel visibly scrambles the Queue rows next to it.
//
// The fix is to stop the terminal re-merging what tview has already
// split, by inserting a zero-width joiner between the virama and the
// consonant after it. Measured against kitty, this takes the drift to
// exactly zero:
//
//	"স্বর্ণালী সন্ধ্যা"   tview 11, terminal 7  -> 11, drift 4 -> 0
//	"লেজেন্ডস"           tview  7, terminal 6  ->  7, drift 1 -> 0
//	"জীবনমুখী"           tview  7, terminal 7  (no virama, already fine)
//
// The cost is that the conjunct renders as its two consonants with the
// virama shown, rather than as a ligature. That is not a regression so
// much as the truth becoming visible: tview had already allocated two
// cells and drawn the halves separately, and the ligature only appeared
// because the terminal quietly re-joined them -- which is precisely what
// was breaking the layout. Correct columns are worth more here than a
// ligature in a list of playlist names.
//
// This is display-only. It must never touch a value sent back to MPD --
// a playlist name with a joiner spliced into it names nothing.
const (
	// conjunctJoiner is U+200C ZERO WIDTH NON-JOINER.
	//
	// It has to be this character specifically, and the obvious
	// alternative silently does nothing. U+2060 WORD JOINER measures
	// identically and looks like the more neutral choice -- it is a pure
	// formatting control, where ZWNJ is a real orthographic mark in
	// Indic scripts -- but its grapheme-cluster class is Control, so it
	// forms a cluster of its own with width zero, and tview skips
	// zero-width clusters without writing them. It never reaches the
	// terminal, and the drift is completely unchanged. ZWNJ is
	// Extend, so it stays attached to the cluster it follows and is
	// written out with it.
	//
	// It is also the honest character for the job: ZWNJ means "do not
	// form a conjunct here", which is exactly what tview has already
	// decided by laying the two consonants out in separate cells. This
	// makes that decision explicit to the terminal instead of leaving it
	// to be silently reversed.
	conjunctJoiner = '‌'
)

// indicLinkers are the viramas that participate in Unicode 15.1's Indic
// conjunct rule (InCB=Linker). Determined by measurement rather than
// transcribed from the standard: each script's consonant+virama+
// consonant sequence was fed to the terminal's own width function to see
// which ones came back as a single cell. Gurmukhi, Tamil, Kannada and
// Sinhala did not merge and so are deliberately absent -- inserting a
// joiner there would be pure noise in the text for no layout benefit.
var indicLinkers = map[rune]bool{
	'्': true, // Devanagari
	'্': true, // Bengali
	'્': true, // Gujarati
	'୍': true, // Oriya
	'్': true, // Telugu
	'്': true, // Malayalam
}

// splitConjuncts returns s with a zero-width joiner after every Indic
// linker, so a terminal cannot re-merge a conjunct that tview has
// already laid out as two cells. Strings without a linker -- which is
// nearly all of them -- come back untouched, with no allocation.
//
// A linker already followed by the joiner is left alone, so running this
// over an already-processed string is a no-op rather than piling up
// joiners.
func splitConjuncts(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return indicLinkers[r] }) {
		return s
	}
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i, r := range runes {
		b.WriteRune(r)
		if indicLinkers[r] && (i+1 >= len(runes) || runes[i+1] != conjunctJoiner) {
			b.WriteRune(conjunctJoiner)
		}
	}
	return b.String()
}
