package albumart

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"sync"
	"time"

	"github.com/rivo/tview"

	"mpdtui/internal/termimage"
)

// resendInterval bounds how long a stale Kitty placement can
// survive a change Draw has no way to detect on its own -- a pure
// display-scale change (e.g. a Wayland compositor DPI change) can leave
// the terminal's character grid (columns/rows, all tcell/tview expose)
// completely unchanged while the actual pixel size of each cell changes
// underneath it. Draw's own sig comparison is keyed on that character
// grid, so it sees nothing different and never retransmits -- the
// already-placed image, whose pixel dimensions were fixed by the
// terminal at transmit time, is then wrong for the new cell-to-pixel
// mapping with no resize event to react to. Forcing a fresh retransmit
// (see Draw's own new-then-delete-old ordering) unconditionally every
// resendInterval is a blunt but reliable self-heal for exactly
// that case, at the cost of a harmless no-op retransmit (identical
// bytes, identical position) the rest of the time.
const resendInterval = 4 * time.Second

// Panel renders the currently playing track's art, either as a
// real image (Kitty graphics protocol, when supported) or as ASCII art
// (everywhere else). The Kitty path writes raw escape sequences directly
// to the terminal, outside tview/tcell's own screen model entirely --
// neither has any notion of pixel graphics -- so mu/seq/sentSig exist
// specifically to keep that safe: seq guards a slow fetch from overwriting
// a newer one, and all direct terminal writes happen from a single place
// (Draw, invoked from the App's SetAfterDrawFunc) so they never overlap
// tview's own single-threaded draw cycle or each other.
// Fetcher is the one thing this package needs from the MPD client:
// given a track URI, the bytes of its embedded album art.
//
// An interface rather than *mpdclient.Client so the fetch path --
// everything between "the track changed" and "there is a picture on
// screen" -- can be tested against canned bytes instead of requiring a
// live server with embedded art. It also means this package needs no
// dependency on internal/mpdclient at all.
type Fetcher interface {
	FetchAlbumArt(uri string) ([]byte, error)
}

type Panel struct {
	client Fetcher
	view   *tview.TextView

	// applyToUI hands a closure to the UI goroutine to run and redraw.
	// A field rather than a direct tv.QueueUpdateDraw call so tests can
	// substitute a synchronous stand-in -- nothing drains a tview
	// application's update queue unless Run() is actually running, so
	// the real one would block forever in a test and the whole fetch
	// path would stay untestable. Same reason App.runAsync is a field.
	applyToUI func(func())

	mu       sync.Mutex
	seq      int    // bumped on every track change; a fetch checks this before applying its result
	kittyPNG []byte // set only when termimage.Supported() and decode+encode succeeded

	currentURI string    // main-goroutine-only (set from OnTrackChanged, read from Draw)
	sentSig    string    // main-goroutine-only: signature of the last frame actually transmitted
	lastSentAt time.Time // main-goroutine-only: when sentSig was last (re)transmitted, zero if never

	// lastImageID is the Kitty image id (see Draw's own i= parameter) of
	// the placement currently on screen, 0 if none. Draw alternates
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

// albumArtIDs is this panel's own pair of Kitty image ids, distinct
// from every other picture that can share the screen with it (see
// termimage.IDPair). The story card holds the next pair.
var albumArtIDs = termimage.IDPair{1, 2}

func New(client Fetcher, tv *tview.Application) *Panel {
	v := tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	v.SetBorder(true).SetTitle(" Album Art ")
	v.SetText("\n\n[::d]Album Art Loading...[-:-:-]")
	return &Panel{client: client, view: v, applyToUI: func(f func()) { tv.QueueUpdateDraw(f) }}
}

// OnTrackChanged kicks off a background fetch for uri's album art, unless
// it's already the current track.
func (p *Panel) OnTrackChanged(uri string) {
	seq, ok := p.startFetch(uri)
	if !ok {
		return
	}
	go p.fetch(uri, seq)
}

// startFetch records uri as the current track and bumps the sequence
// number, unless uri is already current (or empty). Returns the new
// sequence number and whether a fetch should actually be started. Split
// out from OnTrackChanged so this bookkeeping is unit-testable without
// spawning a goroutine that touches the network.
func (p *Panel) startFetch(uri string) (seq int, ok bool) {
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
func (p *Panel) isCurrent(seq int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return seq == p.seq
}

// setKittyPNGIfCurrent stores data as the Kitty-protocol image for this
// fetch, unless a newer track change has since started. Returns whether it
// was actually applied. The check-and-set happens under one lock so there's
// no gap between "is this still current" and "apply the result" for a
// newer OnTrackChanged to land in.
func (p *Panel) setKittyPNGIfCurrent(seq int, data []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if seq != p.seq {
		return false
	}
	p.kittyPNG = data
	return true
}

func (p *Panel) fetch(uri string, seq int) {
	b, err := p.client.FetchAlbumArt(uri)
	if err != nil || len(b) == 0 {
		p.setKittyPNGIfCurrent(seq, nil)
		p.applyToUI(func() {
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
		p.applyToUI(func() {
			if !p.isCurrent(seq) {
				return
			}
			p.view.Clear()
			p.view.SetText("\n\n[::d]Decode Error[-:-:-]")
		})
		return
	}

	if termimage.Supported() {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			p.setKittyPNGIfCurrent(seq, nil)
			p.applyToUI(func() {
				if !p.isCurrent(seq) {
					return
				}
				p.view.Clear()
				p.view.SetText("\n\n[::d]Encode Error[-:-:-]")
			})
			return
		}
		p.setKittyPNGIfCurrent(seq, buf.Bytes())
		p.applyToUI(func() {
			if !p.isCurrent(seq) {
				return
			}
			p.view.Clear() // clears the panel's text so Draw's image isn't obscured
		})
		return
	}

	p.setKittyPNGIfCurrent(seq, nil)

	// Fallback to high-resolution ASCII using Unicode half-blocks and true color
	asciiStr := termimage.HalfBlocks(img, 30, 15)

	p.applyToUI(func() {
		if !p.isCurrent(seq) {
			return
		}
		p.view.Clear()
		fmt.Fprint(tview.ANSIWriter(p.view), asciiStr)
	})
}

// Draw writes (or clears) the Kitty-protocol image for the current frame.
// Called once per screen redraw from the App's SetAfterDrawFunc, which
// tview always invokes synchronously as part of its own single-threaded
// draw cycle -- the only reason it's safe for this to write raw escape
// sequences straight to the terminal without racing tview/tcell's own
// output. Retransmits only when the image or the panel's size actually
// changed (sentSig), or resendInterval has elapsed since the
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
// flicker on resendInterval's own periodic no-real-change
// resends. new-then-delete-old never has that gap -- there's always
// something rendered at that location throughout, old until new lands
// on top of it, old removed harmlessly afterward.
func (p *Panel) Draw() {
	if !termimage.Supported() {
		return
	}

	p.mu.Lock()
	data := p.kittyPNG
	p.mu.Unlock()

	if len(data) == 0 {
		if p.lastImageID != 0 {
			termimage.Delete(p.lastImageID)
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
	if p.sentSig == sig && time.Since(p.lastSentAt) < resendInterval {
		return
	}

	id := albumArtIDs.Next(p.lastImageID)
	termimage.Place(data, x, y, w, h, id)
	termimage.Delete(p.lastImageID)
	p.lastImageID = id
	p.sentSig = sig
	p.lastSentAt = time.Now()
}

func (p *Panel) View() *tview.TextView {
	return p.view
}
