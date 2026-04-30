package ui

import (
	"context"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Ensure ConfirmOverlay satisfies Overlay and HintProvider at compile time.
var (
	_ Overlay      = (*ConfirmOverlay)(nil)
	_ HintProvider = (*ConfirmOverlay)(nil)
)

// ConfirmOverlay is a small modal that asks the user to confirm a destructive
// action with Enter/Esc.
//
// On confirm it closes itself and invokes onConfirm; the caller is then
// responsible for launching the actual work (typically via app.TrackOp so
// progress and errors surface on the status bar). This keeps the overlay
// purely a yes/no prompt and avoids duplicate result-reporting paths.
type ConfirmOverlay struct {
	modal *tview.Grid

	app       *App
	title     string
	prompt    string
	onConfirm func()
	onClose   func()
}

// NewConfirmOverlay constructs a confirmation modal.
//   - title:     shown in the border, e.g. "Delete VM".
//   - prompt:    body text, e.g. "Delete instance my-vm in zone us-central1-a?".
//   - onConfirm: invoked on Enter, AFTER the overlay has closed.
func NewConfirmOverlay(a *App, title, prompt string, onConfirm func()) *ConfirmOverlay {
	co := &ConfirmOverlay{
		app:       a,
		title:     title,
		prompt:    prompt,
		onConfirm: onConfirm,
	}

	body := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" " + prompt)
	body.SetBackgroundColor(AppTheme.BackgroundColor)

	hint := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Enter/y[white] Confirm  [yellow]Esc[white] Cancel")
	hint.SetBackgroundColor(AppTheme.BackgroundColor)

	inner := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewBox().SetBackgroundColor(AppTheme.BackgroundColor), 1, 0, false).
		AddItem(body, 1, 0, false).
		AddItem(tview.NewBox().SetBackgroundColor(AppTheme.BackgroundColor), 0, 1, false).
		AddItem(hint, 1, 0, false)
	inner.SetBackgroundColor(AppTheme.BackgroundColor)

	outer := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(inner, 0, 1, true)
	outer.SetBorder(true)
	outer.SetBorderColor(AppTheme.HighlightColor)
	outer.SetTitle(" " + title + " ")
	outer.SetTitleColor(tcell.ColorWhite)
	outer.SetTitleAlign(tview.AlignCenter)
	outer.SetBackgroundColor(AppTheme.BackgroundColor)

	// Centre dialog: 60 wide, 7 tall — slightly shorter than before since
	// we no longer reserve space for an in-overlay status line.
	grid := tview.NewGrid().
		SetColumns(0, 60, 0).
		SetRows(0, 7, 0).
		AddItem(outer, 1, 1, 1, 1, 0, 0, true)
	grid.SetBackgroundColor(tcell.ColorDefault)

	// Capture keys at the modal level so we don't depend on a focused input.
	grid.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			co.confirm()
			return nil
		case tcell.KeyEscape:
			co.close()
			return nil
		}
		switch event.Rune() {
		case 'y', 'Y':
			co.confirm()
			return nil
		case 'q':
			co.close()
			return nil
		}
		return nil
	})

	co.modal = grid
	return co
}

// Primitive implements Overlay.
func (co *ConfirmOverlay) Primitive() tview.Primitive { return co.modal }

// RenderLoading implements Overlay; nothing to render before user input.
func (co *ConfirmOverlay) RenderLoading() {}

// Start implements Overlay; purely interactive.
func (co *ConfirmOverlay) Start(_ context.Context) {}

// OnClose implements Overlay.
func (co *ConfirmOverlay) OnClose(fn func()) { co.onClose = fn }

// Hints implements HintProvider.
func (co *ConfirmOverlay) Hints() []Hint {
	return []Hint{
		{Key: "Enter/y", Desc: "Confirm"},
		{Key: "Esc", Desc: "Cancel"},
	}
}

// confirm closes the overlay and invokes the onConfirm callback. The
// callback is invoked AFTER close so that the caller can immediately push
// another overlay (e.g. an error) without UI ordering surprises.
func (co *ConfirmOverlay) confirm() {
	co.close()
	if co.onConfirm != nil {
		co.onConfirm()
	}
}

func (co *ConfirmOverlay) close() {
	if co.onClose != nil {
		co.onClose()
	}
}
