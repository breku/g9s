package ui

import (
	"github.com/brekol/g9s/internal/dao"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ResourceTable is a reusable tview.Table wrapper that renders dao.TableData.
// It is embedded by resource-specific view structs (e.g. CloudRunView).
type ResourceTable struct {
	*tview.Table
}

// NewResourceTable creates a ResourceTable with standard styling.
func NewResourceTable() *ResourceTable {
	t := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false). // row-selection mode
		SetFixed(1, 0)              // freeze header row

	t.SetBackgroundColor(tcell.ColorDefault)

	return &ResourceTable{Table: t}
}

// Render populates the table from a dao.TableData snapshot.
// It must be called from within app.QueueUpdateDraw when invoked from a goroutine.
func (r *ResourceTable) Render(data *dao.TableData) {
	r.Clear()

	if data == nil {
		return
	}

	// Header row
	for col, h := range data.Header {
		cell := tview.NewTableCell(" " + h + " ").
			SetTextColor(tcell.ColorYellow).
			SetSelectable(false).
			SetExpansion(1)
		r.SetCell(0, col, cell)
	}

	// Data rows
	for rowIdx, row := range data.Rows {
		for col, val := range row.Columns {
			color := tcell.ColorWhite
			// Highlight status column (index 2 by convention) with colour.
			if col == 2 {
				color = statusColor(val)
			}
			cell := tview.NewTableCell(" " + val + " ").
				SetTextColor(color).
				SetExpansion(1)
			r.SetCell(rowIdx+1, col, cell)
		}
	}
}

// statusColor maps a status string to a tcell colour for quick visual scanning.
func statusColor(status string) tcell.Color {
	switch status {
	case "Ready":
		return tcell.ColorGreen
	case "Failed":
		return tcell.ColorRed
	case "Deploying":
		return tcell.ColorYellow
	default:
		return tcell.ColorGray
	}
}
