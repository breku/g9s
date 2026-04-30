package dao

import (
	"strings"
	"time"
)

// FormatTime formats a time.Time in the local timezone as "2006-01-02 15:04",
// or "—" if the value is zero. Exported for use by subpackage DAOs.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// LastSegment returns the last "/" segment of a fully-qualified resource name.
// e.g. "projects/p/locations/l/services/foo" → "foo"
// Exported for use by subpackage DAOs.
func LastSegment(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return name
	}
	return parts[len(parts)-1]
}
