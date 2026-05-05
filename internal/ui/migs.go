package ui

import (
	"github.com/brekol/g9s/internal/dao/migs"
)

// MIGsView is the tview page for Compute Engine Managed Instance Groups.
//
// All lifecycle, rendering, and Filterable/ResourceView/TableListener glue is
// inherited from the embedded *ResourceTable. MIGs has no resource-specific
// actions today (just generic 'y' for YAML and 'c' for copy), so this view
// is intentionally minimal.
type MIGsView struct {
	*ResourceTable

	app *App
	dao *migs.MIGs
}

// Ensure interfaces are satisfied at compile time.
var (
	_ ResourceView = (*MIGsView)(nil)
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
