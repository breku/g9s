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
type ResourceTable struct {
	*tview.Table

	title    string // resource label shown in the border, e.g. "Cloud Run"
	lastData *dao.TableData
	filter   string

	// rowIndex maps tview row index (1-based, header=0) → dao.Row.
	// Rebuilt on every repaint. Enables SelectedRow().
	rowIndex []dao.Row
}

// NewResourceTable creates a ResourceTable with standard styling.
func NewResourceTable(title string) *ResourceTable {
	t := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false). // row-selection mode
		SetFixed(1, 0)              // freeze header row

	t.SetBackgroundColor(AppTheme.BackgroundColor)
	t.SetBorder(true)
	t.SetBorderColor(AppTheme.BorderColor)
	t.SetTitleColor(AppTheme.TableTitleColor)
	t.SetTitleAlign(tview.AlignCenter)

	return &ResourceTable{Table: t, title: title}
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
func (r *ResourceTable) SelectedRow() *dao.Row {
	row, _ := r.Table.GetSelection()
	// row 0 is the header; data rows start at 1.
	idx := row - 1
	if idx < 0 || idx >= len(r.rowIndex) {
		return nil
	}
	return &r.rowIndex[idx]
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

		for col, val := range row.Columns {
			color := tcell.ColorGray
			if col == 2 {
				color = statusColor(val)
			}
			cell := tview.NewTableCell(" " + val + " ").
				SetTextColor(color).
				SetExpansion(1)
			r.SetCell(rowIdx, col, cell)
		}
		r.rowIndex = append(r.rowIndex, row)
		rowIdx++
	}

	// rowIdx-1 is the number of visible data rows (excluding header).
	r.SetTitle(fmt.Sprintf("[::b] %s [[turquoise]%d[-]] ", r.title, rowIdx-1))
}

// rowMatchesFilter returns true if any column value contains needle (case-insensitive).
func rowMatchesFilter(row dao.Row, needle string) bool {
	for _, val := range row.Columns {
		if strings.Contains(strings.ToLower(val), needle) {
			return true
		}
	}
	return false
}

// statusColor maps a status string to a tcell colour for quick visual scanning.
func statusColor(status string) tcell.Color {
	switch status {
	case "Ready", "Success":
		return tcell.ColorGreen
	case "Failed", "Failure", "Internal Error", "Expired":
		return tcell.ColorRed
	case "Deploying", "Working", "Queued", "Pending":
		return tcell.ColorYellow
	case "Cancelled", "Timeout":
		return tcell.ColorOrange
	default:
		return tcell.ColorGray
	}
}
