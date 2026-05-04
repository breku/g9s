package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// ResourceTable is a reusable tview.Table wrapper that owns a model.Table and
// renders dao.TableData. It encapsulates the entire "show this resource as a
// scrollable table" pattern so per-resource view types only need to add their
// own key handlers, hints, and resource-specific actions.
//
// Responsibilities:
//   - Renders dao.TableData with header, row colouring, and live filtering.
//   - Implements model.TableListener (TableDataChanged / TableLoadFailed) so
//     model updates flow into the table without per-view boilerplate.
//   - Implements ResourceView (Primitive / Watch / Stop / DAO / RenderLoading)
//     so the App can drive any embedded view through a single interface.
//   - Optional cursor-based pagination: when pageSize > 0 the embedded view
//     accumulates rows across pages and a PageDown press triggers
//     dao.Paginator.NextPage. A "↓ PageDown to load more…" hint row is
//     appended when more pages are available.
//
// Concurrency: all state mutation happens on the tview main goroutine. The
// model.TableListener callbacks dispatch onto the UI via app.runOnUI; the
// pagination next-page fetch runs in its own goroutine and re-enters via
// app.runOnUI to apply results.
type ResourceTable struct {
	*tview.Table

	app          *App
	title        string       // resource label shown in the border, e.g. "Cloud Run"
	loadingLabel string       // shown by RenderLoading; "" suppresses the message entirely
	accessor     dao.Accessor // returned by DAO(); also tested for dao.Paginator when pageSize > 0
	mdl          *model.Table

	// --- pagination state (zero-valued when pageSize == 0) ---
	pageSize    int
	allRows     []dao.Row
	nextCursor  string
	lastHeader  []string
	loadingPage bool
	// paginated is true once the user has successfully loaded at least one
	// extra page via PageDown. While true, periodic poll refreshes update
	// the first page in place instead of resetting the accumulator, so the
	// user isn't snapped back to page 1 every RefreshRate.
	paginated bool

	// --- render state ---
	lastData *dao.TableData
	filter   string

	// rowIndex maps tview row index (1-based, header=0) → dao.Row.
	// Rebuilt on every repaint. Enables SelectedRow().
	rowIndex []dao.Row
}

// NewResourceTable creates a non-paginated ResourceTable. Use this for views
// that just need rendering and have no associated model (rare); most callers
// should use NewResourceView instead.
func NewResourceTable(app *App, title string) *ResourceTable {
	return newResourceTable(app, title, "", nil, nil, 0)
}

// NewResourceView builds a ResourceTable wired to a model.Table and DAO,
// returning a ready-to-embed component. The constructed table registers
// itself as the model.TableListener, so the embedding view inherits Watch /
// Stop / TableDataChanged / TableLoadFailed without writing any glue.
//
// Arguments:
//   - app, project: standard plumbing.
//   - resourceKey:  the registry key passed to model.NewTable (e.g. "cloudrun").
//   - title:        border label.
//   - loadingLabel: text shown by RenderLoading; pass "" to suppress entirely.
//   - accessor:     the typed DAO; embedded views typically also keep their
//     own typed reference for resource-specific calls.
//   - pageSize:     0 disables pagination; >0 enables cursor-based accumulation.
//     When >0, accessor MUST implement dao.Paginator.
func NewResourceView(
	app *App,
	project, resourceKey, title, loadingLabel string,
	accessor dao.Accessor,
	pageSize int,
) *ResourceTable {
	mdl := model.NewTable(resourceKey, project)
	rt := newResourceTable(app, title, loadingLabel, accessor, mdl, pageSize)
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
	pageSize int,
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
		pageSize:     pageSize,
	}

	// Wire PageDown to next-page fetch when paginated. We chain a wrapping
	// input capture so the table's own scrolling still happens.
	if pageSize > 0 {
		t.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyPgDn {
				rt.maybeLoadNextPage()
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

// TableDataChanged implements model.TableListener. In non-paginated mode it
// replaces the rendered data wholesale. In paginated mode it resets the
// accumulator to the freshly-polled first page — except when the user has
// already paged past page 1, in which case it merges the fresh first page
// into the accumulator (refreshing statuses for visible rows, prepending
// any new rows) without discarding pages the user has loaded via PageDown.
func (r *ResourceTable) TableDataChanged(data *dao.TableData) {
	r.app.runOnUI(func() {
		if r.pageSize > 0 {
			r.lastHeader = data.Header
			if r.paginated {
				r.mergeFirstPage(data.Rows)
			} else {
				r.allRows = data.Rows
				r.nextCursor = data.NextPageToken
			}
			r.renderAccumulated()
			return
		}
		r.Render(data)
	})
}

// mergeFirstPage updates the accumulator with a freshly polled first page
// while preserving rows the user has loaded by paginating. Existing rows
// whose IDs appear in freshRows are replaced in place (status updates
// flow through). Rows in freshRows whose IDs aren't yet known are
// prepended in their original order. nextCursor is left untouched because
// it already points past the pages the user has loaded; using the token
// returned with page 1 would re-fetch pages that are already on screen.
// Must be called on the tview main goroutine.
func (r *ResourceTable) mergeFirstPage(freshRows []dao.Row) {
	freshByID := make(map[string]dao.Row, len(freshRows))
	for _, row := range freshRows {
		freshByID[row.GetID()] = row
	}

	merged := make([]dao.Row, 0, len(r.allRows)+len(freshRows))
	seen := make(map[string]struct{}, len(r.allRows))

	// Prepend new-to-us rows from the fresh first page in their original order.
	knownIDs := make(map[string]struct{}, len(r.allRows))
	for _, row := range r.allRows {
		knownIDs[row.GetID()] = struct{}{}
	}
	for _, row := range freshRows {
		if _, known := knownIDs[row.GetID()]; !known {
			merged = append(merged, row)
			seen[row.GetID()] = struct{}{}
		}
	}

	// Then walk the existing accumulator, replacing with the fresh copy
	// when one exists so statuses (e.g. QUEUED → SUCCESS) update.
	for _, row := range r.allRows {
		id := row.GetID()
		if _, dup := seen[id]; dup {
			continue
		}
		if fresh, ok := freshByID[id]; ok {
			merged = append(merged, fresh)
		} else {
			merged = append(merged, row)
		}
		seen[id] = struct{}{}
	}

	r.allRows = merged
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

// maybeLoadNextPage fetches the next page in the background and appends rows.
// No-op when there is no next page, a fetch is already in flight, the
// accessor doesn't implement dao.Paginator, or pagination is disabled.
// Must be called on the tview main goroutine (e.g. from an input capture).
func (r *ResourceTable) maybeLoadNextPage() {
	if r.pageSize <= 0 || r.nextCursor == "" || r.loadingPage {
		return
	}
	pager, ok := r.accessor.(dao.Paginator)
	if !ok {
		return
	}
	r.loadingPage = true

	cursor := r.nextCursor
	pageSize := r.pageSize
	project := ""
	if r.mdl != nil {
		project = r.mdl.Project()
	}

	go func() {
		data, err := pager.NextPage(r.app.ctx, project, cursor, pageSize)
		if err != nil {
			log.Error().Err(err).Str("title", r.title).Msg("resource table: next page failed")
			r.app.runOnUI(func() { r.loadingPage = false })
			return
		}
		r.app.runOnUI(func() {
			r.loadingPage = false
			r.allRows = append(r.allRows, data.Rows...)
			r.nextCursor = data.NextPageToken
			if r.lastHeader == nil {
				r.lastHeader = data.Header
			}
			r.paginated = true
			r.renderAccumulated()
		})
	}()
}

// renderAccumulated repaints the table from the accumulated rows and appends
// a "PageDown to load more…" hint when more pages are available.
// Must be called on the tview main goroutine.
func (r *ResourceTable) renderAccumulated() {
	r.Render(&dao.TableData{
		Header:        r.lastHeader,
		Rows:          r.allRows,
		NextPageToken: r.nextCursor,
	})
	if r.nextCursor != "" && !r.loadingPage {
		rowIdx := r.Table.GetRowCount()
		r.Table.SetCell(rowIdx, 0, tview.NewTableCell(" [darkgray]↓ PageDown to load more… ").
			SetSelectable(false))
	}
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
