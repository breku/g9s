package ui

import (
	"context"
	"fmt"

	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// BuildHistoryView is the tview page for Cloud Build execution history.
// It starts with the 10 most recent builds and appends the next page on
// PageDown when a next-page token is available.
type BuildHistoryView struct {
	*ResourceTable

	app     *App
	mdl     *model.Table
	project string

	// accumulated state — updated on every TableDataChanged + every page load.
	allRows       []dao.Row
	nextPageToken string
	loading       bool // true while a background page fetch is in flight
}

// Ensure interfaces are satisfied at compile time.
var (
	_ ResourceView = (*BuildHistoryView)(nil)
	_ KeyHandler   = (*BuildHistoryView)(nil)
	_ HintProvider = (*BuildHistoryView)(nil)
)

// NewBuildHistoryView creates a BuildHistoryView for the given project.
func NewBuildHistoryView(a *App, project string) *BuildHistoryView {
	v := &BuildHistoryView{
		ResourceTable: NewResourceTable("Build History"),
		app:           a,
		project:       project,
		mdl:           model.NewTable("buildhistory", project),
	}
	v.mdl.AddListener(v)

	// Intercept PageDown to load the next page when one is available.
	v.Table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyPgDn {
			v.maybeLoadNextPage()
			// still return the event so the table scrolls normally
		}
		return event
	})

	return v
}

// Primitive implements ResourceView.
func (v *BuildHistoryView) Primitive() tview.Primitive { return v.Table }

// Watch implements ResourceView.
func (v *BuildHistoryView) Watch(ctx context.Context) error { return v.mdl.Watch(ctx) }

// RenderLoading implements ResourceView.
func (v *BuildHistoryView) RenderLoading() {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(" Loading build history… ").
		SetSelectable(false))
}

// SetFilter implements Filterable.
func (v *BuildHistoryView) SetFilter(f string) {
	v.ResourceTable.SetFilter(f)
}

// Hints implements HintProvider.
func (v *BuildHistoryView) Hints() []Hint {
	return []Hint{
		{Key: "l", Desc: "View logs"},
		{Key: "C", Desc: "Cancel build"},
		{Key: "PgDn", Desc: "Next page"},
	}
}

// HandleKey implements KeyHandler.
// 'l' opens the log viewer for the selected build.
// 'C' cancels the selected in-progress build.
func (v *BuildHistoryView) HandleKey(event *tcell.EventKey) bool {
	if event.Rune() == 'C' {
		return v.cancelSelected()
	}
	if event.Rune() != 'l' {
		return false
	}
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	buildID := row.Meta["buildId"]
	bucket := row.Meta["logsBucket"]
	status := row.Meta["status"]
	project := row.Meta["project"]
	loggingMode := row.Meta["loggingMode"]
	createTime := row.Meta["createTime"]
	if buildID == "" {
		return true
	}

	// CLOUD_LOGGING_ONLY: bucket is always empty; go straight to Cloud Logging path.
	if loggingMode == "CLOUD_LOGGING_ONLY" {
		v.openLogs(buildID, "", status, project, loggingMode, createTime)
		return true
	}

	if bucket != "" {
		v.openLogs(buildID, bucket, status, project, loggingMode, createTime)
		return true
	}

	// Bucket not populated yet — call GetBuild to resolve it.
	go func() {
		b, err := dao.GetBuild(v.app.ctx, project, buildID)
		if err != nil {
			log.Error().Err(err).Str("buildId", buildID).Msg("build history: GetBuild failed")
			return
		}
		resolvedMode := b.Options.GetLogging().String()
		resolvedBucket := dao.LogsBucketForBuild(b)
		resolvedCreate := createTime
		if b.CreateTime != nil {
			resolvedCreate = b.CreateTime.AsTime().UTC().Format("2006-01-02T15:04:05Z")
		}
		if resolvedMode != "CLOUD_LOGGING_ONLY" && resolvedBucket == "" {
			log.Warn().Str("buildId", buildID).Msg("build history: cannot determine log bucket")
			return
		}
		v.app.tview.QueueUpdateDraw(func() {
			v.openLogs(buildID, resolvedBucket, status, project, resolvedMode, resolvedCreate)
		})
	}()
	return true
}

// cancelSelected sends a CancelBuild request for the currently selected build
// and immediately updates the in-memory row status to "<status> (Cancelling...)"
// so the user gets visual feedback before the next poll tick refreshes the row.
func (v *BuildHistoryView) cancelSelected() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	buildID := row.Meta["buildId"]
	project := row.Meta["project"]
	status := row.Meta["status"]
	if buildID == "" {
		return true
	}

	// Only allow cancelling builds that are in a non-terminal state.
	switch status {
	case "Working", "Queued", "Pending":
	default:
		log.Debug().Str("buildId", buildID).Str("status", status).
			Msg("build history: cancel ignored — build not in a cancellable state")
		return true
	}

	// Optimistic UI update: mutate the matching row and the live cell so the
	// user sees immediate feedback. The next poll tick will replace the row
	// with the real "Cancelled" status from the API.
	const statusCol = 2 // ID, TRIGGER, STATUS
	cancellingText := status + " (Cancelling...)"
	for i := range v.allRows {
		if v.allRows[i].Meta["buildId"] == buildID {
			if statusCol < len(v.allRows[i].Columns) {
				v.allRows[i].Columns[statusCol].Text = cancellingText
			}
			break
		}
	}
	for i, r := range v.rowIndex {
		if r.Meta["buildId"] == buildID {
			cell := v.Table.GetCell(i+1, statusCol)
			if cell != nil {
				cell.SetText(" " + cancellingText + " ")
			}
			break
		}
	}

	go func() {
		if err := dao.CancelBuild(v.app.ctx, project, buildID); err != nil {
			log.Error().Err(err).Str("buildId", buildID).Msg("build history: cancel failed")
			return
		}
		log.Info().Str("buildId", buildID).Msg("build history: cancel requested")
	}()
	return true
}

// openLogs pushes a LogView overlay via the app. Must be called on the tview main goroutine.
func (v *BuildHistoryView) openLogs(buildID, bucket, status, project, loggingMode, createTime string) {
	lv := NewLogView(v.app, buildID, bucket, status, project, loggingMode, createTime)
	v.app.PushOverlay(lv)
}

// TableDataChanged implements model.TableListener.
// Called on a poll tick — replaces accumulated rows with the fresh first page.
func (v *BuildHistoryView) TableDataChanged(data *dao.TableData) {
	v.app.tview.QueueUpdateDraw(func() {
		v.allRows = data.Rows
		v.nextPageToken = data.NextPageToken
		v.renderAccumulated(data.Header)
	})
}

// TableLoadFailed implements model.TableListener.
func (v *BuildHistoryView) TableLoadFailed(err error) {
	v.app.tview.QueueUpdateDraw(func() {
		v.renderError(err)
	})
}

// maybeLoadNextPage fetches the next page in the background and appends rows.
// No-op if there is no next page or a fetch is already in flight.
// Must be called on the tview main goroutine (input capture handler).
func (v *BuildHistoryView) maybeLoadNextPage() {
	if v.nextPageToken == "" || v.loading {
		return
	}
	v.loading = true

	token := v.nextPageToken
	project := v.project

	go func() {
		dao := new(dao.BuildHistory)
		data, err := dao.NextPage(v.app.ctx, project, token, 10)
		if err != nil {
			log.Error().Err(err).Msg("build history: next page failed")
			v.app.tview.QueueUpdateDraw(func() { v.loading = false })
			return
		}
		v.app.tview.QueueUpdateDraw(func() {
			v.loading = false
			v.allRows = append(v.allRows, data.Rows...)
			v.nextPageToken = data.NextPageToken
			v.renderAccumulated(data.Header)
		})
	}()
}

// renderAccumulated renders all accumulated rows into the table.
// Must be called on the tview main goroutine.
func (v *BuildHistoryView) renderAccumulated(header []string) {
	accumulated := &dao.TableData{
		Header:        header,
		Rows:          v.allRows,
		NextPageToken: v.nextPageToken,
	}
	v.Render(accumulated)

	// If there are more pages, append a hint row.
	if v.nextPageToken != "" && !v.loading {
		rowIdx := v.Table.GetRowCount()
		v.Table.SetCell(rowIdx, 0, tview.NewTableCell(" [darkgray]↓ PageDown to load more… ").
			SetSelectable(false))
	}
}

// renderError clears the table and shows the error message.
func (v *BuildHistoryView) renderError(err error) {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf(" Error: %v ", err)).
		SetSelectable(false))
}
