package ui

import (
	"context"
	"fmt"

	"github.com/brekol/g9s/internal/dao/migs"
	"github.com/gdamore/tcell/v2"
)

// MIGsView is the tview page for Compute Engine Managed Instance Groups.
//
// All lifecycle, rendering, and Filterable/ResourceView/TableListener glue is
// inherited from the embedded *ResourceTable. Adds an 'e' binding to open a
// describe overlay listing recent per-instance errors for the selected MIG
// (equivalent to `gcloud compute instance-groups managed list-errors`).
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
		{Key: "e", Desc: "Errors"},
	}
}

// HandleKey implements KeyHandler.
func (v *MIGsView) HandleKey(event *tcell.EventKey) bool {
	if event.Rune() == 'e' {
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
