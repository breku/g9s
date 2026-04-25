package ui

import (
	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// cmdMode distinguishes how the CmdBar is being used.
type cmdMode int

const (
	modeCommand cmdMode = iota // ':' — navigate to a resource
	modeFilter                 // '/' — filter rows in the active view
)

// CmdBar is a bordered InputField that exposes ':' command and '/' filter
// modes. In command mode it uses tview's native dropdown autocomplete to
// suggest resource aliases.
//
// Callers register callbacks:
//   - OnCommand(text) — fired when the user submits a ':' command (Enter).
//   - OnFilter(text)  — fired on every keystroke in '/' mode.
//   - OnDismiss()     — fired when Escape is pressed or after a command runs.
type CmdBar struct {
	*tview.InputField

	mode cmdMode

	onCommand func(string)
	onFilter  func(string)
	onDismiss func()
}

// NewCmdBar creates an inactive CmdBar.
func NewCmdBar() *CmdBar {
	f := tview.NewInputField()
	f.SetBorder(true)
	f.SetBorderColor(tcell.ColorYellow)
	f.SetFieldBackgroundColor(tcell.ColorDefault)
	f.SetBackgroundColor(tcell.ColorDefault)
	f.SetFieldTextColor(tcell.ColorWhite)

	// Native dropdown autocomplete styling.
	f.SetAutocompleteStyles(
		tcell.ColorDarkBlue,
		tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorDarkBlue),
		tcell.StyleDefault.Foreground(tcell.ColorYellow).Background(tcell.ColorDarkBlue),
	)

	cb := &CmdBar{InputField: f}

	f.SetAutocompleteFunc(func(currentText string) []string {
		if cb.mode != modeCommand || currentText == "" {
			return nil
		}
		return model.CompleteCommand(currentText)
	})

	f.SetAutocompletedFunc(func(text string, index, source int) bool {
		if cb.mode != modeCommand {
			return false
		}
		// Accept on Tab / Enter / click; ignore the on-typing source so the
		// dropdown stays open while the user keeps typing.
		if source == tview.AutocompletedNavigate {
			return false
		}
		f.SetText(text)
		return true
	})

	f.SetChangedFunc(func(text string) {
		if cb.mode == modeFilter && cb.onFilter != nil {
			cb.onFilter(text)
		}
	})

	f.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			if cb.mode == modeCommand && cb.onCommand != nil {
				cb.onCommand(f.GetText())
			}
			if cb.onDismiss != nil {
				cb.onDismiss()
			}
		case tcell.KeyEscape:
			if cb.mode == modeFilter && cb.onFilter != nil {
				cb.onFilter("")
			}
			if cb.onDismiss != nil {
				cb.onDismiss()
			}
		}
	})

	return cb
}

// ActivateCommand switches the bar to command mode (':') and clears state.
func (cb *CmdBar) ActivateCommand() {
	cb.mode = modeCommand
	cb.SetLabel(" : ")
	cb.SetText("")
}

// ActivateFilter switches the bar to filter mode ('/') and clears state.
func (cb *CmdBar) ActivateFilter() {
	cb.mode = modeFilter
	cb.SetLabel(" / ")
	cb.SetText("")
}

// OnCommand registers the callback invoked when a ':' command is submitted.
func (cb *CmdBar) OnCommand(fn func(string)) { cb.onCommand = fn }

// OnFilter registers the callback invoked on every keystroke in '/' mode.
func (cb *CmdBar) OnFilter(fn func(string)) { cb.onFilter = fn }

// OnDismiss registers the callback invoked when the bar should be hidden.
func (cb *CmdBar) OnDismiss(fn func()) { cb.onDismiss = fn }
