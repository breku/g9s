package ui

import (
	"context"
	"fmt"

	"github.com/brekol/g9s/internal/dao/cloudbuild"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// Ensure RunOverlay satisfies Overlay and HintProvider at compile time.
var (
	_ Overlay      = (*RunOverlay)(nil)
	_ HintProvider = (*RunOverlay)(nil)
)

// RunOverlay is a small modal overlay that lets the user confirm (and optionally
// edit) the branch before triggering a Cloud Build run.
type RunOverlay struct {
	// modal is the root primitive passed to tview.Pages — a full-screen Grid
	// that centres the dialog box.
	modal *tview.Grid

	app         *App
	dao         *cloudbuild.CloudBuild
	project     string
	triggerID   string
	triggerName string

	input   *tview.InputField
	status  *tview.TextView
	onClose func()
}

// NewRunOverlay creates a RunOverlay for the given trigger, pre-filled with branch.
func NewRunOverlay(a *App, d *cloudbuild.CloudBuild, triggerName, project, triggerID, branch string) *RunOverlay {
	ro := &RunOverlay{
		app:         a,
		dao:         d,
		project:     project,
		triggerID:   triggerID,
		triggerName: triggerName,
	}

	// Branch input field.
	input := tview.NewInputField().
		SetLabel(" Branch: ").
		SetText(branch).
		SetFieldWidth(40).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetFieldTextColor(tcell.ColorWhite).
		SetLabelColor(tcell.ColorYellow)
	ro.input = input

	// Status line (shows "Running…" / error after submission).
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText("")
	status.SetBackgroundColor(AppTheme.BackgroundColor)
	ro.status = status

	// Layout: label + input on one row, status below, hint at bottom.
	hint := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Enter[white] Run  [yellow]Esc[white] Cancel")
	hint.SetBackgroundColor(AppTheme.BackgroundColor)

	inner := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewBox().SetBackgroundColor(AppTheme.BackgroundColor), 1, 0, false).
		AddItem(input, 1, 0, true).
		AddItem(tview.NewBox().SetBackgroundColor(AppTheme.BackgroundColor), 1, 0, false).
		AddItem(status, 1, 0, false).
		AddItem(tview.NewBox().SetBackgroundColor(AppTheme.BackgroundColor), 0, 1, false).
		AddItem(hint, 1, 0, false)
	inner.SetBackgroundColor(AppTheme.BackgroundColor)

	// Outer box with border.
	outer := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(inner, 0, 1, true)
	outer.SetBorder(true)
	outer.SetBorderColor(AppTheme.HighlightColor)
	outer.SetTitle(fmt.Sprintf(" Run – %s ", triggerName))
	outer.SetTitleColor(tcell.ColorWhite)
	outer.SetTitleAlign(tview.AlignCenter)
	outer.SetBackgroundColor(AppTheme.BackgroundColor)

	// Centre the dialog in a full-screen transparent grid (60 wide, 9 tall).
	grid := tview.NewGrid().
		SetColumns(0, 60, 0).
		SetRows(0, 9, 0).
		AddItem(outer, 1, 1, 1, 1, 0, 0, true)
	grid.SetBackgroundColor(tcell.ColorDefault)

	ro.modal = grid

	// Key handling on the input field.
	input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			ro.submit()
		case tcell.KeyEscape:
			ro.close()
		}
	})
	input.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			ro.close()
			return nil
		}
		return event
	})

	return ro
}

// Primitive implements Overlay.
func (ro *RunOverlay) Primitive() tview.Primitive { return ro.modal }

// RenderLoading implements Overlay. Nothing to show before Start runs.
func (ro *RunOverlay) RenderLoading() {}

// OnClose implements Overlay.
func (ro *RunOverlay) OnClose(fn func()) { ro.onClose = fn }

// Hints implements HintProvider.
func (ro *RunOverlay) Hints() []Hint {
	return []Hint{
		{Key: "Enter", Desc: "Run"},
		{Key: "Esc", Desc: "Cancel"},
	}
}

// Start implements Overlay. The RunOverlay is purely interactive so Start is a
// no-op — the user drives everything via key presses.
func (ro *RunOverlay) Start(_ context.Context) {}

// submit reads the branch, calls cloudbuild.RunTrigger, and shows the result.
// Called from the InputField's DoneFunc — already on the main tview goroutine.
func (ro *RunOverlay) submit() {
	branch := ro.input.GetText()
	if branch == "" {
		ro.status.SetText("[red]Branch must not be empty.")
		return
	}

	// Update UI directly — we're already on the main goroutine.
	ro.status.SetText(" [yellow]Triggering build…")
	ro.input.SetDisabled(true)

	go func() {
		err := ro.dao.RunTrigger(ro.app.ctx, ro.project, ro.triggerID, branch)
		if err != nil {
			log.Error().Err(err).Str("trigger", ro.triggerID).Msg("run overlay: trigger failed")
			ro.app.tview.QueueUpdateDraw(func() {
				ro.status.SetText(fmt.Sprintf("[red]Error: %v", err))
				ro.input.SetDisabled(false)
			})
			return
		}
		ro.app.tview.QueueUpdateDraw(func() {
			ro.status.SetText(fmt.Sprintf("[green]Build triggered on branch '%s'.", branch))
			// Auto-close after success so the user sees it.
			ro.onClose()
		})
	}()
}

// close calls the registered onClose callback.
func (ro *RunOverlay) close() {
	if ro.onClose != nil {
		ro.onClose()
	}
}
