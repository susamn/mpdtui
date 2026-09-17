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
	"os"
	"strings"

	"github.com/nfnt/resize"
)

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
	fmt.Printf("\033[%d;%dH", y+1, x+1)

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
			fmt.Printf("\033_Ga=T,i=%d,f=100,q=2,c=%d,r=%d,m=%d;%s\033\\", id, cols, rows, more, b64[i:end])
			continue
		}
		fmt.Printf("\033_Gm=%d;%s\033\\", more, b64[i:end])
	}
}

// Delete removes the placement with this id.
func Delete(id int) {
	if id == 0 {
		return
	}
	fmt.Printf("\033_Ga=d,d=i,i=%d\033\\", id)
}

// NextID alternates between two ids so a retransmit never reuses the id
// of the placement it is about to delete -- the two are always distinct
// objects to the terminal, never the same id being deleted out from
// under itself.
func NextID(last int) int {
	if last == 1 {
		return 2
	}
	return 1
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
