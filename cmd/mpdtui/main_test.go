package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestModeCount(t *testing.T) {
	cases := []struct {
		flags []bool
		want  int
	}{
		{[]bool{}, 0},
		{[]bool{false, false, false}, 0},
		{[]bool{true, false, false}, 1},
		{[]bool{false, true, false, false}, 1},
		{[]bool{true, true, false}, 2},
		{[]bool{true, true, true, true}, 4},
		{[]bool{false, false, false, false, true, false}, 1},
		{[]bool{false, false, false, false, false, true}, 1},
		{[]bool{false, false, false, false, true, true}, 2},
	}
	for _, tc := range cases {
		if got := modeCount(tc.flags...); got != tc.want {
			t.Errorf("modeCount(%v) = %d, want %d", tc.flags, got, tc.want)
		}
	}
}

func buildTestBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "mpdtui-test")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\nOutput: %s", err, string(out))
	}
	return bin
}

func TestCLIFlagValidation(t *testing.T) {
	bin := buildTestBinary(t)

	cases := []struct {
		name       string
		args       []string
		wantStderr string
		wantExit   int
	}{
		{
			name:       "rating without iu",
			args:       []string{"-r", "4"},
			wantStderr: "mpdtui: -r must be used with -iu",
			wantExit:   1,
		},
		{
			name:       "iu without update flag",
			args:       []string{"-iu"},
			wantStderr: "mpdtui: -iu requires an update flag (e.g. -r 1-5)",
			wantExit:   1,
		},
		{
			name:       "iu with rating below 1",
			args:       []string{"-iu", "-r", "0"},
			wantStderr: "mpdtui: -iu requires an update flag (e.g. -r 1-5)",
			wantExit:   1,
		},
		{
			name:       "iu with rating above 5",
			args:       []string{"-iu", "-r", "6"},
			wantStderr: "mpdtui: -r rating must be between 1 and 5",
			wantExit:   1,
		},
		{
			name:       "iu with negative rating",
			args:       []string{"-iu", "-r", "-1"},
			wantStderr: "mpdtui: -r rating must be between 1 and 5",
			wantExit:   1,
		},
		{
			name:       "mutually exclusive i and iu",
			args:       []string{"-i", "-iu", "-r", "3"},
			wantStderr: "mpdtui: -mini, -p, -t, -lyrics-line, -i, and -iu are mutually exclusive",
			wantExit:   1,
		},
		{
			name:       "mutually exclusive i and mini",
			args:       []string{"-i", "-mini"},
			wantStderr: "mpdtui: -mini, -p, -t, -lyrics-line, -i, and -iu are mutually exclusive",
			wantExit:   1,
		},
		{
			name:       "mutually exclusive i and p",
			args:       []string{"-i", "-p"},
			wantStderr: "mpdtui: -mini, -p, -t, -lyrics-line, -i, and -iu are mutually exclusive",
			wantExit:   1,
		},
		{
			name:       "mutually exclusive i and t",
			args:       []string{"-i", "-t"},
			wantStderr: "mpdtui: -mini, -p, -t, -lyrics-line, -i, and -iu are mutually exclusive",
			wantExit:   1,
		},
		{
			name:       "mutually exclusive i and lyrics-line",
			args:       []string{"-i", "-lyrics-line"},
			wantStderr: "mpdtui: -mini, -p, -t, -lyrics-line, -i, and -iu are mutually exclusive",
			wantExit:   1,
		},
		{
			name:       "mutually exclusive iu and mini",
			args:       []string{"-iu", "-r", "3", "-mini"},
			wantStderr: "mpdtui: -mini, -p, -t, -lyrics-line, -i, and -iu are mutually exclusive",
			wantExit:   1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, tc.args...)
			cmd.Env = append(os.Environ(), "MPD_HOST=127.0.0.1", "MPD_PORT=65534") // avoid connecting to real MPD during flag check
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected command to exit with error, but succeeded. Output: %s", string(out))
			}
			if !strings.Contains(string(out), tc.wantStderr) {
				t.Errorf("output = %q, want it to contain %q", string(out), tc.wantStderr)
			}
		})
	}
}
