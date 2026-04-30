package ui

import (
	"strings"

	"github.com/rivo/tview"
)

// colorizeYAML returns a tview-color-tagged copy of the given YAML text.
// It is a line-based, regex-free highlighter — robust enough for the
// gcloud-style outputs g9s shows, intentionally not a full YAML parser.
//
// Highlighting rules (per line, after indentation):
//   - "# comment"             → gray
//   - "- " list bullet        → magenta dash, value re-coloured below
//   - "key: value"            → cyan key, value coloured by type
//   - bare value (block list, fold continuation) → coloured by type
//
// Value typing:
//   - quoted string ("..."/'...')  → green
//   - true/false/null/~/yes/no/on/off (case-insensitive) → yellow
//   - looks like a number (-?[0-9.]+) → yellow
//   - everything else                → default colour (no tag)
//
// All literal '[' characters in the source are escaped with tview.Escape so
// they aren't interpreted as colour tags.
func colorizeYAML(src string) string {
	var b strings.Builder
	b.Grow(len(src) + len(src)/4)

	for _, line := range strings.SplitAfter(src, "\n") {
		// SplitAfter keeps the trailing "\n" on each line so we don't have
		// to re-add it. The final element is "" if src ends with "\n".
		nl := ""
		if strings.HasSuffix(line, "\n") {
			nl = "\n"
			line = line[:len(line)-1]
		}
		b.WriteString(colorizeYAMLLine(line))
		b.WriteString(nl)
	}
	return b.String()
}

func colorizeYAMLLine(line string) string {
	// Preserve leading whitespace verbatim.
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	rest := line[len(indent):]

	if rest == "" {
		return indent
	}

	// Comment line.
	if strings.HasPrefix(rest, "#") {
		return indent + "[gray]" + tview.Escape(rest) + "[-]"
	}

	// List bullet "- ...". The remainder may itself be a "key: value".
	if strings.HasPrefix(rest, "- ") || rest == "-" {
		dash := "[magenta]-[-]"
		after := strings.TrimPrefix(rest, "-")
		if after == "" {
			return indent + dash
		}
		// after starts with a space; keep it, then recurse on the payload.
		// Wrap the payload through colorizeYAMLLine so "key: value" inside
		// list items is highlighted the same way as top-level mappings.
		return indent + dash + " " + colorizeYAMLLine(strings.TrimPrefix(after, " "))
	}

	// "key: value" — split on the FIRST colon that is followed by a space
	// or end-of-line. This avoids misclassifying URLs or timestamps.
	if k, v, ok := splitYAMLKeyValue(rest); ok {
		out := "[#7fdbff]" + tview.Escape(k) + "[-]:"
		if v == "" {
			return indent + out
		}
		return indent + out + " " + colorizeYAMLValue(v)
	}

	// Bare value (block-scalar continuation, item value on its own line, etc.).
	return indent + colorizeYAMLValue(rest)
}

// splitYAMLKeyValue splits "key: value" into ("key", "value"). It looks for
// the first ": " (or trailing ":") and refuses to split if the colon appears
// inside quotes. Returns ok=false if no key/value separator is found.
func splitYAMLKeyValue(s string) (key, val string, ok bool) {
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ':':
			if inSingle || inDouble {
				continue
			}
			// Match ": " or trailing ":" at end of line.
			if i+1 == len(s) {
				return s[:i], "", true
			}
			if s[i+1] == ' ' || s[i+1] == '\t' {
				return s[:i], strings.TrimLeft(s[i+1:], " \t"), true
			}
		}
	}
	return "", "", false
}

func colorizeYAMLValue(v string) string {
	if v == "" {
		return ""
	}
	// Trailing inline comment: "value # note".
	if i := indexUnquoted(v, " #"); i >= 0 {
		return colorizeYAMLValue(strings.TrimRight(v[:i], " \t")) +
			" [gray]" + tview.Escape(v[i+1:]) + "[-]"
	}

	// Quoted string.
	if (strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) && len(v) >= 2) ||
		(strings.HasPrefix(v, `'`) && strings.HasSuffix(v, `'`) && len(v) >= 2) {
		return "[green]" + tview.Escape(v) + "[-]"
	}

	// Keywords (case-insensitive).
	switch strings.ToLower(v) {
	case "true", "false", "null", "~", "yes", "no", "on", "off":
		return "[yellow]" + tview.Escape(v) + "[-]"
	}

	if looksLikeNumber(v) {
		return "[yellow]" + tview.Escape(v) + "[-]"
	}

	// Block scalar indicators "|" or ">" (optionally followed by + or -).
	if v == "|" || v == ">" || v == "|-" || v == ">-" || v == "|+" || v == ">+" {
		return "[magenta]" + v + "[-]"
	}

	return tview.Escape(v)
}

// indexUnquoted returns the index of needle in s, ignoring matches that occur
// inside single- or double-quoted regions.
func indexUnquoted(s, needle string) int {
	inSingle, inDouble := false, false
	for i := 0; i+len(needle) <= len(s); i++ {
		c := s[i]
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
				continue
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
				continue
			}
		}
		if !inSingle && !inDouble && s[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func looksLikeNumber(s string) bool {
	if s == "" {
		return false
	}
	hasDigit := false
	for i, c := range s {
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E':
			// Sign only at start; exponent letters anywhere after a digit.
			if (c == '-' || c == '+') && i != 0 {
				if prev := s[i-1]; prev != 'e' && prev != 'E' {
					return false
				}
			}
		default:
			return false
		}
	}
	return hasDigit
}
