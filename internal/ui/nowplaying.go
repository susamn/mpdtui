package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

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
		if a.queue != nil {
			cached, ok := a.queue.metaCache[song.File]
			if !ok || cached.Rating != meta.Rating || cached.PlayCount != meta.PlayCount || (cached.Mark == nil) != (meta.Mark == nil) || (cached.Mark != nil && meta.Mark != nil && *cached.Mark != *meta.Mark) {
				a.queue.applyTrackMeta(song.File, meta)
			}
		}
	}

	line2 += a.castStatusLine()

	a.nowPlaying.SetText(line1 + "\n" + line2)
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
	switch {
	case song.Artist != "" && song.Title != "":
		return fmt.Sprintf("[%s::b]%s[-:-:-] - [%s::b]%s[-:-:-]",
			nowPlayingTrackColor, song.Title, nowPlayingArtistColor, song.Artist)
	case song.Title != "":
		return fmt.Sprintf("[%s::b]%s[-:-:-]", nowPlayingTrackColor, song.Title)
	case song.File != "":
		return song.File
	default:
		return "(nothing playing)"
	}
}
