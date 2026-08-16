package ui

import (
	"fmt"
	"strings"

	"mpdtui/internal/mpdclient"
)

// nowPlayingBarColor matches internal/mini's own progress-bar color
// (ansiBarCyan, RGB(0,229,229)) -- explicit request to reuse it here too.
// nowPlayingRatingColor matches queueRatingColor's exact RGB (gold).
// nowPlayingTrackColor matches queueTitleColor's exact RGB (WhatsApp
// green). nowPlayingArtistColor matches tcell.ColorSkyblue's.
const (
	nowPlayingBarColor    = "#00E5E5"
	nowPlayingRatingColor = "#FFD700"
	nowPlayingTrackColor  = "#25D366"
	nowPlayingArtistColor = "#87CEEB"
)

func (a *App) renderNowPlaying(st mpdclient.Status, song mpdclient.Song) {
	bar := ProgressBar(st.Elapsed, st.Duration, 30)
	// "[[" escapes to a literal "[" (see tview's own doc.go on style
	// tags) -- without it, this constructs a real color tag starting
	// right where the bracket that used to just frame the bar visually
	// would be, silently swallowing it instead of showing it.
	line1 := fmt.Sprintf("[%s]%s[-] %s  [[%s]%s[-]] %s/%s",
		StateGlyphColor(st.State), StateGlyph(st.State), nowPlayingTrackText(song), nowPlayingBarColor, bar, FormatDuration(st.Elapsed), FormatDuration(st.Duration))

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
	}

	a.nowPlaying.SetText(line1 + "\n" + line2)
}

// nowPlayingTrackText mirrors mpdclient.Song.DisplayName's own
// Artist+Title / Title-only / File fallback chain, but colors the
// artist and title separately (bold sky blue / bold WhatsApp green)
// rather than returning one plain "Artist - Title" string -- explicit
// request. Artist/Title are also rendered fullwidth (toFullwidth) for
// visual size, since a terminal grid has no real font-size concept to
// reach for instead. The bare-File and nothing-playing fallbacks stay
// plain (normal width, uncolored), same as before.
func nowPlayingTrackText(song mpdclient.Song) string {
	switch {
	case song.Artist != "" && song.Title != "":
		return fmt.Sprintf("[%s::b]%s[-:-:-] - [%s::b]%s[-:-:-]",
			nowPlayingArtistColor, toFullwidth(song.Artist), nowPlayingTrackColor, toFullwidth(song.Title))
	case song.Title != "":
		return fmt.Sprintf("[%s::b]%s[-:-:-]", nowPlayingTrackColor, toFullwidth(song.Title))
	case song.File != "":
		return song.File
	default:
		return "(nothing playing)"
	}
}

// toFullwidth converts each plain-ASCII printable character (0x21-0x7E)
// to its Unicode "fullwidth" counterpart (U+FF01-U+FF5E, a fixed
// +0xFEE0 offset) and a plain space to the fullwidth space (U+3000) --
// most terminals render these roughly double-width, which is the
// closest a fixed character-cell terminal grid gets to "bigger text"
// (explicit request; tview/SGR style tags have no font-size concept at
// all). Runes outside plain ASCII (e.g. the "é" in "Céline Dion",
// already non-ASCII) pass through unchanged -- there's no equivalent
// fullwidth mapping for them via this same fixed-offset trick, so a
// name mixing ASCII and accented letters ends up rendering at two
// visually different widths. A known, explicitly-accepted tradeoff of
// this approach, not a bug.
func toFullwidth(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteRune('　') // fullwidth (ideographic) space
		case r >= '!' && r <= '~':
			b.WriteRune(r + 0xFEE0)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
