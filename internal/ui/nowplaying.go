package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

// nowPlayingBarColor mirrors queueRatingColor/queueTitleColor's own
// theme fields (Cyan/Yellow/Green respectively) so the same concept
// reads as the same color across panels; nowPlayingArtistColor uses
// Blue. nowPlayingBorderColor is the Now Playing panel's own (always-
// on, not focus-driven) border/title color. All theme-derived
// (deriveColors), see theme.go.
var (
	nowPlayingBarColor    string
	nowPlayingRatingColor string
	nowPlayingTrackColor  string
	nowPlayingArtistColor string
	nowPlayingBorderColor tcell.Color
)

// The Now Playing panel is given exactly 4 rows in the root layout
// (app.go's build()), i.e. 2 rows inside its border -- one per line.
// With wrapping left on (TextView's default), a long "Title - Artist"
// on a narrow terminal spilled line 1 onto a second row and pushed
// line 2 (vol/flags/rating) out of the panel entirely. Wrapping is now
// off (app.go), so nothing can ever spill, and line 1 sizes itself to
// the panel's real width instead of being clipped mid-title: the bar
// gives up cells first (down to nowPlayingBarMinWidth), then the track
// text is truncated with an ellipsis.
const (
	// nowPlayingBarWidth is the progress bar's full, unsqueezed width
	// (what it always was before any of this).
	nowPlayingBarWidth = 30
	// nowPlayingBarMinWidth is how short the bar may get before the
	// track text starts paying instead -- below this it stops reading
	// as a progress indicator at all.
	nowPlayingBarMinWidth = 8
	// nowPlayingTrackMinWidth is the track text's protected share: the
	// bar only shrinks once the text is down to this, and below it the
	// "Title - Artist" pair collapses to the title alone (there is no
	// room left to show both halves meaningfully).
	nowPlayingTrackMinWidth = 12
	// nowPlayingLine1Fixed is line 1's non-negotiable furniture: the
	// state glyph (2 -- ⏸ is double-width in some terminals), the space
	// after it, the two spaces before the bar, the bar's own "[" and
	// "]", and the space before the elapsed/duration pair.
	nowPlayingLine1Fixed = 8
)

func (a *App) renderNowPlaying(st mpdclient.Status, song mpdclient.Song) {
	times := FormatDuration(st.Elapsed) + "/" + FormatDuration(st.Duration)
	// GetInnerRect is 0 until the panel has been drawn once (and in
	// tests, which never draw) -- nowPlayingLine1Layout reads that as
	// "unknown width" and returns the full bar with no truncation, the
	// pre-existing behaviour.
	width := 0
	if a.nowPlaying != nil {
		_, _, width, _ = a.nowPlaying.GetInnerRect()
	}
	barWidth, trackWidth := nowPlayingLine1Layout(width, len([]rune(times)))

	bar := ProgressBar(st.Elapsed, st.Duration, barWidth)
	// "[[" escapes to a literal "[" (see tview's own doc.go on style
	// tags) -- without it, this constructs a real color tag starting
	// right where the bracket that used to just frame the bar visually
	// would be, silently swallowing it instead of showing it.
	line1 := fmt.Sprintf("[%s]%s[-] %s  [[%s]%s[-]] %s",
		StateGlyphColor(st.State), StateGlyph(st.State), nowPlayingTrackTextWidth(song, trackWidth), nowPlayingBarColor, bar, times)

	line2 := fmt.Sprintf("vol %s   %s   %s   %s   %s",
		VolumeText(st.Volume),
		FlagText("repeat", st.Repeat),
		FlagText("random", st.Random),
		FlagText("single", st.Single),
		FlagText("consume", st.Consume),
	)
	if a.metaDB != nil && song.File != "" {
		meta, _ := a.metaDB.Get(song.File) // zero-opinion Track (not an error) if unrated/unplayed yet
		line2 += fmt.Sprintf("   rating [%s]%s[-]   played %dx", nowPlayingRatingColor, ratingStars(meta.Rating), meta.PlayCount)
		if a.queue != nil {
			cached, ok := a.queue.metaCache[song.File]
			if !ok || cached.Rating != meta.Rating || cached.PlayCount != meta.PlayCount || !sameMarks(cached.Marks, meta.Marks) {
				a.queue.applyTrackMeta(song.File, meta)
			}
		}
	}

	a.nowPlaying.SetText(line1 + "\n" + line2)
}

// nowPlayingLine1Layout splits an inner width of width cells between the
// progress bar and the track text, given a times ("1:23/4:56") of
// timesWidth cells. A width of 0 means "not laid out yet" (see
// renderNowPlaying) and yields the full bar plus an unbounded (0) track
// budget, so nothing is truncated on a width we do not actually know.
//
// The bar is what gives first: it shrinks from nowPlayingBarWidth only
// once the track text is down to nowPlayingTrackMinWidth, and stops at
// nowPlayingBarMinWidth, after which any further squeeze comes out of
// the track text. On a terminal too narrow even for that, both sit at
// their minimum and tview clips the overflow -- which is now merely
// ugly rather than layout-breaking, since wrapping is off.
func nowPlayingLine1Layout(width, timesWidth int) (barWidth, trackWidth int) {
	if width <= 0 {
		return nowPlayingBarWidth, 0
	}
	avail := width - nowPlayingLine1Fixed - timesWidth
	if avail < nowPlayingBarMinWidth+1 {
		return nowPlayingBarMinWidth, 1
	}
	barWidth = nowPlayingBarWidth
	if slack := avail - nowPlayingTrackMinWidth; slack < barWidth {
		barWidth = slack
	}
	if barWidth < nowPlayingBarMinWidth {
		barWidth = nowPlayingBarMinWidth
	}
	return barWidth, avail - barWidth
}

// nowPlayingTrackText mirrors mpdclient.Song.DisplayName's own
// Artist+Title fallback chain, but shows "Title - Artist" (not
// DisplayName's "Artist - Title" order -- explicit correction) and
// colors the two halves separately (bold WhatsApp green / bold sky
// blue) rather than returning one plain string. The bare-File and
// nothing-playing fallbacks stay uncolored, same as before. (An earlier
// version of this also rendered the text fullwidth for visual size;
// reverted -- explicitly disliked.)
func nowPlayingTrackText(song mpdclient.Song) string {
	return nowPlayingTrackTextWidth(song, 0)
}

// nowPlayingTrackTextWidth is nowPlayingTrackText fitted to max visible
// cells (max <= 0 means unbounded). Truncation always happens on the
// plain tag text, before the style tags are wrapped around it and before
// splitConjuncts inserts its zero-width joiners -- so a cut can never
// land inside a "[green::b]" tag and leave tview parsing a broken one,
// and the joiners (zero width on screen, but extra runes) never eat into
// the budget.
func nowPlayingTrackTextWidth(song mpdclient.Song, max int) string {
	switch {
	case song.Artist != "" && song.Title != "":
		// Too narrow to show both halves plus the " - " between them:
		// drop the artist rather than render two useless stubs.
		if max > 0 && max < nowPlayingTrackMinWidth {
			return fmt.Sprintf("[%s::b]%s[-:-:-]", nowPlayingTrackColor, splitConjuncts(nowPlayingClamp(song.Title, max)))
		}
		title, artist := song.Title, song.Artist
		if max > 0 {
			title, artist = nowPlayingSplitBudget(title, artist, max-3) // 3 == len(" - ")
		}
		return fmt.Sprintf("[%s::b]%s[-:-:-] - [%s::b]%s[-:-:-]",
			nowPlayingTrackColor, splitConjuncts(title), nowPlayingArtistColor, splitConjuncts(artist))
	case song.Title != "":
		return fmt.Sprintf("[%s::b]%s[-:-:-]", nowPlayingTrackColor, splitConjuncts(nowPlayingClamp(song.Title, max)))
	case song.File != "":
		return splitConjuncts(nowPlayingClamp(song.File, max))
	default:
		return nowPlayingClamp("(nothing playing)", max)
	}
}

// nowPlayingSplitBudget divides budget runes between a title and an
// artist. The title gets 3/5 of it (it is the more identifying half),
// but whichever side is shorter than its share hands the slack to the
// other, so "Om - Some Very Long Artist Name" does not truncate the
// artist while leaving the title's share unused.
func nowPlayingSplitBudget(title, artist string, budget int) (string, string) {
	tr, ar := len([]rune(title)), len([]rune(artist))
	if budget <= 0 || tr+ar <= budget {
		return title, artist
	}
	tw := budget * 3 / 5
	aw := budget - tw
	if tr < tw {
		aw += tw - tr
		tw = tr
	} else if ar < aw {
		tw += aw - ar
		aw = ar
	}
	return nowPlayingClamp(title, tw), nowPlayingClamp(artist, aw)
}

// nowPlayingClamp is truncateWithEllipsis with an unbounded case
// (max <= 0, for a width that is not known yet) and a guard for budgets
// too small to hold the "..." itself, which truncateWithEllipsis would
// slice negatively.
func nowPlayingClamp(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return truncateWithEllipsis(s, max)
}

// sameMarks reports whether two mark sets are equal. Both come from
// marksForTrack, which orders by catalog id, so comparing in order is
// enough -- no set logic needed.
func sameMarks(a, b []metadata.MarkReason) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
