package lyricsindex

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStatMtimeNSOnAMissingFile covers the sidecar-freshness probe for a
// file that is not there: it reports zero rather than failing, which is
// what makes "no sidecar" and "sidecar unchanged" comparable.
func TestStatMtimeNSOnAMissingFile(t *testing.T) {
	if got := statMtimeNS(filepath.Join(t.TempDir(), "nope.txt")); got != 0 {
		t.Errorf("statMtimeNS of a missing file = %d, want 0", got)
	}
}

func TestStatMtimeNSOnARealFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lyrics.txt")
	if err := os.WriteFile(path, []byte("words"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := statMtimeNS(path); got == 0 {
		t.Error("statMtimeNS of a real file = 0, want its modification time")
	}
}

func TestMaxInt64(t *testing.T) {
	cases := []struct{ a, b, want int64 }{
		{1, 2, 2},
		{2, 1, 2},
		{5, 5, 5},
		{-3, -1, -1},
		{0, -1, 0},
	}
	for _, tc := range cases {
		if got := maxInt64(tc.a, tc.b); got != tc.want {
			t.Errorf("maxInt64(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
