package ui

import (
	"context"
	"fmt"

	"github.com/brekol/g9s/internal/dao/buildhistory"
	"github.com/gdamore/tcell/v2"
)

// BuildHistoryView is the tview page for Cloud Build execution history.
//
// Lifecycle, rendering, model wiring, pagination (PageDown → next page),
// and Filterable/ResourceView/TableListener glue all come from the embedded
// *ResourceTable. Only resource-specific concerns live here: the typed DAO
// reference, key bindings, hints, and the cancel/log actions.
type BuildHistoryView struct {
	*ResourceTable

	app     *App
	project string
	dao     *buildhistory.BuildHistory
}

// Ensure interfaces are satisfied at compile time.
var (
	_ ResourceView = (*BuildHistoryView)(nil)
	_ KeyHandler   = (*BuildHistoryView)(nil)
	_ HintProvider = (*BuildHistoryView)(nil)
)

// NewBuildHistoryView creates a BuildHistoryView for the given project.
func NewBuildHistoryView(a *App, project string) *BuildHistoryView {
	d := new(buildhistory.BuildHistory)
	return &BuildHistoryView{
		ResourceTable: NewResourceView(a, project, "buildhistory", "Build History", "build history", d),
		app:           a,
		project:       project,
		dao:           d,
	}
}

// Hints implements HintProvider. Only resource-specific bindings; generic
// y/c/PgDn are advertised by the global dispatcher.
func (v *BuildHistoryView) Hints() []Hint {
	return []Hint{
		{Key: "l", Desc: "View logs"},
		{Key: "C", Desc: "Cancel build"},
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
	br, ok := row.(*buildhistory.BuildRow)
	if !ok || br.BuildID == "" {
		return true
	}

	// CLOUD_LOGGING_ONLY: bucket is always empty; go straight to Cloud Logging path.
	if br.LoggingMode == "CLOUD_LOGGING_ONLY" {
		v.openLogs(br.BuildID, "", br.Status, br.Project, br.LoggingMode, br.CreateTime)
		return true
	}

	if br.LogsBucket != "" {
		v.openLogs(br.BuildID, br.LogsBucket, br.Status, br.Project, br.LoggingMode, br.CreateTime)
		return true
	}

	return true
}

// cancelSelected dispatches a CancelBuild via app.TrackOp so the outcome
// surfaces on the status bar even if the user switches away. The in-memory
// row is also flipped to "<status> (Cancelling...)" optimistically so the
// table reflects the user's intent before the next poll tick lands the
// authoritative status from the API.
func (v *BuildHistoryView) cancelSelected() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	br, ok := row.(*buildhistory.BuildRow)
	if !ok || br.BuildID == "" {
		return true
	}

	// Only allow cancelling builds that are in a non-terminal state.
	switch br.Status {
	case "Working", "Queued", "Pending":
	default:
		v.app.Status(StatusInfo, fmt.Sprintf("Build %s is %s — nothing to cancel", br.BuildID, br.Status))
		return true
	}

	// Optimistic UI update: mutate the matching row and the live cell so the
	// user sees immediate feedback. The next poll tick will replace the row
	// with the real "Cancelled" status from the API.
	const statusCol = 2 // ID, TRIGGER, STATUS
	cancellingText := br.Status + " (Cancelling...)"
	br.SetStatusColumn(cancellingText)
	for i, r := range v.rowIndex {
		if other, ok := r.(*buildhistory.BuildRow); ok && other.BuildID == br.BuildID {
			cell := v.Table.GetCell(i+1, statusCol)
			if cell != nil {
				cell.SetText(" " + cancellingText + " ")
			}
			break
		}
	}

	project, buildID := br.Project, br.BuildID
	v.app.TrackOp("Cancel build "+buildID, func(ctx context.Context) error {
		return v.dao.CancelBuild(ctx, project, buildID)
	})
	return true
}

// openLogs pushes a LogView overlay via the app. Must be called on the tview main goroutine.
func (v *BuildHistoryView) openLogs(buildID, bucket, status, project, loggingMode, createTime string) {
	lv := NewLogView(v.app, buildID, bucket, status, project, loggingMode, createTime)
	v.app.PushOverlay(lv)
}
