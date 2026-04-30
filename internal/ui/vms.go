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
	dao *vms.VMs
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
		ResourceTable: NewResourceTable(a, "VMs"),
		app:           a,
		mdl:           model.NewTable("vms", project),
		dao:           new(vms.VMs),
	}
	v.mdl.AddListener(v)
	return v
}

// Primitive implements ResourceView.
func (v *VMsView) Primitive() tview.Primitive { return v.Table }

// Watch implements ResourceView.
func (v *VMsView) Watch(ctx context.Context) error { return v.mdl.Watch(ctx) }

// Stop implements ResourceView.
func (v *VMsView) Stop() { v.mdl.Stop() }

// DAO implements ResourceView.
func (v *VMsView) DAO() dao.Accessor { return v.dao }

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

// Hints implements HintProvider. Only resource-specific bindings; generic
// d/y/c are advertised by the global dispatcher.
func (v *VMsView) Hints() []Hint {
	return []Hint{
		{Key: "l", Desc: "Logs"},
		{Key: "Ctrl-D", Desc: "Delete"},
	}
}

// HandleKey implements KeyHandler.
func (v *VMsView) HandleKey(event *tcell.EventKey) bool {
	if event.Key() == tcell.KeyCtrlD {
		return v.confirmDelete()
	}
	if event.Rune() == 'l' {
		return v.openLogs()
	}
	return false
}

// confirmDelete pushes a ConfirmOverlay; on Enter it dispatches the delete
// via app.TrackOp so the user sees "Delete foo… (running)" / "succeeded" /
// "failed: …" on the status bar even after switching away from this view.
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
	project, zone, name := ir.Project, ir.Zone, ir.Name
	co := NewConfirmOverlay(v.app, title, prompt, func() {
		v.app.TrackOp("Delete VM "+name, func(ctx context.Context) error {
			return v.dao.Delete(ctx, project, zone, name)
		})
	})
	v.app.PushOverlay(co)
	return true
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

// TableDataChanged / TableLoadFailed are inherited from the embedded
// *ResourceTable, which schedules repaints on the tview main goroutine.
