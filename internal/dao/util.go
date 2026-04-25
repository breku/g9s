package dao

import (
	"strings"
	"time"
)

// formatTime formats a time.Time in the local timezone as "2006-01-02 15:04",
// or "—" if the value is zero.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// lastSegment returns the last "/" segment of a fully-qualified resource name.
// e.g. "projects/p/locations/l/services/foo" → "foo"
func lastSegment(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return name
	}
	return parts[len(parts)-1]
}
