package metadata

import (
	"testing"
	"time"
)

// parseTimestamp exists because the SQLite driver hands timestamps back
// in whichever form it feels like -- a time.Time, a string, or raw
// bytes, and in either of two string layouts depending on how the row
// was written. Everything that reads a bookmark goes through it, so a
// format it does not recognize is a bookmark the user cannot open.
func TestParseTimestamp(t *testing.T) {
	want := time.Date(2026, 9, 13, 18, 25, 10, 0, time.UTC)

	t.Run("time.Time passes through", func(t *testing.T) {
		got, err := parseTimestamp(want)
		if err != nil {
			t.Fatalf("parseTimestamp: %v", err)
		}
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("SQLite's own layout", func(t *testing.T) {
		got, err := parseTimestamp("2026-09-13 18:25:10")
		if err != nil {
			t.Fatalf("parseTimestamp: %v", err)
		}
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("RFC3339", func(t *testing.T) {
		got, err := parseTimestamp("2026-09-13T18:25:10Z")
		if err != nil {
			t.Fatalf("parseTimestamp: %v", err)
		}
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("raw bytes recurse through the string cases", func(t *testing.T) {
		got, err := parseTimestamp([]byte("2026-09-13 18:25:10"))
		if err != nil {
			t.Fatalf("parseTimestamp: %v", err)
		}
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("unrecognized string", func(t *testing.T) {
		_, err := parseTimestamp("13/09/2026")
		if err == nil {
			t.Fatal("no error for an unparseable timestamp")
		}
	})

	t.Run("unexpected type", func(t *testing.T) {
		for _, v := range []any{42, 3.5, nil, true} {
			if _, err := parseTimestamp(v); err == nil {
				t.Errorf("no error for a %T timestamp", v)
			}
		}
	})
}
