package ui

import (
	"strings"

	"github.com/rivo/tview"
)

// highlight returns a tview-color-tagged copy of the given text, picking a
// JSON or YAML highlighter based on a cheap shape sniff. Falls back to the
// escaped raw text if neither matches.
func highlight(src string) string {
	t := strings.TrimLeft(src, " \t\r\n")
	if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		return colorizeJSON(src)
	}
	return colorizeYAML(src)
}

// colorizeJSON returns a tview-color-tagged copy of the given JSON text.
//
// Token colours:
//   - object keys (string immediately followed by ':')   → cyan
//   - string values                                      → green
//   - numbers                                            → yellow
//   - true / false / null                                → yellow
//   - structural punctuation { } [ ] , :                 → default
//
// Whitespace and unknown bytes are passed through verbatim. The function is
// intentionally a hand-rolled scanner (no encoding/json) so it preserves the
// exact formatting (indentation, key order) produced upstream.
func colorizeJSON(src string) string {
	var b strings.Builder
	b.Grow(len(src) + len(src)/4)

	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '"':
			end := scanJSONString(src, i)
			tok := src[i:end]
			// A key is a string immediately followed (after optional ws) by ':'.
			if isJSONKey(src, end) {
				b.WriteString("[#7fdbff]")
				b.WriteString(tview.Escape(tok))
				b.WriteString("[-]")
			} else {
				b.WriteString("[green]")
				b.WriteString(tview.Escape(tok))
				b.WriteString("[-]")
			}
			i = end

		case c == '-' || c == '+' || (c >= '0' && c <= '9'):
			end := scanJSONNumber(src, i)
			b.WriteString("[yellow]")
			b.WriteString(src[i:end])
			b.WriteString("[-]")
			i = end

		case c == 't' || c == 'f' || c == 'n':
			if end, ok := scanJSONKeyword(src, i); ok {
				b.WriteString("[yellow]")
				b.WriteString(src[i:end])
				b.WriteString("[-]")
				i = end
				continue
			}
			b.WriteByte(c)
			i++

		case c == '[':
			// Escape '[' so tview doesn't interpret it as a colour-tag opener.
			b.WriteString(tview.Escape("["))
			i++

		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// scanJSONString returns the index just past the closing quote of the
// JSON string starting at src[i] (which must be '"'). Honours backslash
// escapes. Returns len(src) if the string is unterminated.
func scanJSONString(src string, i int) int {
	j := i + 1
	for j < len(src) {
		switch src[j] {
		case '\\':
			j += 2 // skip the escape and the next byte
		case '"':
			return j + 1
		default:
			j++
		}
	}
	return len(src)
}

// isJSONKey reports whether the next non-whitespace byte after pos is ':'.
func isJSONKey(src string, pos int) bool {
	for j := pos; j < len(src); j++ {
		switch src[j] {
		case ' ', '\t', '\r', '\n':
			continue
		case ':':
			return true
		default:
			return false
		}
	}
	return false
}

// scanJSONNumber returns the index just past the JSON number starting at i.
func scanJSONNumber(src string, i int) int {
	j := i
	if src[j] == '-' || src[j] == '+' {
		j++
	}
	for j < len(src) {
		c := src[j]
		if (c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-' {
			j++
			continue
		}
		break
	}
	return j
}

// scanJSONKeyword matches "true", "false", or "null" at src[i:].
func scanJSONKeyword(src string, i int) (int, bool) {
	for _, kw := range []string{"true", "false", "null"} {
		if strings.HasPrefix(src[i:], kw) {
			return i + len(kw), true
		}
	}
	return i, false
}
