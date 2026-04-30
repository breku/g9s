package ui

import (
	"fmt"
	"strings"

	"github.com/brekol/g9s/internal/dao"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ResourceTable is a reusable tview.Table wrapper that renders dao.TableData.
// It stores the last-received data so it can re-render when the filter changes
// without waiting for the next model poll.
//
// ResourceTable also implements model.TableListener so simple resource views
// can embed it and inherit the standard "render or show error" glue without
// reimplementing TableDataChanged / TableLoadFailed in every view. Views that
// need custom behaviour (e.g. accumulating pages) can shadow these methods —
// Go's method resolution picks the outer type's implementation when present.
type ResourceTable struct {
	*tview.Table

	app      *App
	title    string // resource label shown in the border, e.g. "Cloud Run"
	lastData *dao.TableData
	filter   string

	// rowIndex maps tview row index (1-based, header=0) → dao.Row.
	// Rebuilt on every repaint. Enables SelectedRow().
	rowIndex []dao.Row
}

// NewResourceTable creates a ResourceTable with standard styling. The app
// reference is required so the embedded TableListener methods can dispatch
// repaints onto the tview main goroutine via QueueUpdateDraw.
func NewResourceTable(app *App, title string) *ResourceTable {
	t := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false). // row-selection mode
		SetFixed(1, 0)              // freeze header row

	t.SetBackgroundColor(AppTheme.BackgroundColor)
	t.SetBorder(true)
	t.SetBorderColor(AppTheme.HighlightColor)
	t.SetTitleColor(AppTheme.TableTitleColor)
	t.SetTitleAlign(tview.AlignCenter)

	return &ResourceTable{Table: t, app: app, title: title}
}

// TableDataChanged implements model.TableListener. Schedules a repaint with
// the new data on the tview main goroutine. Views that need to mutate the
// data (e.g. accumulate pages) should shadow this method on the outer type.
func (r *ResourceTable) TableDataChanged(data *dao.TableData) {
	r.app.runOnUI(func() { r.Render(data) })
}

// TableLoadFailed implements model.TableListener. Schedules an error render
// on the tview main goroutine.
func (r *ResourceTable) TableLoadFailed(err error) {
	r.app.runOnUI(func() { r.renderError(err) })
}

// renderError clears the table and shows the error message in the first cell.
// Must be called on the tview main goroutine.
func (r *ResourceTable) renderError(err error) {
	r.Clear()
	r.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf(" Error: %v ", err)).
		SetSelectable(false))
}

// Render stores data and repaints the table, applying any active filter.
// Must be called on the tview main goroutine.
func (r *ResourceTable) Render(data *dao.TableData) {
	r.lastData = data
	r.repaint()
}

// SetFilter updates the active filter string and repaints immediately.
// An empty string clears the filter and shows all rows.
// Must be called on the tview main goroutine.
func (r *ResourceTable) SetFilter(f string) {
	r.filter = f
	r.repaint()
}

// SelectedRow returns the dao.Row for the currently selected table row.
// Returns nil if nothing is selected or the table is empty.
func (r *ResourceTable) SelectedRow() dao.Row {
	row, _ := r.Table.GetSelection()
	// row 0 is the header; data rows start at 1.
	idx := row - 1
	if idx < 0 || idx >= len(r.rowIndex) {
		return nil
	}
	return r.rowIndex[idx]
}

// repaint redraws the table from lastData, applying the current filter.
func (r *ResourceTable) repaint() {
	r.Clear()
	r.rowIndex = r.rowIndex[:0]

	if r.lastData == nil {
		r.SetTitle(" " + r.title + " ")
		return
	}

	// Header row — always visible.
	for col, h := range r.lastData.Header {
		cell := tview.NewTableCell(" " + h + " ").
			SetTextColor(AppTheme.TableColumnHeaderColor).
			SetSelectable(false).
			SetExpansion(1)
		r.SetCell(0, col, cell)
	}

	needle := strings.ToLower(r.filter)
	rowIdx := 1
	for _, row := range r.lastData.Rows {
		// Filter: skip rows where no column contains the needle.
		if needle != "" && !rowMatchesFilter(row, needle) {
			continue
		}

		color := rowTypeColor(row.GetType())
		for col, c := range row.GetColumns() {
			cell := tview.NewTableCell(" " + c.Text + " ").
				SetTextColor(color).
				SetExpansion(1)
			r.SetCell(rowIdx, col, cell)
		}
		r.rowIndex = append(r.rowIndex, row)
		rowIdx++
	}

	// rowIdx-1 is the number of visible data rows (excluding header).
	titleFilter := ""
	if r.filter != "" {
		titleFilter = fmt.Sprintf(" [yellow]/%s[-]", r.filter)
	}
	r.SetTitle(fmt.Sprintf("[::b] %s [[turquoise]%d[-]]%s ", r.title, rowIdx-1, titleFilter))
}

// rowMatchesFilter returns true if any column value contains needle (case-insensitive).
func rowMatchesFilter(row dao.Row, needle string) bool {
	for _, col := range row.GetColumns() {
		if strings.Contains(strings.ToLower(col.Text), needle) {
			return true
		}
	}
	return false
}

// rowTypeColor maps a RowType to a tcell colour.
func rowTypeColor(t dao.RowType) tcell.Color {
	switch t {
	case dao.RowTypeActive:
		return AppTheme.HighlightColor
	case dao.RowTypeError:
		return tcell.ColorRed
	default:
		return tcell.ColorGray
	}
}
