package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/dao/vms"
	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// VMsView is the tview page for Compute Engine VM instances.
type VMsView struct {
	*ResourceTable

	app *App
	mdl *model.Table
}

// Ensure interfaces are satisfied at compile time.
var (
	_ ResourceView = (*VMsView)(nil)
	_ KeyHandler   = (*VMsView)(nil)
	_ HintProvider = (*VMsView)(nil)
)

// NewVMsView creates a VMsView for the given project.
func NewVMsView(a *App, project string) *VMsView {
	v := &VMsView{
		ResourceTable: NewResourceTable("VMs"),
		app:           a,
		mdl:           model.NewTable("vms", project),
	}
	v.mdl.AddListener(v)
	return v
}

// Primitive implements ResourceView.
func (v *VMsView) Primitive() tview.Primitive { return v.Table }

// Watch implements ResourceView.
func (v *VMsView) Watch(ctx context.Context) error { return v.mdl.Watch(ctx) }

// RenderLoading implements ResourceView.
func (v *VMsView) RenderLoading() {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(" Loading VM instances… ").
		SetSelectable(false))
}

// SetFilter implements Filterable.
func (v *VMsView) SetFilter(f string) {
	v.ResourceTable.SetFilter(f)
}

// Hints implements HintProvider.
func (v *VMsView) Hints() []Hint {
	return []Hint{
		{Key: "d", Desc: "Describe"},
		{Key: "y", Desc: "YAML"},
		{Key: "l", Desc: "Logs"},
		{Key: "Ctrl-D", Desc: "Delete"},
	}
}

// HandleKey implements KeyHandler.
func (v *VMsView) HandleKey(event *tcell.EventKey) bool {
	if event.Key() == tcell.KeyCtrlD {
		return v.confirmDelete()
	}
	switch event.Rune() {
	case 'd':
		return v.openDescribe(false)
	case 'y':
		return v.openDescribe(true)
	case 'l':
		return v.openLogs()
	}
	return false
}

// confirmDelete pushes a ConfirmOverlay; on 'y' it calls vms.DeleteVM.
func (v *VMsView) confirmDelete() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	ir, ok := row.(*vms.InstanceRow)
	if !ok || ir.Project == "" || ir.Zone == "" || ir.Name == "" {
		return true
	}

	prompt := fmt.Sprintf("Delete instance [yellow]%s[white] in zone [yellow]%s[white]?", ir.Name, ir.Zone)
	title := fmt.Sprintf("VM – %s", ir.Name)
	co := NewConfirmOverlay(v.app, title, prompt, func(ctx context.Context) error {
		return vms.DeleteVM(ctx, ir.Project, ir.Zone, ir.Name)
	})
	v.app.PushOverlay(co)
	return true
}

// openDescribe pushes a DescribeView overlay for the selected instance.
// yaml=true renders YAML; otherwise pretty-printed JSON.
func (v *VMsView) openDescribe(asYAML bool) bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	ir, ok := row.(*vms.InstanceRow)
	if !ok || ir.Project == "" || ir.Zone == "" || ir.Name == "" {
		return true
	}

	format := "Describe"
	if asYAML {
		format = "YAML"
	}
	title := fmt.Sprintf("%s – %s", format, ir.Name)

	var fetchFn func(ctx context.Context) (string, error)
	if asYAML {
		fetchFn = func(ctx context.Context) (string, error) {
			return vms.DescribeVMYAML(ctx, ir.Project, ir.Zone, ir.Name)
		}
	} else {
		fetchFn = func(ctx context.Context) (string, error) {
			return vms.DescribeVMText(ctx, ir.Project, ir.Zone, ir.Name)
		}
	}

	dv := NewDescribeView(v.app, title, fetchFn)
	v.app.PushOverlay(dv)
	return true
}

// TableDataChanged implements model.TableListener.
func (v *VMsView) TableDataChanged(data *dao.TableData) {
	v.app.tview.QueueUpdateDraw(func() {
		v.Render(data)
	})
}

// TableLoadFailed implements model.TableListener.
func (v *VMsView) TableLoadFailed(err error) {
	v.app.tview.QueueUpdateDraw(func() {
		v.renderError(err)
	})
}

// renderError clears the table and shows the error message.
func (v *VMsView) renderError(err error) {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf(" Error: %v ", err)).
		SetSelectable(false))
}

// openLogs pushes a LogView overlay streaming Cloud Logging entries for the
// selected VM, scoped by numeric instance_id (precise, zone-independent).
func (v *VMsView) openLogs() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	ir, ok := row.(*vms.InstanceRow)
	if !ok || ir.Project == "" || ir.NumericID == "" {
		return true
	}

	filter := fmt.Sprintf(`resource.type="gce_instance" AND resource.labels.instance_id="%s"`, ir.NumericID)

	cfg := LogViewConfig{
		Title:       fmt.Sprintf("Logs – %s", ir.Name),
		Streaming:   true,
		Project:     ir.Project,
		LogFilter:   filter,
		LogSince:    time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
		LogPageSize: 200,
	}
	lv := NewLogViewFromConfig(v.app, cfg)
	v.app.PushOverlay(lv)
	return true
}
