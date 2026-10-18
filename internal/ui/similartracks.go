package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
	"mpdtui/internal/textutil"
	"mpdtui/internal/uitheme"
)

const maxTrackGroupHints = 10

type trackGroupHints struct {
	songs     []mpdclient.Song
	labels    []string
	order     []int
	total     int
	highlight int
}

func (h *trackGroupHints) rebuild(query string, songs []mpdclient.Song, labels []string) {
	h.songs = songs
	h.labels = labels
	matched := fuzzyFilterSortIndex(query, labels)
	h.total = len(matched)
	if h.total > maxTrackGroupHints {
		matched = matched[:maxTrackGroupHints]
	}
	h.order = matched
	h.highlight = -1
	if len(h.order) > 0 {
		h.highlight = 0
	}
}

func (h *trackGroupHints) move(delta int) {
	h.highlight = moveHintHighlight(h.highlight, len(h.order), delta)
}

func (h *trackGroupHints) jumpFirst() {
	if len(h.order) > 0 {
		h.highlight = 0
	}
}

func (h *trackGroupHints) jumpLast() {
	if len(h.order) > 0 {
		h.highlight = len(h.order) - 1
	}
}

func (h *trackGroupHints) current() (mpdclient.Song, int) {
	if h.highlight < 0 || h.highlight >= len(h.order) {
		return mpdclient.Song{}, -1
	}
	idx := h.order[h.highlight]
	return h.songs[idx], idx
}

func normalizeTrackPath(file string) string {
	segments := strings.Split(file, "/")
	for i, seg := range segments {
		segments[i] = textutil.NormalizeSegment(seg)
	}
	return strings.Join(segments, "/")
}

func trackGroupItemLabel(label string, inGroup bool) string {
	const pad = "  "
	if inGroup {
		return pad + "[green]✓[-]  " + label
	}
	return pad + "   " + label
}

// handleGroupTrack handles 'u': fuzzy search library tracks to group with currently playing track.
func (a *App) handleGroupTrack() {
	if a.currentSong.File == "" {
		a.flash("[red]no track is currently playing[-]")
		return
	}
	if a.metaDB == nil {
		a.flash("[red]track metadata database is not enabled[-]")
		return
	}
	a.openTrackGroupingPicker()
}

func (a *App) openTrackGroupingPicker() {
	all, err := a.client.AllSongs()
	if err != nil {
		a.showError(err)
		return
	}
	a.cacheSongs(all)

	currentFile := a.currentSong.File
	currentNorm := normalizeTrackPath(currentFile)

	alreadyGrouped, _ := a.metaDB.GetSimilarTracks(currentFile)
	groupedSet := make(map[string]bool, len(alreadyGrouped)*2)
	for _, t := range alreadyGrouped {
		groupedSet[t.RealPath] = true
		groupedSet[t.NormalizedPath] = true
		groupedSet[normalizeTrackPath(t.RealPath)] = true
	}

	var candidateSongs []mpdclient.Song
	var candidateLabels []string
	for _, s := range all {
		if s.File == currentFile || normalizeTrackPath(s.File) == currentNorm {
			continue
		}
		candidateSongs = append(candidateSongs, s)
		label := s.DisplayName()
		if s.Album != "" {
			label += " [" + s.Album + "]"
		}
		candidateLabels = append(candidateLabels, label)
	}

	field := tview.NewInputField().SetLabel(" Search track: ").SetFieldWidth(40)
	field.SetBorder(true).SetTitle(" Group similar track with: \"" + a.currentSong.DisplayName() + "\" ")

	list := tview.NewList()
	list.ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetSelectedTextColor(uitheme.SelectedFg())
	list.SetSelectedBackgroundColor(uitheme.SelectedBg())
	list.SetBorder(true)

	hints := &trackGroupHints{}

	syncHighlight := func() {
		if hints.highlight >= 0 && hints.highlight < list.GetItemCount() {
			list.SetCurrentItem(hints.highlight)
		}
	}

	renderList := func() {
		hints.rebuild(field.GetText(), candidateSongs, candidateLabels)
		list.Clear()
		for _, idx := range hints.order {
			s := candidateSongs[idx]
			inGroup := groupedSet[s.File] || groupedSet[normalizeTrackPath(s.File)]
			list.AddItem(trackGroupItemLabel(hints.labels[idx], inGroup), "", 0, nil)
		}
		syncHighlight()
		switch {
		case len(candidateSongs) == 0:
			list.SetTitle(" no other tracks in library ")
		case hints.total == 0:
			list.SetTitle(" no matching tracks ")
		default:
			list.SetTitle(fmt.Sprintf(" %d matching track(s) (Tab: switch focus, Enter: group, Esc: close) ", hints.total))
		}
	}
	field.SetChangedFunc(func(string) { renderList() })

	confirm := func() {
		target, idx := hints.current()
		if idx < 0 {
			return
		}
		a.closeOverlay()
		if err := a.metaDB.GroupTracks(a.currentSong.File, target.File); err != nil {
			a.showError(err)
			return
		}
		a.showMessage("grouped \"" + target.DisplayName() + "\" with \"" + a.currentSong.DisplayName() + "\"")
	}

	focusList := func() {
		field.SetBorderColor(uitheme.InactiveBorder()).SetTitleColor(uitheme.InactiveBorder())
		list.SetBorderColor(uitheme.ActiveBorder()).SetTitleColor(uitheme.ActiveBorder())
		a.tv.SetFocus(list)
	}
	focusField := func() {
		list.SetBorderColor(uitheme.InactiveBorder()).SetTitleColor(uitheme.InactiveBorder())
		field.SetBorderColor(uitheme.ActiveBorder()).SetTitleColor(uitheme.ActiveBorder())
		a.tv.SetFocus(field)
	}

	field.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN:
			hints.move(1)
			syncHighlight()
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			hints.move(-1)
			syncHighlight()
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			focusList()
			return nil
		case tcell.KeyEnter:
			confirm()
			return nil
		}
		return event
	})

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN:
			hints.move(1)
			syncHighlight()
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			hints.move(-1)
			syncHighlight()
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			focusField()
			return nil
		case tcell.KeyEnter:
			confirm()
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'j':
				hints.move(1)
				syncHighlight()
				return nil
			case 'k':
				hints.move(-1)
				syncHighlight()
				return nil
			case 'g':
				hints.jumpFirst()
				syncHighlight()
				return nil
			case 'G':
				hints.jumpLast()
				syncHighlight()
				return nil
			case 'f':
				focusField()
				return nil
			}
		}
		return event
	})

	renderList()
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(field, 3, 0, true).
		AddItem(list, 0, 1, false)

	// Focus field initially and apply active yellow border
	focusField()
	a.showOverlay("group-track", centered(layout, 72, 3+maxTrackGroupHints+2), field)
}

// handlePlaySimilarTrack handles 'U': view and play similar recordings of currently playing track.
func (a *App) handlePlaySimilarTrack() {
	if a.currentSong.File == "" {
		a.flash("[red]no track is currently playing[-]")
		return
	}
	if a.metaDB == nil {
		a.flash("[red]track metadata database is not enabled[-]")
		return
	}
	a.openSimilarTracksPicker()
}

func (a *App) openSimilarTracksPicker() {
	currentFile := a.currentSong.File
	currentName := a.currentSong.DisplayName()

	similar, err := a.metaDB.GetSimilarTracks(currentFile)
	if err != nil {
		a.showError(err)
		return
	}
	if len(similar) == 0 {
		a.flash("no similar tracks grouped for: " + currentName)
		return
	}

	all, _ := a.client.AllSongs()
	a.cacheSongs(all)
	songByFile := make(map[string]mpdclient.Song, len(all))
	for _, s := range all {
		songByFile[s.File] = s
	}

	buildSongs := func() []mpdclient.Song {
		items := make([]mpdclient.Song, len(similar))
		for i, t := range similar {
			if s, ok := songByFile[t.RealPath]; ok {
				items[i] = s
			} else {
				items[i] = mpdclient.Song{File: t.RealPath}
			}
		}
		return items
	}

	similarSongs := buildSongs()

	list := tview.NewList()
	list.ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetSelectedTextColor(uitheme.SelectedFg())
	list.SetSelectedBackgroundColor(uitheme.SelectedBg())
	list.SetBorder(true)
	list.SetTitle(" Similar recordings of: \"" + currentName + "\" (Enter: play, d: unlink, Esc/U: close) ")
	activateOverlayBorder(list)

	populateList := func() {
		list.Clear()
		for _, s := range similarSongs {
			label := s.DisplayName()
			if s.Album != "" {
				label += " [" + s.Album + "]"
			}
			list.AddItem("  "+label, "", 0, nil)
		}
	}
	populateList()

	list.SetSelectedFunc(func(idx int, _, _ string, _ rune) {
		if idx >= 0 && idx < len(similarSongs) {
			picked := similarSongs[idx]
			a.closeOverlay()
			a.addAndPlay(picked)
		}
	})

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			a.closeOverlay()
			return nil
		case tcell.KeyDown, tcell.KeyCtrlN:
			list.SetCurrentItem(moveHintHighlight(list.GetCurrentItem(), list.GetItemCount(), 1))
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			list.SetCurrentItem(moveHintHighlight(list.GetCurrentItem(), list.GetItemCount(), -1))
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'j':
				list.SetCurrentItem(moveHintHighlight(list.GetCurrentItem(), list.GetItemCount(), 1))
				return nil
			case 'k':
				list.SetCurrentItem(moveHintHighlight(list.GetCurrentItem(), list.GetItemCount(), -1))
				return nil
			case 'g':
				if list.GetItemCount() > 0 {
					list.SetCurrentItem(0)
				}
				return nil
			case 'G':
				if n := list.GetItemCount(); n > 0 {
					list.SetCurrentItem(n - 1)
				}
				return nil
			case 'U':
				a.closeOverlay()
				return nil
			case 'd':
				idx := list.GetCurrentItem()
				if idx >= 0 && idx < len(similarSongs) {
					target := similarSongs[idx]
					if err := a.metaDB.UnlinkFromGroup(target.File); err != nil {
						a.showError(err)
						return nil
					}
					// Refresh list
					similar, _ = a.metaDB.GetSimilarTracks(currentFile)
					if len(similar) == 0 {
						a.closeOverlay()
						a.showMessage("unlinked \"" + target.DisplayName() + "\", no grouped tracks remaining")
						return nil
					}
					similarSongs = buildSongs()
					populateList()
					if idx >= len(similarSongs) {
						list.SetCurrentItem(len(similarSongs) - 1)
					}
					a.showMessage("unlinked \"" + target.DisplayName() + "\" from group")
				}
				return nil
			}
		}
		return event
	})

	height := len(similarSongs) + 2
	if height < 5 {
		height = 5
	}
	if height > 16 {
		height = 16
	}

	a.showOverlay("similar-tracks", centered(list, 72, height), list)
}
