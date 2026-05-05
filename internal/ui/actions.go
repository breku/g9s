package ui

import (
	"context"
	"fmt"

	"github.com/atotto/clipboard"
	"github.com/brekol/g9s/internal/dao"
	"github.com/gdamore/tcell/v2"
)

// combinedHints implements HintProvider by concatenating multiple Hint slices.
type combinedHints []Hint

func (c combinedHints) Hints() []Hint { return []Hint(c) }

// viewHintProvider returns a HintProvider that combines the generic
// dispatcher hints with the view's own hints (if it implements HintProvider).
// Generic hints come first so they're consistently positioned across views.
func viewHintProvider(view ResourceView) HintProvider {
	hints := genericHints(view)
	if hp, ok := view.(HintProvider); ok {
		hints = append(hints, hp.Hints()...)
	}
	if len(hints) == 0 {
		return nil
	}
	return combinedHints(hints)
}

// rowSelector is satisfied by any view exposing the currently selected row.
// All ResourceViews satisfy it via their embedded *ResourceTable.
type rowSelector interface {
	SelectedRow() dao.Row
}

// genericHints returns hints for the keys handled by handleGenericKey,
// gated on the current view's DAO capabilities. 'c' is always advertised
// because every Row implements CopyColumnValue (it may no-op at runtime).
// PgDn is intentionally NOT advertised here — the table itself shows a
// "↓ PageDown to load more…" footer when more pages are available, which
// is sufficient and avoids cluttering the header.
func genericHints(view ResourceView) []Hint {
	if view == nil {
		return nil
	}
	var hints []Hint
	d := view.DAO()
	if _, ok := d.(dao.YAMLDescriber); ok {
		hints = append(hints, Hint{Key: "y", Desc: "YAML"})
	}
	hints = append(hints, Hint{Key: "c", Desc: "Copy"})
	return hints
}

// handleGenericKey dispatches the cross-resource keys ('y', 'c') by
// inspecting the active view's DAO capabilities. Returns true when the key
// was consumed so the caller can short-circuit further handling.
func handleGenericKey(a *App, view ResourceView, event *tcell.EventKey) bool {
	if view == nil {
		return false
	}
	rs, ok := view.(rowSelector)
	if !ok {
		return false
	}
	row := rs.SelectedRow()
	if row == nil {
		// Still consume the key so the resource view doesn't see it.
		switch event.Rune() {
		case 'y', 'c':
			return true
		}
		return false
	}

	switch event.Rune() {
	case 'y':
		yd, ok := view.DAO().(dao.YAMLDescriber)
		if !ok {
			return false
		}
		id := row.GetID()
		title := fmt.Sprintf("YAML – %s", lastSegmentUI(id))
		dv := NewDescribeView(a, title, func(ctx context.Context) (string, error) {
			return yd.DescribeYAML(ctx, id)
		})
		a.PushOverlay(dv)
		return true

	case 'c':
		val, ok := row.CopyColumnValue()
		if !ok || val == "" {
			a.Status(StatusInfo, "Nothing to copy")
			return true
		}
		if err := clipboard.WriteAll(val); err != nil {
			a.Status(StatusError, "Copy failed: "+err.Error())
			return true
		}
		a.Status(StatusSuccess, "Copied: "+val)
		return true
	}
	return false
}
