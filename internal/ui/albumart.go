package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/qeesung/image2ascii/convert"
	"github.com/rivo/tview"
)

// albumArtResendInterval bounds how long a stale Kitty placement can
// survive a change draw() has no way to detect on its own -- a pure
// display-scale change (e.g. a Wayland compositor DPI change) can leave
// the terminal's character grid (columns/rows, all tcell/tview expose)
// completely unchanged while the actual pixel size of each cell changes
// underneath it. draw()'s own sig comparison is keyed on that character
// grid, so it sees nothing different and never retransmits -- the
// already-placed image, whose pixel dimensions were fixed by the
// terminal at transmit time, is then wrong for the new cell-to-pixel
// mapping with no resize event to react to. Forcing a fresh retransmit
// (see draw's own new-then-delete-old ordering) unconditionally every
// albumArtResendInterval is a blunt but reliable self-heal for exactly
// that case, at the cost of a harmless no-op retransmit (identical
// bytes, identical position) the rest of the time.
const albumArtResendInterval = 4 * time.Second

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

	currentURI string    // main-goroutine-only (set from onTrackChanged, read from draw)
	sentSig    string    // main-goroutine-only: signature of the last frame actually transmitted
	lastSentAt time.Time // main-goroutine-only: when sentSig was last (re)transmitted, zero if never

	// lastImageID is the Kitty image id (see draw's own i= parameter) of
	// the placement currently on screen, 0 if none. draw() alternates
	// between two fixed ids (see nextImageID) instead of reusing one,
	// specifically so a retransmit can place the *new* image first (an
	// id the terminal has never seen collides with nothing) and only
	// delete the old one -- by that same specific id -- afterward, once
	// the new one is already covering it on screen. Deleting first
	// (the original approach) leaves a visible blank gap between the
	// delete and the new image finishing decode, which is what actually
	// produced the flicker; deleting a *shared* id after transmitting
	// under it again would instead delete the image that was just
	// placed.
	lastImageID int
}

// nextImageID returns the Kitty image id draw() should transmit under
// next -- alternates 1/2 so it's never the same as lastImageID, which is
// what makes a delete-after-transmit of lastImageID safe (see its own
// doc comment): the two placements are always distinct objects to the
// terminal, never the same id being deleted out from under itself.
func nextImageID(last int) int {
	if last == 1 {
		return 2
	}
	return 1
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
	opts := convert.DefaultOptions
	opts.FixedWidth = 30
	opts.FixedHeight = 15
	asciiStr := convert.NewImageConverter().Image2ASCIIString(img, &opts)

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
// changed (sentSig), or albumArtResendInterval has elapsed since the
// last transmit regardless (see its own doc comment: a pure display-
// scale change can leave sig's own inputs, the character grid, totally
// unchanged while the terminal's actual pixel-per-cell size doesn't).
//
// A retransmit always transmits the *new* image first, under a fresh id
// (nextImageID), and only deletes the previous placement -- by that
// specific id, via d=i -- afterward, once the new one is already
// covering it on screen: tview's own Clear() only touches its text
// buffer, it has no idea a Kitty image is separately composited on top,
// and Kitty's own a=T always creates a new placement rather than
// replacing the last one, so *some* explicit delete is unavoidable to
// avoid a permanent ghost (see git history for the resize-ghosting bug
// this fixed). Deleting the old placement *first* was tried and
// reverted: it leaves a visible blank gap between the delete and the
// new image finishing decode, which is what actually produced visible
// flicker on albumArtResendInterval's own periodic no-real-change
// resends. new-then-delete-old never has that gap -- there's always
// something rendered at that location throughout, old until new lands
// on top of it, old removed harmlessly afterward.
func (p *albumArtPanel) draw() {
	if !supportsKittyGraphics() {
		return
	}

	p.mu.Lock()
	data := p.kittyPNG
	p.mu.Unlock()

	if len(data) == 0 {
		if p.lastImageID != 0 {
			fmt.Printf("\033_Ga=d,d=i,i=%d\033\\", p.lastImageID)
			p.lastImageID = 0
			p.sentSig = ""
		}
		return
	}

	x, y, w, h := p.view.GetInnerRect()
	if w == 0 || h == 0 {
		return
	}

	sig := fmt.Sprintf("%s:%d:%d:%d", p.currentURI, len(data), w, h)
	if p.sentSig == sig && time.Since(p.lastSentAt) < albumArtResendInterval {
		return
	}

	id := nextImageID(p.lastImageID)
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
			fmt.Printf("\033_Ga=T,i=%d,f=100,q=2,c=%d,r=%d,m=%d;%s\033\\", id, w, h, m, b64[i:end])
		} else {
			fmt.Printf("\033_Gm=%d;%s\033\\", m, b64[i:end])
		}
	}

	if p.lastImageID != 0 {
		fmt.Printf("\033_Ga=d,d=i,i=%d\033\\", p.lastImageID)
	}
	p.lastImageID = id
	p.sentSig = sig
	p.lastSentAt = time.Now()
}
