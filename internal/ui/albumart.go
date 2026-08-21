package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"
	"sync"

	"github.com/nfnt/resize"
	"github.com/rivo/tview"
)

// albumArtPanel renders the currently playing track's art, either as a
// real image (Kitty graphics protocol, when supported) or as ASCII art
// (everywhere else). The Kitty path writes raw escape sequences directly
// to the terminal, outside tview/tcell's own screen model entirely --
// neither has any notion of pixel graphics -- so mu/seq/sentSig exist
// specifically to keep that safe: seq guards a slow fetch from overwriting
// a newer one, and all direct terminal writes happen from a single place
// (draw, invoked from App.build's SetAfterDrawFunc) so they never overlap
// tview's own single-threaded draw cycle or each other.
type albumArtPanel struct {
	app  *App
	view *tview.TextView

	mu       sync.Mutex
	seq      int    // bumped on every track change; a fetch checks this before applying its result
	kittyPNG []byte // set only when supportsKittyGraphics() and decode+encode succeeded

	currentURI string // main-goroutine-only (set from onTrackChanged, read from draw)
	sentSig    string // main-goroutine-only: signature of the last frame actually transmitted
}

func newAlbumArtPanel(app *App) *albumArtPanel {
	v := tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	v.SetBorder(true).SetTitle(" Album Art ")
	v.SetText("\n\n[::d]Album Art Loading...[-:-:-]")
	return &albumArtPanel{app: app, view: v}
}

// supportsKittyGraphics reports whether the terminal is likely to
// understand the Kitty graphics protocol. TERM containing "kitty" catches
// the common case; KITTY_WINDOW_ID is set by kitty itself and stays set
// even when TERM is overridden to something like xterm-256color (a common
// workaround for remote hosts lacking kitty's terminfo entry), which TERM
// alone would miss. Terminals that implement the protocol without either
// signal (e.g. some WezTerm/Konsole configurations) still fall back to
// ASCII -- there's no reliable capability-query path without risking a
// hang against terminals that don't answer it.
func supportsKittyGraphics() bool {
	return strings.Contains(os.Getenv("TERM"), "kitty") || os.Getenv("KITTY_WINDOW_ID") != ""
}

// onTrackChanged kicks off a background fetch for uri's album art, unless
// it's already the current track.
func (p *albumArtPanel) onTrackChanged(uri string) {
	seq, ok := p.startFetch(uri)
	if !ok {
		return
	}
	go p.fetch(uri, seq)
}

// startFetch records uri as the current track and bumps the sequence
// number, unless uri is already current (or empty). Returns the new
// sequence number and whether a fetch should actually be started. Split
// out from onTrackChanged so this bookkeeping is unit-testable without
// spawning a goroutine that touches the network.
func (p *albumArtPanel) startFetch(uri string) (seq int, ok bool) {
	if uri == "" || uri == p.currentURI {
		return 0, false
	}
	p.currentURI = uri

	p.mu.Lock()
	p.seq++
	seq = p.seq
	p.mu.Unlock()
	return seq, true
}

// isCurrent reports whether seq is still the most recent fetch started --
// false means a newer track change has superseded it and its result
// should be discarded.
func (p *albumArtPanel) isCurrent(seq int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return seq == p.seq
}

// setKittyPNGIfCurrent stores data as the Kitty-protocol image for this
// fetch, unless a newer track change has since started. Returns whether it
// was actually applied. The check-and-set happens under one lock so there's
// no gap between "is this still current" and "apply the result" for a
// newer onTrackChanged to land in.
func (p *albumArtPanel) setKittyPNGIfCurrent(seq int, data []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if seq != p.seq {
		return false
	}
	p.kittyPNG = data
	return true
}

func (p *albumArtPanel) fetch(uri string, seq int) {
	b, err := p.app.client.FetchAlbumArt(uri)
	if err != nil || len(b) == 0 {
		p.setKittyPNGIfCurrent(seq, nil)
		p.app.tv.QueueUpdateDraw(func() {
			if !p.isCurrent(seq) {
				return
			}
			p.view.Clear()
			p.view.SetText("\n\n[::d]No Album Art[-:-:-]")
		})
		return
	}

	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		p.setKittyPNGIfCurrent(seq, nil)
		p.app.tv.QueueUpdateDraw(func() {
			if !p.isCurrent(seq) {
				return
			}
			p.view.Clear()
			p.view.SetText("\n\n[::d]Decode Error[-:-:-]")
		})
		return
	}

	if supportsKittyGraphics() {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			p.setKittyPNGIfCurrent(seq, nil)
			p.app.tv.QueueUpdateDraw(func() {
				if !p.isCurrent(seq) {
					return
				}
				p.view.Clear()
				p.view.SetText("\n\n[::d]Encode Error[-:-:-]")
			})
			return
		}
		p.setKittyPNGIfCurrent(seq, buf.Bytes())
		p.app.tv.QueueUpdateDraw(func() {
			if !p.isCurrent(seq) {
				return
			}
			p.view.Clear() // clears the panel's text so draw()'s image isn't obscured
		})
		return
	}

	p.setKittyPNGIfCurrent(seq, nil)

	// Fallback to high-resolution ASCII using Unicode half-blocks and true color
	asciiStr := imageToHalfBlocks(img, 30, 15)

	p.app.tv.QueueUpdateDraw(func() {
		if !p.isCurrent(seq) {
			return
		}
		p.view.Clear()
		fmt.Fprint(tview.ANSIWriter(p.view), asciiStr)
	})
}

// draw writes (or clears) the Kitty-protocol image for the current frame.
// Called once per screen redraw from App.build's SetAfterDrawFunc, which
// tview always invokes synchronously as part of its own single-threaded
// draw cycle -- the only reason it's safe for this to write raw escape
// sequences straight to the terminal without racing tview/tcell's own
// output. Retransmits only when the image or the panel's size actually
// changed (sentSig), and explicitly deletes the previously-drawn image
// (a=d) when there's no longer one to show -- tview's own Clear() only
// touches its text buffer, it has no idea a Kitty image is separately
// composited on top, so without this the old art would stay ghosted on
// screen after switching to a track with no art.
func (p *albumArtPanel) draw() {
	if !supportsKittyGraphics() {
		return
	}

	p.mu.Lock()
	data := p.kittyPNG
	p.mu.Unlock()

	if len(data) == 0 {
		if p.sentSig != "" {
			fmt.Print("\033_Ga=d\033\\")
			p.sentSig = ""
		}
		return
	}

	x, y, w, h := p.view.GetInnerRect()
	if w == 0 || h == 0 {
		return
	}

	sig := fmt.Sprintf("%s:%d:%d:%d", p.currentURI, len(data), w, h)
	if p.sentSig == sig {
		return
	}
	p.sentSig = sig

	fmt.Printf("\033[%d;%dH", y+1, x+1)
	b64 := base64.StdEncoding.EncodeToString(data)
	const chunkSize = 4096
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

// imageToHalfBlocks converts an image to a high-resolution terminal string
// using Unicode half-blocks (▀) and ANSI true color sequences.
// Each character represents 2 vertical pixels (foreground for top, background for bottom).
func imageToHalfBlocks(img image.Image, width, height int) string {
	// resize to width, height*2 (since each character represents 2 vertical pixels)
	img = resize.Resize(uint(width), uint(height*2), img, resize.Lanczos3)

	bounds := img.Bounds()
	var b []byte

	for y := bounds.Min.Y; y < bounds.Max.Y; y += 2 {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			cTop := img.At(x, y)
			var cBot color.Color = color.RGBA{0, 0, 0, 0}
			if y+1 < bounds.Max.Y {
				cBot = img.At(x, y+1)
			}

			r1, g1, b1, a1 := cTop.RGBA()
			r2, g2, b2, a2 := cBot.RGBA()

			r1, g1, b1 = r1>>8, g1>>8, b1>>8
			r2, g2, b2 = r2>>8, g2>>8, b2>>8

			if a1 < 128 && a2 < 128 {
				b = append(b, []byte(" ")...)
			} else {
				b = append(b, []byte(fmt.Sprintf("\033[38;2;%d;%d;%dm\033[48;2;%d;%d;%dm▀", r1, g1, b1, r2, g2, b2))...)
			}
		}
		b = append(b, []byte("\033[0m\n")...)
	}
	return string(b)
}
