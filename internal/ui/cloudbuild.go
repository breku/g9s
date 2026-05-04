package ui

import (
	"github.com/brekol/g9s/internal/dao/cloudbuild"
	"github.com/gdamore/tcell/v2"
)

// CloudBuildView is the tview page for Cloud Build triggers.
//
// All lifecycle, rendering, and Filterable/ResourceView/TableListener glue is
// inherited from the embedded *ResourceTable.
type CloudBuildView struct {
	*ResourceTable

	app *App
	dao *cloudbuild.CloudBuild
}

// Ensure interfaces are satisfied at compile time.
var (
	_ ResourceView = (*CloudBuildView)(nil)
	_ KeyHandler   = (*CloudBuildView)(nil)
	_ HintProvider = (*CloudBuildView)(nil)
)

// NewCloudBuildView creates a CloudBuildView for the given project.
func NewCloudBuildView(a *App, project string) *CloudBuildView {
	d := new(cloudbuild.CloudBuild)
	return &CloudBuildView{
		ResourceTable: NewResourceView(a, project, "cloudbuild", "Cloud Build", "Cloud Build triggers", d),
		app:           a,
		dao:           d,
	}
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
	overlay := NewRunOverlay(v.app, v.dao, tr.Name, tr.Project, tr.TriggerID, tr.Branch)
	v.app.PushOverlay(overlay)
	return true
}
