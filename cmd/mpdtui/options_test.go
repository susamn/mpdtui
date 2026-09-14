package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"testing"

	"mpdtui/internal/config"
	"mpdtui/internal/mpdclient"
	"mpdtui/internal/version"
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

// --- run ----------------------------------------------------------------
//
// run is main's body with the process boundary pulled out, so these
// exercise the real dispatch: flag handling, the MPD connection, and
// each mode that prints and exits. The modes that take over the
// terminal (-mini, -p, -t, and the default panel UI) are not driven
// here -- they need a tty.

// mpdReachable reports whether the modes that need a server can run.
func mpdReachable(t *testing.T) bool {
	t.Helper()
	c, err := mpdclient.Dial(config.Load())
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func TestRunVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-v"}, &out, &errOut); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if got := strings.TrimSpace(out.String()); got != strings.TrimSpace(version.String) {
		t.Errorf("printed %q, want the version %q", got, version.String)
	}
	if errOut.Len() != 0 {
		t.Errorf("wrote %q to stderr, want nothing", errOut.String())
	}
}

func TestRunRejectsBadFlagCombinations(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"-i", "-mini"}, "mutually exclusive"},
		{[]string{"-r", "3"}, "-r must be used with -iu"},
		{[]string{"-iu"}, "-iu requires an update flag"},
		{[]string{"-iu", "-r", "9"}, "between 1 and 5"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run(tc.args, &out, &errOut); code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("stderr = %q, want it to mention %q", errOut.String(), tc.want)
			}
		})
	}
}

func TestRunRejectsAnUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-nope"}, &out, &errOut); code != 2 {
		t.Errorf("exit code = %d, want 2 (the flag package's own)", code)
	}
}

// TestRunReportsAnUnreachableServer covers the connection failure path,
// pointed at a port nothing listens on.
func TestRunReportsAnUnreachableServer(t *testing.T) {
	t.Setenv("MPD_HOST", "127.0.0.1")
	t.Setenv("MPD_PORT", "1")

	var out, errOut bytes.Buffer
	if code := run([]string{"-i"}, &out, &errOut); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "connect to MPD") {
		t.Errorf("stderr = %q, want it to report the connection failure", errOut.String())
	}
}

// TestRunTrackInfoNeedsLiveMPD covers -i end to end: connect, read the
// current track, print it. Read-only.
func TestRunTrackInfoNeedsLiveMPD(t *testing.T) {
	if !mpdReachable(t) {
		t.Skip("no MPD server reachable")
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"-i"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if out.Len() == 0 {
		t.Error("-i printed nothing")
	}
}

// TestRunLyricsLineNeedsLiveMPD covers -lyrics-line, the mode external
// tools poll. Read-only, and it must always print its whole window.
func TestRunLyricsLineNeedsLiveMPD(t *testing.T) {
	if !mpdReachable(t) {
		t.Skip("no MPD server reachable")
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"-lyrics-line"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if n := strings.Count(out.String(), "\n"); n != 4 {
		t.Errorf("-lyrics-line printed %d lines, want the fixed 4-line window", n)
	}
}

// TestRunTrackInfoUpdateRejectsWithoutMetadata covers -iu reaching the
// dispatch and failing cleanly when the feature is off. Pointed at an
// empty config directory so track_metadata is unset, which also keeps
// it from touching the real database.
func TestRunTrackInfoUpdateRejectsWithoutMetadata(t *testing.T) {
	if !mpdReachable(t) {
		t.Skip("no MPD server reachable")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out, errOut bytes.Buffer
	if code := run([]string{"-iu", "-r", "3"}, &out, &errOut); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "track metadata") {
		t.Errorf("stderr = %q, want it to explain the feature is off", errOut.String())
	}
}

func TestSummaryFromCarriesEveryResolvedSetting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	s := summaryFrom(config.Config{Host: "localhost", Port: "6600", Password: "secret"})

	if s.MPDHost != "localhost" || s.MPDPort != "6600" {
		t.Errorf("host/port = %q/%q, want the dialled values", s.MPDHost, s.MPDPort)
	}
	if !s.MPDPasswordSet {
		t.Error("MPDPasswordSet = false with a password configured")
	}
	// The password's own value must never reach the summary -- a
	// settings view has no business displaying a credential.
	if strings.Contains(fmt.Sprintf("%+v", s), "secret") {
		t.Error("the summary carries the password itself")
	}
	if s.ConfigFilePath == "" || s.DBFilePath == "" || s.LyricsIndexPath == "" {
		t.Errorf("summary has unresolved paths: %+v", s)
	}
	if s.VisualizerFIFO == "" {
		t.Error("VisualizerFIFO is empty, want the resolved default")
	}
}
