package mpdclient

import (
	"testing"
	"time"
)

// parseSeconds and parseUnix both turn a missing or malformed stats
// field into a zero value rather than an error, so one bad line cannot
// fail the whole command. That is the behaviour worth pinning: an
// integration test against a live server only ever sees well-formed
// input.
func TestParseSecondsAndParseUnixTolerateBadInput(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"3600", time.Hour},
		{"1", time.Second},
		{"", 0},
		{"0", 0},
		{"-5", 0},
		{"not a number", 0},
		{"12.5", 0},
	} {
		if got := parseSeconds(tc.in); got != tc.want {
			t.Errorf("parseSeconds(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}

	if got := parseUnix("1758400000"); !got.Equal(time.Unix(1758400000, 0)) {
		t.Errorf("parseUnix = %v, want the Unix time it names", got)
	}
	for _, in := range []string{"", "0", "-1", "yesterday"} {
		if got := parseUnix(in); !got.IsZero() {
			t.Errorf("parseUnix(%q) = %v, want the zero time", in, got)
		}
	}
}
