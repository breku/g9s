package ui

import (
	"context"
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// Ensure ConfirmOverlay satisfies Overlay and HintProvider at compile time.
var (
	_ Overlay      = (*ConfirmOverlay)(nil)
	_ HintProvider = (*ConfirmOverlay)(nil)
)

// ConfirmOverlay is a small modal that asks the user to confirm a destructive
// action with y/N. On 'y' it runs the supplied action in a background
// goroutine; on success the overlay closes itself, on failure it shows the
// error and lets the user dismiss with Esc/n.
type ConfirmOverlay struct {
	modal *tview.Grid

	app     *App
	title   string
	prompt  string
	action  func(ctx context.Context) error
	status  *tview.TextView
	onClose func()

	// running guards against re-entry while the action is in flight.
	running bool
}

// NewConfirmOverlay constructs a confirmation modal.
//   - title:  shown in the border, e.g. "Delete VM".
//   - prompt: body text, e.g. "Delete instance my-vm in zone us-central1-a?".
//   - action: invoked on 'y'; runs on a goroutine off the main tview thread.
func NewConfirmOverlay(a *App, title, prompt string, action func(ctx context.Context) error) *ConfirmOverlay {
	co := &ConfirmOverlay{
		app:    a,
		title:  title,
		prompt: prompt,
		action: action,
	}

	body := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" " + prompt)
	body.SetBackgroundColor(AppTheme.BackgroundColor)

	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText("")
	status.SetBackgroundColor(AppTheme.BackgroundColor)
	co.status = status

	hint := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Enter[white] Confirm  [yellow]Esc[white] Cancel")
	hint.SetBackgroundColor(AppTheme.BackgroundColor)

	inner := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewBox().SetBackgroundColor(AppTheme.BackgroundColor), 1, 0, false).
		AddItem(body, 1, 0, false).
		AddItem(tview.NewBox().SetBackgroundColor(AppTheme.BackgroundColor), 1, 0, false).
		AddItem(status, 1, 0, false).
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

	// Centre dialog: 60 wide, 9 tall — same dimensions as RunOverlay.
	grid := tview.NewGrid().
		SetColumns(0, 60, 0).
		SetRows(0, 9, 0).
		AddItem(outer, 1, 1, 1, 1, 0, 0, true)
	grid.SetBackgroundColor(tcell.ColorDefault)

	// Capture keys at the modal level so we don't depend on a focused input.
	grid.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if co.running {
			// Block all keys while the action is running.
			return nil
		}
		switch event.Key() {
		case tcell.KeyEnter:
			co.submit()
			return nil
		case tcell.KeyEscape:
			co.close()
			return nil
		}
		switch event.Rune() {
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
		{Key: "Enter", Desc: "Confirm"},
		{Key: "Esc", Desc: "Cancel"},
	}
}

// submit runs the action on a goroutine. We're on the main goroutine here
// (called from the input capture), so direct mutations of status are safe.
func (co *ConfirmOverlay) submit() {
	co.running = true
	co.status.SetText(" [yellow]Working…")

	go func() {
		err := co.action(co.app.ctx)
		co.app.tview.QueueUpdateDraw(func() {
			if err != nil {
				log.Error().Err(err).Str("title", co.title).Msg("confirm overlay: action failed")
				co.status.SetText(fmt.Sprintf("[red]Error: %v", err))
				co.running = false
				return
			}
			co.close()
		})
	}()
}

func (co *ConfirmOverlay) close() {
	if co.onClose != nil {
		co.onClose()
	}
}
