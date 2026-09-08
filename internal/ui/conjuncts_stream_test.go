package ui

import (
	"strings"

	"github.com/rivo/uniseg"
)

// streamAsPrinted reproduces the byte stream tview's print loop emits
// for s: each grapheme cluster written into one cell, the remaining
// width-1 cells filled with spaces, and zero-width clusters skipped
// entirely (which is the behaviour that decides whether a joiner ever
// reaches the terminal at all).
func streamAsPrinted(s string) string {
	var b strings.Builder
	state := -1
	rest := s
	for len(rest) > 0 {
		var cluster string
		var width int
		cluster, rest, width, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if width == 0 {
			continue
		}
		b.WriteString(cluster)
		for i := 1; i < width; i++ {
			b.WriteString(" ")
		}
	}
	return b.String()
}
