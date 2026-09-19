// Package termimage puts a picture on a terminal, by whichever of two
// routes the terminal supports.
//
// Kitty's graphics protocol draws real pixels, but does it *outside*
// tview/tcell's screen model entirely -- neither has any notion of pixel
// graphics, so the escape sequences go straight to stdout and the
// terminal composites them over whatever tview drew. That is why every
// call here must come from inside tview's own single-threaded draw cycle
// (an AfterDraw hook): anywhere else races tcell's output.
//
// Everywhere else, HalfBlocks renders into ordinary text that tview can
// draw like any other content.
//
// Extracted from internal/albumart, which was the only caller until the
// story modal needed the same two routes for a different picture in a
// different place (see DEPENDENCY.md: shared behaviour moves down to a
// leaf rather than one component importing another).
package termimage

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"strings"

	"github.com/nfnt/resize"
)

// out overrides where the escape sequences go, for tests that want to
// read back what the protocol actually emitted -- these byte sequences
// are the whole contract with the terminal, and nothing used to assert
// one of them.
//
// nil rather than os.Stdout, resolved per call by sink(): binding the
// package variable at init captures whatever os.Stdout was *then*, and
// internal/albumart's own tests work by swapping os.Stdout afterwards.
// A captured reference would leave those writing to the real terminal
// and their assertions reading an empty buffer.
var out io.Writer

func sink() io.Writer {
	if out != nil {
		return out
	}
	return os.Stdout
}

// Supported reports whether the terminal is likely to understand the
// Kitty graphics protocol. TERM containing "kitty" catches the common
// case; KITTY_WINDOW_ID is set by kitty itself and stays set even when
// TERM is overridden to something like xterm-256color (a common
// workaround for remote hosts lacking kitty's terminfo entry), which
// TERM alone would miss. Terminals that implement the protocol without
// either signal (e.g. some WezTerm/Konsole configurations) still fall
// back to ASCII -- there's no reliable capability-query path without
// risking a hang against terminals that don't answer it.
func Supported() bool {
	return strings.Contains(os.Getenv("TERM"), "kitty") || os.Getenv("KITTY_WINDOW_ID") != ""
}

// Place transmits png and has the terminal draw it in the cols x rows
// character cell box whose top-left corner is at (x, y), zero-based.
//
// id identifies the placement so Delete can remove exactly this one
// later. Callers alternate ids (see NextID) and delete the *previous*
// placement only after the new one is on screen: Kitty's a=T always
// creates a new placement rather than replacing the last, so some
// explicit delete is needed to avoid a permanent ghost, and doing it
// first instead leaves a visible gap while the new image decodes.
func Place(png []byte, x, y, cols, rows, id int) {
	if len(png) == 0 || cols <= 0 || rows <= 0 {
		return
	}
	// Cursor position is 1-based in the terminal's own coordinates.
	fmt.Fprintf(sink(), "\033[%d;%dH", y+1, x+1)

	b64 := base64.StdEncoding.EncodeToString(png)
	const chunkSize = 4096
	for i := 0; i < len(b64); i += chunkSize {
		end := i + chunkSize
		more := 1
		if end >= len(b64) {
			end = len(b64)
			more = 0
		}
		if i == 0 {
			fmt.Fprintf(sink(), "\033_Ga=T,i=%d,f=100,q=2,c=%d,r=%d,m=%d;%s\033\\", id, cols, rows, more, b64[i:end])
			continue
		}
		fmt.Fprintf(sink(), "\033_Gm=%d;%s\033\\", more, b64[i:end])
	}
}

// Delete removes the placement with this id.
func Delete(id int) {
	if id == 0 {
		return
	}
	fmt.Fprintf(sink(), "\033_Ga=d,d=i,i=%d\033\\", id)
}

// IDPair is one caller's private pair of Kitty image ids.
//
// Ids are terminal-wide, not per-program and certainly not per-widget:
// transmitting under an id replaces that image for the whole terminal,
// and deleting an id removes whatever is placed under it no matter who
// put it there. So every picture that can be on screen at the same time
// as another needs a pair nobody else uses.
//
// Two callers sharing a pair is not a subtle failure. It looks like one
// picture flickering in and out while the other disappears outright,
// because that is exactly what happens: each one's retransmit deletes
// the other's placement.
type IDPair [2]int

// Next returns whichever of the pair is not last, so a retransmit never
// reuses the id it is about to delete -- the two placements are always
// distinct objects to the terminal.
func (p IDPair) Next(last int) int {
	if last == p[0] {
		return p[1]
	}
	return p[0]
}

// HalfBlocks renders img as text, at cols x rows character cells.
//
// Each cell is an upper-half-block glyph with a foreground and a
// background colour, so one character carries two vertically stacked
// pixels -- twice the vertical resolution of one colour per cell, which
// is what makes a small image legible at all in a terminal.
func HalfBlocks(img image.Image, cols, rows int) string {
	if cols <= 0 || rows <= 0 {
		return ""
	}
	img = resize.Resize(uint(cols), uint(rows*2), img, resize.Lanczos3)

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
				b = append(b, ' ')
				continue
			}
			b = append(b, fmt.Sprintf("\033[38;2;%d;%d;%dm\033[48;2;%d;%d;%dm▀", r1, g1, b1, r2, g2, b2)...)
		}
		b = append(b, "\033[0m\n"...)
	}
	return string(b)
}

// SetOutputForTest redirects the escape sequences and returns a function
// restoring the previous sink. For tests in other packages, which cannot
// reach the unexported variable -- the story card's drawing lives in
// internal/ui, and whether it transmits at all is worth asserting there
// rather than only here.
func SetOutputForTest(w io.Writer) func() {
	prev := out
	out = w
	return func() { out = prev }
}
