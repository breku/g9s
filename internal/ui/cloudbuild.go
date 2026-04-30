package ui

import (
	"context"
	"fmt"

	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/dao/cloudbuild"
	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// CloudBuildView is the tview page for Cloud Build triggers.
type CloudBuildView struct {
	*ResourceTable

	app *App
	mdl *model.Table
}

// Ensure interfaces are satisfied at compile time.
var (
	_ ResourceView = (*CloudBuildView)(nil)
	_ KeyHandler   = (*CloudBuildView)(nil)
	_ HintProvider = (*CloudBuildView)(nil)
)

// NewCloudBuildView creates a CloudBuildView for the given project.
func NewCloudBuildView(a *App, project string) *CloudBuildView {
	v := &CloudBuildView{
		ResourceTable: NewResourceTable("Cloud Build"),
		app:           a,
		mdl:           model.NewTable("cloudbuild", project),
	}
	v.mdl.AddListener(v)
	return v
}

// Primitive implements ResourceView.
func (v *CloudBuildView) Primitive() tview.Primitive { return v.Table }

// Watch implements ResourceView.
func (v *CloudBuildView) Watch(ctx context.Context) error { return v.mdl.Watch(ctx) }

// RenderLoading implements ResourceView.
func (v *CloudBuildView) RenderLoading() {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(" Loading Cloud Build triggers… ").
		SetSelectable(false))
}

// SetFilter implements Filterable.
func (v *CloudBuildView) SetFilter(f string) {
	v.ResourceTable.SetFilter(f)
}

// Hints implements HintProvider.
func (v *CloudBuildView) Hints() []Hint {
	return []Hint{
		{Key: "t", Desc: "trigger build"},
	}
}

// HandleKey implements KeyHandler.
func (v *CloudBuildView) HandleKey(event *tcell.EventKey) bool {
	if event.Rune() == 't' {
		return v.openRunOverlay()
	}
	return false
}

// openRunOverlay pushes a RunOverlay for the selected trigger.
func (v *CloudBuildView) openRunOverlay() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	tr, ok := row.(*cloudbuild.TriggerRow)
	if !ok || tr.TriggerID == "" {
		return true
	}
	overlay := NewRunOverlay(v.app, tr.Name, tr.Project, tr.TriggerID, tr.Branch)
	v.app.PushOverlay(overlay)
	return true
}

// TableDataChanged implements model.TableListener.
func (v *CloudBuildView) TableDataChanged(data *dao.TableData) {
	v.app.tview.QueueUpdateDraw(func() {
		v.Render(data)
	})
}

// TableLoadFailed implements model.TableListener.
func (v *CloudBuildView) TableLoadFailed(err error) {
	v.app.tview.QueueUpdateDraw(func() {
		v.renderError(err)
	})
}

// renderError clears the table and shows the error message.
func (v *CloudBuildView) renderError(err error) {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf(" Error: %v ", err)).
		SetSelectable(false))
}
