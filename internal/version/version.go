// Package version exposes mpdtui's own release version, embedded from
// the single VERSION file in this directory (a plain semver string, no
// "v" prefix -- e.g. "1.5.0") rather than a hardcoded Go const, so
// bumping a release is a one-line diff to a plain text file, the same
// value cmd/mpdtui or a future --version flag could read too, not a
// source-code edit buried in internal/ui. Embedded at build time (not
// read from disk at runtime): an installed binary (e.g. via the
// Homebrew formula) no longer has the source tree sitting next to it,
// so a runtime file read would break there.
package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var raw string

// String is mpdtui's own release version, v-prefixed (e.g. "v1.5.0")
// regardless of VERSION's own bare-semver format -- trimmed of any
// trailing newline the file might carry.
var String = "v" + strings.TrimSpace(raw)
