package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// cmdMode distinguishes how the CmdBar is being used.
type cmdMode int

const (
	modeCommand cmdMode = iota // ':' — navigate to a resource
	modeFilter                 // '/' — filter rows in the active view
)

// CmdBar is the single-line input bar shown at the top of the layout.
// It is hidden (zero height) when inactive and shown when the user presses
// ':' (command mode) or '/' (filter mode).
//
// Callers register callbacks:
//   - OnCommand(text) — fired when the user submits a ':' command (Enter).
//   - OnFilter(text)  — fired on every keystroke in '/' mode.
//   - OnDismiss()     — fired when Escape is pressed.
type CmdBar struct {
	*tview.InputField

	mode      cmdMode
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
	f.SetLabelColor(tcell.ColorYellow)

	cb := &CmdBar{InputField: f}

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
			// Clear filter when escaping filter mode.
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

// ActivateCommand switches the bar to command mode (':') and focuses it.
func (cb *CmdBar) ActivateCommand() {
	cb.mode = modeCommand
	cb.SetLabel(" :")
	cb.SetText("")
}

// ActivateFilter switches the bar to filter mode ('/') and focuses it.
func (cb *CmdBar) ActivateFilter() {
	cb.mode = modeFilter
	cb.SetLabel(" /")
	cb.SetText("")
}

// OnCommand registers the callback invoked when a ':' command is submitted.
func (cb *CmdBar) OnCommand(fn func(string)) { cb.onCommand = fn }

// OnFilter registers the callback invoked on every keystroke in '/' mode.
func (cb *CmdBar) OnFilter(fn func(string)) { cb.onFilter = fn }

// OnDismiss registers the callback invoked when the bar should be hidden.
func (cb *CmdBar) OnDismiss(fn func()) { cb.onDismiss = fn }
