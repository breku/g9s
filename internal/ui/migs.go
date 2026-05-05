package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/dao/miginstances"
	"github.com/brekol/g9s/internal/dao/migs"
	"github.com/gdamore/tcell/v2"
)

// MIGsView is the tview page for Compute Engine Managed Instance Groups.
//
// All lifecycle, rendering, and Filterable/ResourceView/TableListener glue is
// inherited from the embedded *ResourceTable. Adds:
//   - 'e' opens a DescribeView listing the MIG's recent per-instance errors.
//   - 'i' opens a TableOverlay drilling into the MIG's managed instances,
//     from which 'l' opens a streaming Cloud Logging overlay for the
//     selected instance.
type MIGsView struct {
	*ResourceTable

	app *App
	dao *migs.MIGs
}

// Ensure interfaces are satisfied at compile time.
var (
	_ ResourceView = (*MIGsView)(nil)
	_ KeyHandler   = (*MIGsView)(nil)
	_ HintProvider = (*MIGsView)(nil)
)

// NewMIGsView creates a MIGsView for the given project.
func NewMIGsView(a *App, project string) *MIGsView {
	d := new(migs.MIGs)
	return &MIGsView{
		ResourceTable: NewResourceView(a, project, "migs", "Managed Instance Groups", "managed instance groups", d),
		app:           a,
		dao:           d,
	}
}

// Hints implements HintProvider. Only resource-specific bindings; generic
// y/c are advertised by the global dispatcher.
func (v *MIGsView) Hints() []Hint {
	return []Hint{
		{Key: "i", Desc: "Instances"},
		{Key: "e", Desc: "Errors"},
	}
}

// HandleKey implements KeyHandler.
func (v *MIGsView) HandleKey(event *tcell.EventKey) bool {
	switch event.Rune() {
	case 'i':
		return v.openInstances()
	case 'e':
		return v.openErrors()
	}
	return false
}

// openErrors pushes a DescribeView showing the recent per-instance errors
// reported by the MIG's controller for the selected row.
func (v *MIGsView) openErrors() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	mr, ok := row.(*migs.MIGRow)
	if !ok || mr.Name == "" {
		return true
	}
	id := mr.GetID()
	dv := NewDescribeView(v.app, fmt.Sprintf("Errors – %s", mr.Name), func(ctx context.Context) (string, error) {
		return v.dao.ListErrors(ctx, id)
	})
	dv.EnableCopy("Copy errors")
	v.app.PushOverlay(dv)
	return true
}

// openInstances pushes a TableOverlay listing the managed instances of the
// selected MIG. The overlay registers an 'l' RowAction that opens a
// streaming logs overlay for the selected instance, stacking on top.
func (v *MIGsView) openInstances() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	mr, ok := row.(*migs.MIGRow)
	if !ok || mr.Name == "" {
		return true
	}

	d := &miginstances.MIGInstances{
		Project:  mr.Project,
		Location: mr.Location,
		Name:     mr.Name,
		Scope:    mr.Scope,
	}

	// Inline-meta resource view: not in model.Registry because the DAO is
	// parameterised by the parent MIG. RefreshRate is shorter than the
	// MIGs view (10s) since the user is actively watching a single group.
	rt := NewResourceViewWithMeta(
		v.app, mr.Project, "miginstances",
		fmt.Sprintf("Instances – %s (%s)", mr.Name, mr.Location),
		"managed instances",
		d,
		10*time.Second,
	)

	to := NewTableOverlay(v.app, fmt.Sprintf("Instances – %s (%s)", mr.Name, mr.Location), rt)
	to.AddAction('l', "Logs", func(a *App, r dao.Row) {
		ir, ok := r.(*miginstances.ManagedInstanceRow)
		if !ok {
			return
		}
		openInstanceLogs(a, ir.Project, ir.NumericID, ir.Name)
	})

	v.app.PushOverlay(to)
	return true
}
