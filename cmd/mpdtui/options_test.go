package main

import (
	"errors"
	"flag"
	"io"
	"testing"
)

func parse(t *testing.T, args ...string) options {
	t.Helper()
	fs := flag.NewFlagSet("mpdtui", flag.ContinueOnError)
	o, err := parseFlags(fs, args, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags(%v): %v", args, err)
	}
	return o
}

func TestParseFlags(t *testing.T) {
	t.Run("defaults to the full UI", func(t *testing.T) {
		o := parse(t)
		if !o.fullUI() {
			t.Error("no flags did not select the full UI")
		}
		if o.modeCount() != 0 {
			t.Errorf("modeCount = %d with no flags, want 0", o.modeCount())
		}
	})

	t.Run("each mode flag", func(t *testing.T) {
		cases := []struct {
			arg string
			get func(options) bool
		}{
			{"-mini", func(o options) bool { return o.miniMode }},
			{"-p", func(o options) bool { return o.playlistPicker }},
			{"-t", func(o options) bool { return o.trackPicker }},
			{"-lyrics-line", func(o options) bool { return o.lyricsLine }},
			{"-i", func(o options) bool { return o.trackInfo }},
			{"-iu", func(o options) bool { return o.trackInfoUpdate }},
			{"-v", func(o options) bool { return o.showVersion }},
		}
		for _, tc := range cases {
			t.Run(tc.arg, func(t *testing.T) {
				o := parse(t, tc.arg)
				if !tc.get(o) {
					t.Errorf("%s did not set its flag", tc.arg)
				}
			})
		}
	})

	t.Run("rating value", func(t *testing.T) {
		o := parse(t, "-iu", "-r", "4")
		if o.rating != 4 {
			t.Errorf("rating = %d, want 4", o.rating)
		}
	})

	t.Run("an unknown flag is an error", func(t *testing.T) {
		fs := flag.NewFlagSet("mpdtui", flag.ContinueOnError)
		if _, err := parseFlags(fs, []string{"-nope"}, io.Discard); err == nil {
			t.Error("an unknown flag was accepted")
		}
	})
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want error
	}{
		{"no flags", nil, nil},
		{"one mode", []string{"-mini"}, nil},
		{"iu with a rating", []string{"-iu", "-r", "3"}, nil},
		{"version alongside a mode is fine", []string{"-v"}, nil},

		{"two modes", []string{"-mini", "-p"}, errExclusiveModes},
		{"three modes", []string{"-i", "-t", "-lyrics-line"}, errExclusiveModes},
		{"rating without iu", []string{"-r", "3"}, errRatingNeedsIU},
		{"rating with the wrong mode", []string{"-i", "-r", "3"}, errRatingNeedsIU},
		{"iu with no rating", []string{"-iu"}, errIUNeedsUpdate},
		{"rating too low", []string{"-iu", "-r", "-1"}, errRatingRange},
		{"rating too high", []string{"-iu", "-r", "6"}, errRatingRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := parse(t, tc.args...).validate()
			if !errors.Is(err, tc.want) {
				t.Errorf("validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestNeedsMetaDB pins which modes open the local database. -lyrics-line
// in particular must not: it is polled once a second by external tools,
// one process per line, and having those contend over a database they
// never read produced a stream of "database is locked" warnings.
func TestNeedsMetaDB(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, true},               // full UI
		{[]string{"-mini"}, true}, // shows ratings and counts play-throughs
		{[]string{"-i"}, true},
		{[]string{"-iu", "-r", "3"}, true},

		{[]string{"-lyrics-line"}, false},
		{[]string{"-p"}, false},
		{[]string{"-t"}, false},
	}
	for _, tc := range cases {
		name := "full UI"
		if len(tc.args) > 0 {
			name = tc.args[0]
		}
		t.Run(name, func(t *testing.T) {
			if got := parse(t, tc.args...).needsMetaDB(); got != tc.want {
				t.Errorf("needsMetaDB() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFullUIOnlyWhenNoModeChosen pins the complement of the mode flags,
// which is what decides whether the panel UI runs at all.
func TestFullUIOnlyWhenNoModeChosen(t *testing.T) {
	if !parse(t).fullUI() {
		t.Error("no flags should mean the full UI")
	}
	if !parse(t, "-v").fullUI() {
		t.Error("-v is not a mode, so it should not stop the full UI being selected")
	}
	for _, arg := range []string{"-mini", "-p", "-t", "-lyrics-line", "-i", "-iu"} {
		if parse(t, arg).fullUI() {
			t.Errorf("%s should not leave the full UI selected", arg)
		}
	}
}
