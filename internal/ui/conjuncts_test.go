package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// The playlist names from the library this was reported against. The
// first two contain Indic conjuncts and were the ones scrambling their
// rows; the third has none and was already fine, which is what makes it
// worth keeping here.
const (
	bengaliConjunct  = "স্বর্ণালী সন্ধ্যা" // 4 conjuncts
	bengaliOneJoin   = "লেজেন্ডস"          // 1 conjunct
	bengaliNoJoin    = "জীবনমুখী"          // no virama at all
	devanagariSample = "क्ष"               // Devanagari, a different linker
)

func TestSplitConjunctsLeavesPlainTextAlone(t *testing.T) {
	for _, s := range []string{"", "Thumri", "Sensuous 2024", "🎵 Hindi Gold", "Café", bengaliNoJoin} {
		if got := splitConjuncts(s); got != s {
			t.Errorf("splitConjuncts(%q) = %q, want it unchanged", s, got)
		}
	}
}

func TestSplitConjunctsInsertsAfterEveryLinker(t *testing.T) {
	got := splitConjuncts(bengaliConjunct)
	if n := strings.Count(got, string(conjunctJoiner)); n != 4 {
		t.Errorf("inserted %d joiners into %q, want one per conjunct (4)", n, bengaliConjunct)
	}
	// Every joiner must sit immediately after a linker, never anywhere else.
	runes := []rune(got)
	for i, r := range runes {
		if r != conjunctJoiner {
			continue
		}
		if i == 0 || !indicLinkers[runes[i-1]] {
			t.Errorf("joiner at index %d does not follow a linker", i)
		}
	}
}

func TestSplitConjunctsHandlesOtherIndicScripts(t *testing.T) {
	if got := splitConjuncts(devanagariSample); got == devanagariSample {
		t.Errorf("Devanagari conjunct %q was left alone, want it split", devanagariSample)
	}
	// Scripts whose viramas the terminal does not merge are deliberately
	// excluded -- adding a joiner there is noise in the text for no
	// layout benefit.
	for _, s := range []string{"ਕ੍ਕ" /* Gurmukhi */, "க்க" /* Tamil */, "ಕ್ಕ" /* Kannada */} {
		if got := splitConjuncts(s); got != s {
			t.Errorf("splitConjuncts(%q) = %q, want it unchanged -- that script's virama does not merge", s, got)
		}
	}
}

// TestSplitConjunctsDoesNotChangeLayoutWidth is the constraint that makes
// this safe to apply everywhere: the joiner must be invisible to the
// layout, so columns are budgeted exactly as they were. If this ever
// fails, the fix has started causing the very problem it exists to
// solve.
func TestSplitConjunctsDoesNotChangeLayoutWidth(t *testing.T) {
	for _, s := range []string{bengaliConjunct, bengaliOneJoin, bengaliNoJoin, devanagariSample, "Thumri"} {
		before := tview.TaggedStringWidth(s)
		after := tview.TaggedStringWidth(splitConjuncts(s))
		if before != after {
			t.Errorf("%q: layout width changed %d -> %d", s, before, after)
		}
	}
}

// TestSplitConjunctsJoinerSurvivesTviewsPrintLoop guards the trap that
// made the obvious choice of character silently useless: tview skips
// grapheme clusters of zero width without writing them, so a joiner that
// forms its own cluster (U+2060 WORD JOINER does) never reaches the
// terminal and the drift is unchanged. The joiner must attach to the
// cluster before it.
func TestSplitConjunctsJoinerSurvivesTviewsPrintLoop(t *testing.T) {
	split := splitConjuncts(bengaliOneJoin)
	if !strings.ContainsRune(streamAsPrinted(split), conjunctJoiner) {
		t.Error("joiner did not survive tview's print loop -- it forms its own zero-width cluster and is being dropped")
	}
	if strings.ContainsRune(streamAsPrinted(bengaliOneJoin), conjunctJoiner) {
		t.Error("unsplit text somehow contains a joiner")
	}
}

func TestSplitConjunctsIsIdempotent(t *testing.T) {
	once := splitConjuncts(bengaliConjunct)
	twice := splitConjuncts(once)
	if once != twice {
		t.Errorf("applying twice changed the result: %d joiners then %d",
			strings.Count(once, string(conjunctJoiner)), strings.Count(twice, string(conjunctJoiner)))
	}
}

// TestConjunctSplittingIsDisplayOnly is the safety property: a name with
// a joiner spliced into it names no playlist MPD has ever heard of, so
// the transformation must never reach a value used for lookups.
func TestConjunctSplittingIsDisplayOnly(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{bengaliConjunct, "Thumri"})
	a.playlists.table.Select(playlistsHeaderRows, 0)

	if got := a.playlists.selectedName(); got != bengaliConjunct {
		t.Errorf("selectedName() = %q, want the untouched name %q", got, bengaliConjunct)
	}
	if strings.ContainsRune(a.playlists.selectedName(), conjunctJoiner) {
		t.Error("selectedName() returned a name with a display joiner in it")
	}
	// The rendered cell, by contrast, must carry it.
	cell := a.playlists.table.GetCell(playlistsHeaderRows, 0)
	if !strings.ContainsRune(cell.Text, conjunctJoiner) {
		t.Errorf("rendered cell %q has no joiner -- the row would still scramble", cell.Text)
	}
}

func TestConjunctSplittingReachesEveryTextSurface(t *testing.T) {
	// Each of these puts MPD-provided text on screen next to another
	// panel, so any one of them missing the treatment scrambles rows.
	song := mpdclient.Song{Title: bengaliConjunct, Artist: bengaliOneJoin, Album: bengaliConjunct, File: "x.mp3", Duration: 100}

	cases := map[string]string{
		"playlist name":  playlistDisplayName(bengaliConjunct),
		"library track":  trackLabel(song),
		"library folder": folderLabel(bengaliConjunct, false),
		"now playing":    nowPlayingTrackText(song),
		"queue cell":     cellText(bengaliConjunct, 40),
	}
	for name, got := range cases {
		if !strings.ContainsRune(got, conjunctJoiner) {
			t.Errorf("%s: %q has no joiner", name, got)
		}
	}
}

func TestCellTextStillTruncates(t *testing.T) {
	// The joiner must not eat into the truncation budget or push a
	// column over it.
	long := strings.Repeat("a", 60)
	if got := cellText(long, 20); len([]rune(got)) != 20 {
		t.Errorf("cellText(60 chars, 20) = %d runes, want 20", len([]rune(got)))
	}
	if got := cellText("short", 20); got != "short" {
		t.Errorf("cellText(short) = %q, want it unchanged", got)
	}
}
