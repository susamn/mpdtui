package textutil

import "testing"

// FoldSearch is what makes every search in this app accent-insensitive
// -- the README's own promise that "buble" finds "Bublé".
func TestFoldSearch(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Bublé", "buble"},
		{"BUBLÉ", "buble"},
		{"Björk", "bjork"},
		{"Sigur Rós", "sigur ros"},
		{"Mötley Crüe", "motley crue"},
		{"Beyoncé", "beyonce"},
		{"àéîõü", "aeiou"},
		{"plain ascii", "plain ascii"},
		{"", ""},
		// Punctuation and digits pass through untouched: folding is
		// about case and accents, not about stripping characters.
		{"A.C.-D/C 2!", "a.c.-d/c 2!"},
	}
	for _, tc := range cases {
		if got := FoldSearch(tc.in); got != tc.want {
			t.Errorf("FoldSearch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFoldSearchIsIdempotent pins that folding already-folded text is a
// no-op -- search paths fold both the haystack and the needle, and some
// fold values that may already have been folded upstream.
func TestFoldSearchIsIdempotent(t *testing.T) {
	for _, in := range []string{"Bublé", "Sigur Rós", "plain", ""} {
		once := FoldSearch(in)
		if twice := FoldSearch(once); twice != once {
			t.Errorf("FoldSearch is not idempotent for %q: %q -> %q", in, once, twice)
		}
	}
}

// TestFoldSearchStripsIndicVowelSigns pins a real limitation rather
// than an intended behavior.
//
// StripDiacritics removes every Unicode nonspacing mark (unicode.Mn).
// In Latin scripts those are decorations and dropping them is the whole
// point -- "Bublé" must match "buble". In Indic scripts they are not
// decorations: the dependent vowel signs and the virama are letters,
// carrying sounds that distinguish one word from another. Stripping
// them collapses distinct words together.
//
// Search still *works*, because both the haystack and the needle are
// folded the same way, so a track is always findable by its own name.
// What is lost is precision: গুরু ("guru") and গরু ("goru", cow) both
// fold to গর, so either query surfaces both. Given this app already
// treats Bengali and Devanagari as first-class (see
// internal/ui/conjuncts.go, which exists solely to render them
// correctly), that is worth knowing about.
//
// This test documents what the code does today. If the folding is ever
// narrowed to Latin-script marks, this is the test that should change.
func TestFoldSearchStripsIndicVowelSigns(t *testing.T) {
	cases := []struct{ in, want string }{
		{"গুরু", "গর"},       // Bengali "guru"
		{"গরু", "গর"},        // Bengali "goru" (cow) -- same result
		{"सन्ध्या", "सनधया"}, // Devanagari, virama dropped
	}
	for _, tc := range cases {
		if got := FoldSearch(tc.in); got != tc.want {
			t.Errorf("FoldSearch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if FoldSearch("গুরু") != FoldSearch("গরু") {
		t.Error("the documented collision no longer happens -- if the folding was " +
			"deliberately narrowed to Latin marks, update this test and the comment above")
	}
}

// TestFoldSearchLeavesMarklessScriptsAlone covers scripts with no
// combining marks to strip, which must pass through untouched.
func TestFoldSearchLeavesMarklessScriptsAlone(t *testing.T) {
	for _, in := range []string{"日本語", "한국어", "мир"} {
		want := in
		if in == "мир" {
			want = "мир" // already lowercase Cyrillic
		}
		if got := FoldSearch(in); got != want {
			t.Errorf("FoldSearch(%q) = %q, want %q", in, got, want)
		}
	}
}

// NormalizeSegment is the stricter form used for matching path segments,
// where separators and punctuation vary between MPD's paths and a
// lyrics file's name.
func TestNormalizeSegment(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hello World", "helloworld"},
		{"01 - Track Name.mp3", "01tracknamemp3"},
		{"a-r-rahman", "arrahman"},
		{"A_B.C-D", "abcd"},
		{"  spaced  out  ", "spacedout"},
		{"!!!", ""},
		{"", ""},
		{"Track 2", "track2"},
		// Letters of any script count as letters, and accented ones keep
		// their accent here -- unlike FoldSearch, this does not decompose.
		{"Café", "café"},
	}
	for _, tc := range cases {
		if got := NormalizeSegment(tc.in); got != tc.want {
			t.Errorf("NormalizeSegment(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFoldSearchSurvivesInvalidUTF8 covers what happens to bytes that
// are not valid UTF-8 at all -- MPD tags come off disk and are not
// guaranteed well-formed. The transformer replaces them with U+FFFD
// rather than failing, so folding never errors and never panics.
//
// Worth noting: because of that, FoldSearch's own `if err != nil`
// fallback is unreachable with this transformer chain. It is cheap
// insurance rather than live code, which is why coverage of this
// function stops short of 100%.
func TestFoldSearchSurvivesInvalidUTF8(t *testing.T) {
	for _, in := range []string{"\xff\xfe", "abc\xffdef", "Bubl\xe9"} {
		got := FoldSearch(in)
		if got == "" && in != "" {
			t.Errorf("FoldSearch(%q) = %q, want it to return something rather than collapse", in, got)
		}
	}
	// Valid text around an invalid byte still folds and lowercases.
	if got := FoldSearch("ABC\xffDEF"); got != "abc�def" {
		t.Errorf("FoldSearch(%q) = %q, want the valid parts folded around a replacement char", "ABC\xffDEF", got)
	}
}
