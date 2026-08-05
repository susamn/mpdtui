// Package ui implements the full, lazygit-style panel TUI for mpdtui.
package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/qeesung/image2ascii/convert"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

const (
	modeNormal = iota
	modeOverlay
)

// flashDuration is how long a transient hint-bar message (an error, a
// "added N tracks" confirmation, etc.) stays up before the hint bar
// reverts to showing keybindings again.
const flashDuration = 3 * time.Second

// App is the full panel-based TUI application.
type App struct {
	tv      *tview.Application
	client  *mpdclient.Client
	watcher *mpdclient.Watcher

	pages *tview.Pages
	root  *tview.Flex

	library   *libraryPanel
	playlists *playlistsPanel
	queue     *queuePanel

	nowPlaying *tview.TextView
	hintBar    *tview.TextView
	albumArt      *tview.TextView
	currentArtURI string
	kittyPNG      []byte

	panels   []tview.Primitive
	panelIdx int

	mode               int
	beforeOverlayFocus tview.Primitive
	closeOverlay       func()

	msgSeq int

	done chan struct{}
}

// Run connects a Watcher and runs the full TUI until the user quits or an
// unrecoverable error occurs. Blocks until the application exits.
func Run(client *mpdclient.Client) error {
	a := &App{
		tv:     tview.NewApplication(),
		client: client,
		done:   make(chan struct{}),
	}

	w, err := client.Watch("player", "mixer", "options", "playlist", "stored_playlist")
	if err != nil {
		return fmt.Errorf("watch mpd: %w", err)
	}
	a.watcher = w
	defer func() {
		close(a.done)
		w.Close()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			a.tv.Stop()
		case <-a.done:
		}
	}()

	a.build()
	a.refreshAll()
	go a.eventLoop()

	return a.tv.Run()
}

func (a *App) build() {
	applyTheme()

	a.library = newLibraryPanel(a)
	a.playlists = newPlaylistsPanel(a)
	a.queue = newQueuePanel(a)

	wireFocusColors(a.library.list)
	wireFocusColors(a.playlists.list)
	wireFocusColors(a.queue.table)
	wireFocusColors(a.queue.search)

	a.nowPlaying = tview.NewTextView().SetDynamicColors(true)
	a.nowPlaying.SetBorder(true).SetTitle(" Now Playing ").SetTitleColor(tcell.ColorYellow)
	a.nowPlaying.SetBorderColor(tcell.ColorYellow)

	a.albumArt = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	a.albumArt.SetBorder(true).SetTitle(" Album Art ")
	a.albumArt.SetText("\n\n[::d]Album Art Loading...[-:-:-]")

	a.hintBar = tview.NewTextView().SetDynamicColors(true)

	bottomLeft := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(a.albumArt, 0, 1, false).
		AddItem(a.playlists.list, 0, 1, false)

	left := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.library.list, 0, 2, true).
		AddItem(bottomLeft, 0, 1, false)

	queueBox := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.queue.search, 3, 0, false).
		AddItem(a.queue.table, 0, 1, true)

	main := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(left, 0, 1, true).
		AddItem(queueBox, 0, 2, false)

	a.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(main, 0, 1, true).
		AddItem(a.nowPlaying, 4, 0, false).
		AddItem(a.hintBar, 1, 0, false)

	a.panels = []tview.Primitive{a.library.list, a.playlists.list, a.queue.table}

	a.pages = tview.NewPages().AddPage("main", a.root, true, true)

	a.tv.SetInputCapture(a.globalInputCapture)
	a.tv.SetRoot(a.pages, true).SetFocus(a.library.list)
	a.updateHintBar()

	a.tv.SetAfterDrawFunc(func(screen tcell.Screen) {
		if len(a.kittyPNG) > 0 && strings.Contains(os.Getenv("TERM"), "kitty") {
			x, y, w, h := a.albumArt.GetInnerRect()
			if w == 0 || h == 0 {
				return
			}
			// Move cursor to panel content area
			fmt.Printf("\033[%d;%dH", y+1, x+1)
			
			b64 := base64.StdEncoding.EncodeToString(a.kittyPNG)
			chunkSize := 4096
			for i := 0; i < len(b64); i += chunkSize {
				end := i + chunkSize
				m := 1
				if end >= len(b64) {
					end = len(b64)
					m = 0
				}
				if i == 0 {
					fmt.Printf("\033_Ga=T,f=100,q=2,c=%d,r=%d,m=%d;%s\033\\", w, h, m, b64[i:end])
				} else {
					fmt.Printf("\033_Gm=%d;%s\033\\", m, b64[i:end])
				}
			}
		}
	})
}

func (a *App) refreshAll() {
	a.library.showArtists()
	a.playlists.refresh()
	a.queue.refresh()
	a.refreshNowPlaying()
}

func (a *App) eventLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case name, ok := <-a.watcher.Events():
			if !ok {
				return
			}
			a.tv.QueueUpdateDraw(func() { a.handleSubsystem(name) })
		case _, ok := <-a.watcher.Errors():
			if !ok {
				return
			}
			// The idle connection is dead. This is a personal single-user
			// tool; rather than reconnect-looping forever, surface it and
			// let ticker-driven polling keep the Now Playing bar alive.
			a.tv.QueueUpdateDraw(func() { a.showError(fmt.Errorf("lost MPD event connection")) })
			return
		case <-ticker.C:
			a.tv.QueueUpdateDraw(func() { a.refreshNowPlaying() })
		case <-a.done:
			return
		}
	}
}

func (a *App) handleSubsystem(name string) {
	switch name {
	case "player", "mixer", "options":
		a.refreshNowPlaying()
	case "playlist":
		a.queue.refresh()
	case "stored_playlist":
		a.playlists.refresh()
	}
}

func (a *App) refreshNowPlaying() {
	st, err := a.client.Status()
	if err != nil {
		a.showError(err)
		return
	}
	song, err := a.client.CurrentSong()
	if err != nil {
		a.showError(err)
		return
	}
	a.renderNowPlaying(st, song)
	a.queue.setCurrent(st.SongID)

	if song.File != "" && song.File != a.currentArtURI {
		a.currentArtURI = song.File
		go a.updateAlbumArt(song.File)
	}
}

func (a *App) updateAlbumArt(uri string) {
	b, err := a.client.FetchAlbumArt(uri)
	if err != nil || len(b) == 0 {
		a.kittyPNG = nil
		a.tv.QueueUpdateDraw(func() {
			a.albumArt.Clear()
			a.albumArt.SetText("\n\n[::d]No Album Art[-:-:-]")
		})
		return
	}

	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		a.kittyPNG = nil
		a.tv.QueueUpdateDraw(func() {
			a.albumArt.Clear()
			a.albumArt.SetText("\n\n[::d]Decode Error[-:-:-]")
		})
		return
	}

	if strings.Contains(os.Getenv("TERM"), "kitty") {
		var buf bytes.Buffer
		png.Encode(&buf, img)
		a.kittyPNG = buf.Bytes()
		a.tv.QueueUpdateDraw(func() {
			a.albumArt.Clear() // clears the panel so we can draw the image over it
		})
		return
	}

	a.kittyPNG = nil
	opts := convert.DefaultOptions
	opts.FixedWidth = 30
	opts.FixedHeight = 15

	converter := convert.NewImageConverter()
	asciiStr := converter.Image2ASCIIString(img, &opts)

	a.tv.QueueUpdateDraw(func() {
		a.albumArt.Clear()
		ansiWriter := tview.ANSIWriter(a.albumArt)
		fmt.Fprint(ansiWriter, asciiStr)
	})
}

func (a *App) cycleFocus(delta int) {
	a.panelIdx = (a.panelIdx + delta + len(a.panels)) % len(a.panels)
	a.tv.SetFocus(a.panels[a.panelIdx])
	a.updateHintBar()
}

func (a *App) focusPanel(i int) {
	if i < 0 || i >= len(a.panels) {
		return
	}
	a.panelIdx = i
	a.tv.SetFocus(a.panels[i])
	a.updateHintBar()
}

func (a *App) focusPanelPrimitive(p tview.Primitive) {
	for i, pp := range a.panels {
		if pp == p {
			a.panelIdx = i
		}
	}
	a.tv.SetFocus(p)
	a.updateHintBar()
}

func (a *App) showError(err error) {
	a.flash("[red]error: " + err.Error() + "[-]")
}

func (a *App) showMessage(msg string) {
	a.flash("[yellow]" + msg + "[-]")
}

// flash shows text in the hint bar, then reverts to the keybinding hints
// after flashDuration -- unless a newer flash (or a focus change, which
// calls updateHintBar directly) has already superseded it.
func (a *App) flash(text string) {
	a.msgSeq++
	seq := a.msgSeq
	a.hintBar.SetText(text)
	time.AfterFunc(flashDuration, func() {
		a.tv.QueueUpdateDraw(func() {
			if a.msgSeq == seq {
				a.updateHintBar()
			}
		})
	})
}

func (a *App) updateHintBar() {
	var panelHints string
	switch a.tv.GetFocus() {
	case a.library.list:
		panelHints = "Enter:open/play  a:add  Bksp:back"
		if a.library.level == libSearch {
			panelHints += "  Esc:clear search"
		}
	case a.playlists.list:
		panelHints = "Enter:load+play  a:append  d:delete  S:save queue"
		if a.playlists.filter != "" {
			panelHints += "  Esc:clear filter"
		}
	case a.queue.table:
		panelHints = "Enter:play  d:remove  J/K:move"
	}
	global := "Space:play/pause  s:stop  n/p:next/prev  ,/.:seek  -/=:vol  z:shuffle  x:repeat  D:clear queue  /:search  ?:help  Tab/1-3:panels  q:quit"
	a.hintBar.SetText("[::b]" + panelHints + "[-:-:-]  |  " + global)
}

func (a *App) addAndPlay(song mpdclient.Song) {
	id, err := a.client.QueueAddID(song.File)
	if err != nil {
		a.showError(err)
		return
	}
	if err := a.client.PlayID(id); err != nil {
		a.showError(err)
		return
	}
	a.queue.refresh()
	a.refreshNowPlaying()
}

func (a *App) queueAddSongs(songs []mpdclient.Song) {
	if len(songs) == 0 {
		return
	}
	for _, s := range songs {
		if err := a.client.QueueAdd(s.File); err != nil {
			a.showError(err)
			return
		}
	}
	a.queue.refresh()
	a.showMessage(fmt.Sprintf("added %d track(s) to queue", len(songs)))
}

func (a *App) loadPlaylist(name string) {
	if err := a.client.PlaylistLoad(name); err != nil {
		a.showError(err)
		return
	}
	a.queue.refresh()
	a.refreshNowPlaying()
	a.showMessage("loaded playlist " + name)
}
