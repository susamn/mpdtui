package kvparser

import "testing"

func TestParseValue(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain", "6600", "6600"},
		{"quoted", `"hello world"`, "hello world"},
		{"quoted with inner spaces trimmed", `"  padded  "`, "padded"},
		{"quoted keeps a hash inside", `"pass#word"`, "pass#word"},
		// A bare #rrggbb color must survive -- the theme files are full
		// of them, and cutting at a bare '#' would destroy every one.
		{"bare hex color kept whole", "#ff00aa", "#ff00aa"},
		{"spaced comment dropped", "#ff00aa # the accent", "#ff00aa"},
		{"spaced comment after a word", "true # enable it", "true"},
		{"unterminated quote", `"unclosed`, "unclosed"},
		{"empty", "", ""},
		// Only " #" starts a comment, so a hash mid-token stays put.
		{"hash inside a word", "C#major", "C#major"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseValue(tc.in); got != tc.want {
				t.Errorf("ParseValue(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParse(t *testing.T) {
	data := []byte(`
# a full-line comment
host = localhost
port=6600

  indented = yes
quoted = "a value"
color = #ff00aa
commented = value # trailing
no_equals_sign
= missing key
empty_value =
duplicate = first
duplicate = second
`)
	got := Parse(data)
	want := map[string]string{
		"host":      "localhost",
		"port":      "6600",
		"indented":  "yes",
		"quoted":    "a value",
		"color":     "#ff00aa",
		"commented": "value",
		"duplicate": "second", // last wins
	}
	if len(got) != len(want) {
		t.Errorf("Parse returned %d keys, want %d: %v", len(got), len(want), got)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("Parse()[%q] = %q, want %q", k, got[k], w)
		}
	}
	for _, skipped := range []string{"no_equals_sign", "", "empty_value"} {
		if v, ok := got[skipped]; ok {
			t.Errorf("Parse kept %q = %q, want it skipped", skipped, v)
		}
	}
}

func TestParseEmptyInput(t *testing.T) {
	for _, in := range [][]byte{nil, {}, []byte("\n\n\n"), []byte("# only comments\n")} {
		if got := Parse(in); len(got) != 0 {
			t.Errorf("Parse(%q) = %v, want empty", in, got)
		}
	}
}

// TestParseHandlesCRLF covers config files written on Windows or edited
// by a tool that leaves carriage returns: bufio.Scanner strips \n but
// not \r, so the value would otherwise carry one.
func TestParseHandlesCRLF(t *testing.T) {
	got := Parse([]byte("host = localhost\r\nport = 6600\r\n"))
	if got["host"] != "localhost" {
		t.Errorf("host = %q, want %q (a stray carriage return was kept)", got["host"], "localhost")
	}
	if got["port"] != "6600" {
		t.Errorf("port = %q, want %q (a stray carriage return was kept)", got["port"], "6600")
	}
}
