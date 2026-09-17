package termimage

import (
	"bytes"
	"strings"
	"testing"
)

// capture swaps the escape-sequence sink for the duration of a test.
func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := out
	out = &buf
	t.Cleanup(func() { out = prev })
	return &buf
}

// TestPlaceEmitsTheKittyProtocol pins the bytes that are the entire
// contract with the terminal: position first, then a transmit-and-place
// command carrying the size in *cells*, the PNG format marker, and the
// image id the matching Delete will name.
func TestPlaceEmitsTheKittyProtocol(t *testing.T) {
	buf := capture(t)
	Place([]byte("pretend-png"), 4, 2, 24, 12, 1)

	got := buf.String()
	// Cursor home is 1-based, so (4,2) addresses row 3, column 5.
	if !strings.HasPrefix(got, "\033[3;5H") {
		t.Errorf("does not start by positioning the cursor: %q", got[:min(24, len(got))])
	}
	for _, want := range []string{
		"\033_Ga=T,", // transmit and place
		"i=1,",       // the id Delete will name
		"f=100,",     // PNG
		"c=24,",      // width in cells, not pixels
		"r=12,",      // height in cells
		"m=0;",       // single chunk: no continuation
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if !strings.HasSuffix(got, "\033\\") {
		t.Error("the escape sequence is not terminated")
	}
}

// TestPlaceChunksLargeImages covers the 4096-byte chunking: every chunk
// but the last must say m=1 so the terminal keeps reading.
func TestPlaceChunksLargeImages(t *testing.T) {
	buf := capture(t)
	Place(bytes.Repeat([]byte("x"), 10_000), 0, 0, 10, 10, 2)

	got := buf.String()
	if n := strings.Count(got, "m=1;"); n < 2 {
		t.Errorf("large image sent in %d continued chunks, want several", n)
	}
	if n := strings.Count(got, "m=0;"); n != 1 {
		t.Errorf("found %d final chunks, want exactly 1", n)
	}
	if n := strings.Count(got, "\033_Ga=T,"); n != 1 {
		t.Errorf("sent %d transmit headers, want 1 followed by continuations", n)
	}
}

func TestPlaceIgnoresNothingToDraw(t *testing.T) {
	for _, tc := range []struct {
		name       string
		png        []byte
		cols, rows int
	}{
		{"no data", nil, 10, 10},
		{"no width", []byte("x"), 0, 10},
		{"no height", []byte("x"), 10, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := capture(t)
			Place(tc.png, 0, 0, tc.cols, tc.rows, 1)
			if buf.Len() != 0 {
				t.Errorf("wrote %q, want nothing", buf.String())
			}
		})
	}
}

// TestDeleteNamesTheImage: d=i deletes by id, rather than clearing every
// placement on screen -- the album art panel and the story card can both
// have one up at once.
func TestDeleteNamesTheImage(t *testing.T) {
	buf := capture(t)
	Delete(2)
	if got, want := buf.String(), "\033_Ga=d,d=i,i=2\033\\"; got != want {
		t.Errorf("Delete(2) = %q, want %q", got, want)
	}

	buf2 := capture(t)
	Delete(0) // nothing was ever placed
	if buf2.Len() != 0 {
		t.Errorf("Delete(0) wrote %q, want nothing", buf2.String())
	}
}

func TestIDPairAlternatesWithinItself(t *testing.T) {
	p := IDPair{3, 4}
	if got := p.Next(3); got != 4 {
		t.Errorf("Next(3) = %d, want 4", got)
	}
	// Anything that is not the first id -- including 0 before anything
	// has been placed, and an id belonging to somebody else -- yields
	// the first.
	for _, last := range []int{0, 4, 1} {
		if got := p.Next(last); got != 3 {
			t.Errorf("Next(%d) = %d, want 3", last, got)
		}
	}
}

// TestIDPairsDoNotOverlap is the regression test for two pictures
// deleting each other. The album art panel and the story card can be on
// screen together, and an id is terminal-wide: a shared pair made the
// card flicker in and out while the album art vanished.
func TestIDPairsDoNotOverlap(t *testing.T) {
	art, card := IDPair{1, 2}, IDPair{3, 4}

	seen := map[int]string{}
	for name, p := range map[string]IDPair{"album art": art, "story card": card} {
		for _, id := range p {
			if id == 0 {
				t.Errorf("%s uses id 0, which Delete treats as 'nothing placed'", name)
			}
			if other, dup := seen[id]; dup {
				t.Errorf("%s and %s both use id %d", name, other, id)
			}
			seen[id] = name
		}
	}

	// And neither can ever hand out one of the other's, whatever it is
	// told was placed last.
	for _, last := range []int{0, 1, 2, 3, 4} {
		if got := art.Next(last); got != 1 && got != 2 {
			t.Errorf("album art produced %d, outside its own pair", got)
		}
		if got := card.Next(last); got != 3 && got != 4 {
			t.Errorf("story card produced %d, outside its own pair", got)
		}
	}
}
