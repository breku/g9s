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

// Ensure interface is satisfied at compile time.
var _ ResourceView = (*BuildHistoryView)(nil)

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
