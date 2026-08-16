package ui

import (
	"fmt"

	"mpdtui/internal/mpdclient"
)

// nowPlayingBarColor matches internal/mini's own progress-bar color
// (ansiBarCyan, RGB(0,229,229)) -- explicit request to reuse it here too.
// nowPlayingRatingColor matches queueRatingColor's exact RGB (gold).
const (
	nowPlayingBarColor    = "#00E5E5"
	nowPlayingRatingColor = "#FFD700"
)

func (a *App) renderNowPlaying(st mpdclient.Status, song mpdclient.Song) {
	track := song.DisplayName()
	if track == "" {
		track = "(nothing playing)"
	}
	bar := ProgressBar(st.Elapsed, st.Duration, 30)
	// "[[" escapes to a literal "[" (see tview's own doc.go on style
	// tags) -- without it, this constructs a real color tag starting
	// right where the bracket that used to just frame the bar visually
	// would be, silently swallowing it instead of showing it.
	line1 := fmt.Sprintf("%s %s  [[%s]%s[-]] %s/%s",
		StateGlyph(st.State), track, nowPlayingBarColor, bar, FormatDuration(st.Elapsed), FormatDuration(st.Duration))

	line2 := fmt.Sprintf("vol %s   %s   %s   %s   %s",
		VolumeText(st.Volume),
		FlagText("repeat", st.Repeat),
		FlagText("random", st.Random),
		FlagText("single", st.Single),
		FlagText("consume", st.Consume),
	)
	if a.metaDB != nil && song.File != "" {
		meta, _ := a.metaDB.Get(song.File) // zero-opinion Track (not an error) if unrated yet
		line2 += fmt.Sprintf("   rating [%s]%s[-]", nowPlayingRatingColor, ratingStars(meta.Rating))
	}

	a.nowPlaying.SetText(line1 + "\n" + line2)
}
