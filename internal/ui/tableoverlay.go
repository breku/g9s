package ui

import (
	"context"
	"fmt"

	"github.com/atotto/clipboard"
	"github.com/brekol/g9s/internal/dao"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Compile-time assertions.
var (
	_ Overlay      = (*TableOverlay)(nil)
	_ Filterable   = (*TableOverlay)(nil)
	_ HintProvider = (*TableOverlay)(nil)
)

// RowAction is invoked by a TableOverlay when the user presses a custom
// key on a row. The action receives the app (for status reporting and
// pushing further overlays) and the selected dao.Row.
type RowAction func(a *App, row dao.Row)

// TableOverlay is a drill-down overlay that wraps a ResourceTable in the
// Overlay interface. It reuses the full ResourceTable rendering /
// pagination / filtering / polling pipeline while presenting itself as a
// stackable overlay above another view.
//
// Generic 'y' (YAML) and 'c' (copy) handling lives here rather than going
// through the global key dispatcher, because the dispatcher is bypassed
// while any overlay is active. Resource-specific bindings are supplied
// via Actions; their hint labels via ActionHints.
type TableOverlay struct {
	*ResourceTable

	app     *App
	title   string
	onClose func()
	cancel  context.CancelFunc

	// actions maps a rune (e.g. 'l', 'd') to the function that should run
	// when the user presses it on a selected row. Looked up before the
	// generic y/c handling so resource-specific keys take precedence.
	actions map[rune]RowAction

	// actionHints describes the bindings registered in actions for the
	// header hint bar. Generic hints (y, c, q/Esc) are always added by
	// Hints() so callers don't need to re-list them.
	actionHints []Hint
}

// NewTableOverlay wraps an already-constructed ResourceTable as an Overlay.
// title is shown in the table border.
func NewTableOverlay(a *App, title string, rt *ResourceTable) *TableOverlay {
	rt.SetTitle(fmt.Sprintf("[::b] %s ", title))
	to := &TableOverlay{
		ResourceTable: rt,
		app:           a,
		title:         title,
		actions:       map[rune]RowAction{},
	}

	// Wrap the existing input capture (which forwards PgDn to the model
	// for pagination) with overlay-specific keys (Esc/q close, y describe,
	// c copy, plus any registered RowActions). Order matters: registered
	// actions win first, then generic y/c, then close keys, then fall
	// through to the table's pre-existing capture for pagination/scrolling.
	prev := rt.Table.GetInputCapture()
	rt.Table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Esc clears an active filter first, mirrors the top-level Esc UX.
		if event.Key() == tcell.KeyEscape {
			if rt.Filter() != "" {
				rt.SetFilter("")
				return nil
			}
			to.close()
			return nil
		}
		if event.Rune() == 'q' {
			to.close()
			return nil
		}

		if r := event.Rune(); r != 0 {
			if act, ok := to.actions[r]; ok {
				row := rt.SelectedRow()
				if row != nil {
					act(a, row)
				}
				return nil
			}
		}

		switch event.Rune() {
		case 'y':
			row := rt.SelectedRow()
			if row == nil {
				return nil
			}
			yd, ok := rt.DAO().(dao.YAMLDescriber)
			if !ok {
				return nil
			}
			id := row.GetID()
			dv := NewDescribeView(a, fmt.Sprintf("YAML – %s", lastSegmentUI(id)), func(ctx context.Context) (string, error) {
				return yd.DescribeYAML(ctx, id)
			})
			a.PushOverlay(dv)
			return nil
		case 'c':
			row := rt.SelectedRow()
			if row == nil {
				a.Status(StatusInfo, "Nothing to copy")
				return nil
			}
			val, ok := row.CopyColumnValue()
			if !ok || val == "" {
				a.Status(StatusInfo, "Nothing to copy")
				return nil
			}
			if err := clipboard.WriteAll(val); err != nil {
				a.Status(StatusError, "Copy failed: "+err.Error())
				return nil
			}
			a.Status(StatusSuccess, "Copied: "+val)
			return nil
		}

		if prev != nil {
			return prev(event)
		}
		return event
	})

	return to
}

// AddAction registers a key binding that runs fn(a, selectedRow) when the
// user presses key on the overlay. hintLabel is shown in the header hint
// bar. Pass an empty hintLabel to suppress the hint.
func (to *TableOverlay) AddAction(key rune, hintLabel string, fn RowAction) {
	to.actions[key] = fn
	if hintLabel != "" {
		to.actionHints = append(to.actionHints, Hint{Key: string(key), Desc: hintLabel})
	}
}

// Primitive implements Overlay.
func (to *TableOverlay) Primitive() tview.Primitive { return to.ResourceTable.Primitive() }

// RenderLoading implements Overlay. Delegates to the embedded table.
func (to *TableOverlay) RenderLoading() { to.ResourceTable.RenderLoading() }

// OnClose implements Overlay.
func (to *TableOverlay) OnClose(fn func()) { to.onClose = fn }

// Start implements Overlay. Drives the underlying model.Table polling
// loop; blocks until the derived ctx is cancelled (via close()).
func (to *TableOverlay) Start(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	to.cancel = cancel
	defer cancel()
	if err := to.ResourceTable.Watch(ctx); err != nil {
		// Watch reports the initial-fetch error via TableLoadFailed which
		// the embedded ResourceTable renders into the table; nothing more
		// to do here.
		return
	}
	<-ctx.Done()
}

// Hints implements HintProvider. Combines registered RowAction hints with
// always-on generic hints (y if DAO supports it, c, close).
func (to *TableOverlay) Hints() []Hint {
	hints := append([]Hint{}, to.actionHints...)
	if _, ok := to.ResourceTable.DAO().(dao.YAMLDescriber); ok {
		hints = append(hints, Hint{Key: "y", Desc: "YAML"})
	}
	hints = append(hints,
		Hint{Key: "c", Desc: "Copy"},
		Hint{Key: "q/Esc", Desc: "Close"},
	)
	return hints
}

// close cancels background work and notifies the App so it can pop this
// overlay off the stack.
func (to *TableOverlay) close() {
	if to.cancel != nil {
		to.cancel()
	}
	if to.onClose != nil {
		to.onClose()
	}
}
