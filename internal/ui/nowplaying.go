package ui

import (
	"fmt"

	"mpdtui/internal/mpdclient"
)

func (a *App) renderNowPlaying(st mpdclient.Status, song mpdclient.Song) {
	track := song.DisplayName()
	if track == "" {
		track = "(nothing playing)"
	}
	bar := ProgressBar(st.Elapsed, st.Duration, 30)
	line1 := fmt.Sprintf("%s %s  [%s] %s/%s",
		StateGlyph(st.State), track, bar, FormatDuration(st.Elapsed), FormatDuration(st.Duration))
	line2 := fmt.Sprintf("vol %s%%   repeat %s   random %s   single %s   consume %s",
		VolText(st.Volume), OnOff(st.Repeat), OnOff(st.Random), OnOff(st.Single), OnOff(st.Consume))
	a.nowPlayingText.SetText(line1 + "\n" + line2)
}
