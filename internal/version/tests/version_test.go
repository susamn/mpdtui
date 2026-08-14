package tests

import (
	"strings"
	"testing"

	"mpdtui/internal/version"
)

// TestStringIsVPrefixedAndTrimmed guards the two things version.String
// promises on top of the raw embedded VERSION file: a leading "v" (the
// file itself is bare semver, no prefix) and no trailing whitespace/
// newline even if VERSION ends with one.
func TestStringIsVPrefixedAndTrimmed(t *testing.T) {
	if !strings.HasPrefix(version.String, "v") {
		t.Errorf("version.String = %q, want a leading %q", version.String, "v")
	}
	if strings.TrimSpace(version.String) != version.String {
		t.Errorf("version.String = %q, want no surrounding whitespace", version.String)
	}
}

// TestStringLooksLikeSemver is a light sanity check, not a full semver
// parser: three dot-separated, non-empty parts after the "v".
func TestStringLooksLikeSemver(t *testing.T) {
	parts := strings.Split(strings.TrimPrefix(version.String, "v"), ".")
	if len(parts) != 3 {
		t.Fatalf("version.String = %q, want vMAJOR.MINOR.PATCH (3 dot-separated parts), got %d", version.String, len(parts))
	}
	for _, p := range parts {
		if p == "" {
			t.Errorf("version.String = %q, has an empty semver component", version.String)
		}
	}
}
