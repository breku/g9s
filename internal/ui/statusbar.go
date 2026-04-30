package ui

import (
	"sync"
	"time"

	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// StatusLevel categorises a status message for colouring and auto-clear behaviour.
type StatusLevel int

const (
	// StatusInfo is for neutral progress messages ("Deploying foo…").
	StatusInfo StatusLevel = iota
	// StatusSuccess is for completed actions ("Copied", "Deploy succeeded").
	StatusSuccess
	// StatusWarning is for non-fatal issues.
	StatusWarning
	// StatusError is for failures. Errors persist on the bar until replaced;
	// other levels auto-clear after statusAutoClear.
	StatusError
)

// statusAutoClear is the time after which a non-error status message is
// cleared automatically. Errors stick until the next message replaces them
// so the user always sees the latest failure.
const statusAutoClear = 5 * time.Second

// statusMaxLen is the maximum number of characters of a status message
// rendered in the bar. Longer messages are truncated with "…"; the full
// text is always logged at the corresponding zerolog level.
const statusMaxLen = 200

// StatusBar is the single-line app-wide bar at the bottom of the screen.
//
// Messages are dispatched via App.Status; the bar handles colouring,
// truncation, auto-clear, and zerolog mirroring. All updates are scheduled
// onto the tview main goroutine via QueueUpdateDraw so the bar is safe to
// call from any goroutine.
type StatusBar struct {
	*tview.TextView

	app *App

	mu      sync.Mutex
	clearAt time.Time // when the current message should auto-clear; zero for errors
	gen     uint64    // monotonic generation; clears only fire if gen unchanged
}

// NewStatusBar creates an empty status bar bound to the given app.
func NewStatusBar(a *App) *StatusBar {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(false).
		SetWrap(false)
	tv.SetBackgroundColor(AppTheme.BackgroundColor)
	return &StatusBar{TextView: tv, app: a}
}

// Set replaces the current status with msg at the given level. Safe to call
// from any goroutine — when invoked off the main goroutine the redraw is
// scheduled via QueueUpdateDraw; when invoked on the main goroutine the
// SetText call is performed inline (calling QueueUpdateDraw from the main
// goroutine deadlocks tview). The full text is also logged at the matching
// zerolog level so long errors remain reviewable in the log file.
func (s *StatusBar) Set(level StatusLevel, msg string) {
	logStatus(level, msg)

	display := truncate(msg, statusMaxLen)
	colored := " " + colourise(level, display)

	s.mu.Lock()
	s.gen++
	gen := s.gen
	if level == StatusError {
		s.clearAt = time.Time{}
	} else {
		s.clearAt = time.Now().Add(statusAutoClear)
	}
	s.mu.Unlock()

	s.app.runOnUI(func() { s.SetText(colored) })

	// Schedule auto-clear for non-error levels. The generation guard means a
	// follow-up Set call cancels the pending clear automatically.
	if level != StatusError {
		go func() {
			time.Sleep(statusAutoClear)
			s.mu.Lock()
			current := s.gen
			s.mu.Unlock()
			if current != gen {
				return
			}
			s.app.tview.QueueUpdateDraw(func() {
				s.SetText("")
			})
		}()
	}
}

// Clear blanks the status bar immediately.
func (s *StatusBar) Clear() {
	s.mu.Lock()
	s.gen++
	s.mu.Unlock()
	s.app.runOnUI(func() { s.SetText("") })
}

// colourise wraps msg in the dynamic-colour tag for its level.
func colourise(level StatusLevel, msg string) string {
	switch level {
	case StatusSuccess:
		return "[green]" + msg + "[white]"
	case StatusWarning:
		return "[yellow]" + msg + "[white]"
	case StatusError:
		return "[red]" + msg + "[white]"
	default:
		return msg
	}
}

// logStatus mirrors the message to zerolog at the equivalent level so the
// full text (no truncation) is always recoverable from the log file.
func logStatus(level StatusLevel, msg string) {
	switch level {
	case StatusSuccess:
		log.Info().Str("status", "success").Msg(msg)
	case StatusWarning:
		log.Warn().Str("status", "warning").Msg(msg)
	case StatusError:
		log.Error().Str("status", "error").Msg(msg)
	default:
		log.Info().Str("status", "info").Msg(msg)
	}
}

// truncate clips s to max runes, appending "…" when shortened. Operates on
// runes (not bytes) so multi-byte characters are not split.
func truncate(s string, max int) string {
	if max <= 1 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// statusBarHeight is the fixed row count for the status bar in the root flex.
const statusBarHeight = 1

var _ tview.Primitive = (*StatusBar)(nil)
