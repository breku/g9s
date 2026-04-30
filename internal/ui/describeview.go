package ui

import (
	"context"
	"fmt"

	"github.com/atotto/clipboard"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// Ensure DescribeView satisfies Overlay at compile time.
var _ Overlay = (*DescribeView)(nil)

// DescribeView is a generic full-screen overlay that fetches and displays
// a text description of a resource. The content is fetched once via a
// caller-supplied function; no streaming/polling.
type DescribeView struct {
	*tview.TextView

	app     *App
	title   string
	fetch   func(ctx context.Context) (string, error)
	onClose func()

	// content holds the last successfully fetched payload, used by the
	// 'c' copy binding so it doesn't have to refetch.
	content string

	// copyHint is the label shown in the hint bar for the 'c' binding.
	// Defaults to "Copy" in NewDescribeView; override via EnableCopy.
	copyHint string
}

// NewDescribeView creates a DescribeView.
// title is shown in the border. fetch is called in Start to retrieve content.
func NewDescribeView(a *App, title string, fetch func(ctx context.Context) (string, error)) *DescribeView {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(false)
	tv.SetBorder(true)
	tv.SetBorderColor(tcell.ColorBlue)
	tv.SetTitle(fmt.Sprintf(" %s ", title))
	tv.SetTitleColor(tcell.ColorWhite)
	tv.SetTitleAlign(tview.AlignCenter)
	tv.SetBackgroundColor(AppTheme.BackgroundColor)

	dv := &DescribeView{
		TextView: tv,
		app:      a,
		title:    title,
		fetch:    fetch,
		copyHint: "Copy",
	}

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Rune() == 'q' {
			dv.close()
			return nil
		}
		if event.Rune() == 'c' {
			dv.copyContent()
			return nil
		}
		return event
	})

	return dv
}

// EnableCopy overrides the default "Copy" label shown next to the 'c' hint.
// Copy itself is always enabled; this only changes the wording (e.g.
// "Copy value" for secrets, "Copy YAML" for describe overlays).
func (dv *DescribeView) EnableCopy(hintLabel string) {
	if hintLabel == "" {
		return
	}
	dv.copyHint = hintLabel
}

// copyContent writes the last fetched payload to the system clipboard and
// surfaces the outcome on the global status bar.
// No-op while the fetch is still in flight (content is empty).
func (dv *DescribeView) copyContent() {
	if dv.content == "" {
		dv.app.Status(StatusInfo, "Nothing to copy yet — still loading.")
		return
	}
	if err := clipboard.WriteAll(dv.content); err != nil {
		dv.app.Status(StatusError, fmt.Sprintf("Copy failed: %v", err))
		return
	}
	dv.app.Status(StatusSuccess, fmt.Sprintf("%s: copied to clipboard", dv.copyHint))
}

// Primitive implements Overlay.
func (dv *DescribeView) Primitive() tview.Primitive { return dv.TextView }

// RenderLoading implements Overlay.
func (dv *DescribeView) RenderLoading() {
	dv.TextView.SetText(" Loading…")
}

// OnClose implements Overlay.
func (dv *DescribeView) OnClose(fn func()) { dv.onClose = fn }

// Hints implements HintProvider.
func (dv *DescribeView) Hints() []Hint {
	return []Hint{
		{Key: "q/Esc", Desc: "Close"},
		{Key: "c", Desc: dv.copyHint},
	}
}

// Start implements Overlay. Fetches content and renders it.
func (dv *DescribeView) Start(ctx context.Context) {
	content, err := dv.fetch(ctx)
	if err != nil {
		log.Error().Err(err).Str("title", dv.title).Msg("describe fetch failed")
		e := err
		dv.app.tview.QueueUpdateDraw(func() {
			dv.TextView.SetText(fmt.Sprintf("[red]Error: %v[white]", e))
		})
		return
	}
	dv.app.tview.QueueUpdateDraw(func() {
		dv.content = content
		dv.TextView.SetText(highlight(content))
		dv.TextView.ScrollToBeginning()
	})
}

// close calls the registered onClose callback.
func (dv *DescribeView) close() {
	if dv.onClose != nil {
		dv.onClose()
	}
}
