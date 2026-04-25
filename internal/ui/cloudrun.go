package ui

import (
	"fmt"

	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/model"
	"github.com/rivo/tview"
)

// Filterable is implemented by resource views that support row filtering.
type Filterable interface {
	SetFilter(string)
}

// CloudRunView is the tview page for Cloud Run services.
// It embeds ResourceTable (the generic tview widget) and implements
// model.TableListener so it receives model push notifications.
type CloudRunView struct {
	*ResourceTable

	app   *App
	model *model.Table
}

// Ensure interfaces are satisfied at compile time.
var (
	_ model.TableListener = (*CloudRunView)(nil)
	_ Filterable          = (*CloudRunView)(nil)
)

// NewCloudRunView creates a CloudRunView for the given project.
func NewCloudRunView(a *App, project string) *CloudRunView {
	v := &CloudRunView{
		ResourceTable: NewResourceTable(),
		app:           a,
		model:         model.NewTable("cloudrun", project),
	}
	v.model.AddListener(v)
	return v
}

// SetFilter implements Filterable. Delegates to ResourceTable.
// Must be called on the tview main goroutine.
func (v *CloudRunView) SetFilter(f string) {
	v.ResourceTable.SetFilter(f)
}

// TableDataChanged implements model.TableListener.
// Called from the model goroutine — dispatches to the tview main loop.
func (v *CloudRunView) TableDataChanged(data *dao.TableData) {
	v.app.tview.QueueUpdateDraw(func() {
		v.Render(data)
	})
}

// TableLoadFailed implements model.TableListener.
func (v *CloudRunView) TableLoadFailed(err error) {
	v.app.tview.QueueUpdateDraw(func() {
		v.renderError(err)
	})
}

// renderLoading clears the table and shows a single loading message.
// Must be called from the tview main goroutine.
func (v *CloudRunView) renderLoading() {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(" Loading Cloud Run services… ").
		SetSelectable(false))
}

// renderError clears the table and shows the error message.
func (v *CloudRunView) renderError(err error) {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf(" Error: %v ", err)).
		SetSelectable(false))
}
