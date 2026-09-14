package kvparser

import (
	"bufio"
	"bytes"
	"strings"
)

// ParseValue extracts the actual value string from a raw RHS string, stripping
// comments and quotes.
// An unquoted value is cut at the first " #", which keeps a
// bare `#rrggbb` intact while still dropping a spaced-off comment.
func ParseValue(value string) string {
	if rest, ok := strings.CutPrefix(value, `"`); ok {
		if inner, _, ok := strings.Cut(rest, `"`); ok {
			return strings.TrimSpace(inner)
		}
		return strings.TrimSpace(rest)
	}
	if before, _, ok := strings.Cut(value, " #"); ok {
		return strings.TrimSpace(before)
	}
	return value
}

// Parse extracts key-value pairs from a line-oriented config byte slice.
func Parse(data []byte) map[string]string {
	fields := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = ParseValue(strings.TrimSpace(value))
		if key == "" || value == "" {
			continue
		}
		fields[key] = value
	}
	return fields
}
