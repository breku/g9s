package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ResourceTable is a reusable tview.Table wrapper that owns a model.Table
// and renders dao.TableData. It encapsulates the entire "show this resource
// as a scrollable table" pattern so per-resource view types only need to
// add their own key handlers, hints, and resource-specific actions.
//
// Responsibilities:
//   - Renders dao.TableData with header, row colouring, and live filtering.
//   - Implements model.TableListener (TableDataChanged / TableLoadFailed) so
//     model updates flow into the table without per-view boilerplate.
//   - Implements ResourceView (Primitive / Watch / Stop / DAO / RenderLoading)
//     so the App can drive any embedded view through a single interface.
//   - Forwards PgDn key presses to model.Table.LoadNextPage. Pagination
//     state (page size, cursor, accumulated rows, merge-on-refresh) lives
//     entirely in the model layer; the UI just renders whatever
//     TableData arrives via TableDataChanged and shows a hint footer when
//     more pages are available.
//
// Concurrency: all state mutation happens on the tview main goroutine. The
// model.TableListener callbacks dispatch onto the UI via app.runOnUI.
type ResourceTable struct {
	*tview.Table

	app          *App
	title        string       // resource label shown in the border, e.g. "Cloud Run"
	loadingLabel string       // shown by RenderLoading; "" suppresses the message entirely
	accessor     dao.Accessor // returned by DAO()
	mdl          *model.Table

	// --- render state ---
	lastData *dao.TableData
	filter   string

	// rowIndex maps tview row index (1-based, header=0) → dao.Row.
	// Rebuilt on every repaint. Enables SelectedRow().
	rowIndex []dao.Row
}

// NewResourceTable creates a ResourceTable with no model attached. Used for
// views that just need rendering and have no associated model (rare); most
// callers should use NewResourceView instead.
func NewResourceTable(app *App, title string) *ResourceTable {
	return newResourceTable(app, title, "", nil, nil)
}

// NewResourceView builds a ResourceTable wired to a model.Table and DAO,
// returning a ready-to-embed component. The constructed table registers
// itself as the model.TableListener, so the embedding view inherits Watch /
// Stop / TableDataChanged / TableLoadFailed without writing any glue.
//
// Pagination is on for every resource: PgDn calls model.Table.LoadNextPage,
// which uses the per-resource page size from model.Registry (or
// model.DefaultPageSize when unset). Resources whose underlying API
// returns everything in one shot will simply have an empty NextPageToken
// and no further pages to load.
func NewResourceView(
	app *App,
	project, resourceKey, title, loadingLabel string,
	accessor dao.Accessor,
) *ResourceTable {
	mdl := model.NewTable(resourceKey, project)
	rt := newResourceTable(app, title, loadingLabel, accessor, mdl)
	mdl.AddListener(rt)
	return rt
}

// newResourceTable is the shared constructor used by both NewResourceTable
// and NewResourceView.
func newResourceTable(
	app *App,
	title, loadingLabel string,
	accessor dao.Accessor,
	mdl *model.Table,
) *ResourceTable {
	t := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false). // row-selection mode
		SetFixed(1, 0)              // freeze header row

	t.SetBackgroundColor(AppTheme.BackgroundColor)
	t.SetBorder(true)
	t.SetBorderColor(AppTheme.HighlightColor)
	t.SetTitleColor(AppTheme.TableTitleColor)
	t.SetTitleAlign(tview.AlignCenter)

	rt := &ResourceTable{
		Table:        t,
		app:          app,
		title:        title,
		loadingLabel: loadingLabel,
		accessor:     accessor,
		mdl:          mdl,
	}

	// Wire PgDn to next-page fetch when a model is attached. We chain a
	// wrapping input capture so the table's own scrolling still happens.
	if mdl != nil {
		t.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyPgDn {
				mdl.LoadNextPage()
			}
			return event
		})
	}

	return rt
}

// Primitive implements ResourceView.
func (r *ResourceTable) Primitive() tview.Primitive { return r.Table }

// Watch implements ResourceView. No-op when this ResourceTable has no model
// (i.e. constructed via NewResourceTable rather than NewResourceView).
func (r *ResourceTable) Watch(ctx context.Context) error {
	if r.mdl == nil {
		return nil
	}
	return r.mdl.Watch(ctx)
}

// Stop implements ResourceView. No-op when this ResourceTable has no model.
func (r *ResourceTable) Stop() {
	if r.mdl != nil {
		r.mdl.Stop()
	}
}

// DAO implements ResourceView.
func (r *ResourceTable) DAO() dao.Accessor { return r.accessor }

// RenderLoading implements ResourceView. Shows " Loading <loadingLabel>… "
// in the first cell when loadingLabel is set; otherwise just clears the table.
func (r *ResourceTable) RenderLoading() {
	r.Clear()
	if r.loadingLabel == "" {
		return
	}
	r.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf(" Loading %s… ", r.loadingLabel)).
		SetSelectable(false))
}

// TableDataChanged implements model.TableListener. The model layer owns
// pagination accumulation/merging, so this just renders whatever it gets
// and (when more pages are available) appends a footer hint row.
func (r *ResourceTable) TableDataChanged(data *dao.TableData) {
	r.app.runOnUI(func() {
		r.Render(data)
		if data.NextPageToken != "" {
			rowIdx := r.Table.GetRowCount()
			r.Table.SetCell(rowIdx, 0, tview.NewTableCell(" [darkgray]↓ PageDown to load more… ").
				SetSelectable(false))
		}
	})
}

// TableLoadFailed implements model.TableListener.
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

// Filter returns the active filter string ("" if none). Used by the App's
// Esc handler to decide whether Esc should clear the filter.
func (r *ResourceTable) Filter() string { return r.filter }

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
