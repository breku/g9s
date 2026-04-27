package ui

import (
	"context"
	"fmt"

	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// CloudRunView is the tview page for Cloud Run services.
type CloudRunView struct {
	*ResourceTable

	app     *App
	project string
	mdl     *model.Table
}

// Ensure interfaces are satisfied at compile time.
var (
	_ ResourceView = (*CloudRunView)(nil)
	_ KeyHandler   = (*CloudRunView)(nil)
	_ HintProvider = (*CloudRunView)(nil)
)

// NewCloudRunView creates a CloudRunView for the given project.
func NewCloudRunView(a *App, project string) *CloudRunView {
	v := &CloudRunView{
		ResourceTable: NewResourceTable("Cloud Run"),
		app:           a,
		project:       project,
		mdl:           model.NewTable("cloudrun", project),
	}
	v.mdl.AddListener(v)
	return v
}

// Primitive implements ResourceView.
func (v *CloudRunView) Primitive() tview.Primitive { return v.Table }

// Watch implements ResourceView.
func (v *CloudRunView) Watch(ctx context.Context) error { return v.mdl.Watch(ctx) }

// RenderLoading implements ResourceView.
func (v *CloudRunView) RenderLoading() {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(" Loading Cloud Run services… ").
		SetSelectable(false))
}

// SetFilter implements Filterable.
func (v *CloudRunView) SetFilter(f string) {
	v.ResourceTable.SetFilter(f)
}

// Hints implements HintProvider.
func (v *CloudRunView) Hints() []Hint {
	return []Hint{
		{Key: "d", Desc: "Describe"},
		{Key: "y", Desc: "YAML"},
	}
}

// HandleKey implements KeyHandler.
func (v *CloudRunView) HandleKey(event *tcell.EventKey) bool {
	switch event.Rune() {
	case 'd':
		return v.openDescribe(false)
	case 'y':
		return v.openDescribe(true)
	}
	return false
}

// openDescribe pushes a DescribeView overlay for the selected service.
// yaml=true renders YAML format; yaml=false renders human-readable JSON.
func (v *CloudRunView) openDescribe(asYAML bool) bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	name := row.ID
	format := "Describe"
	if asYAML {
		format = "YAML"
	}
	title := fmt.Sprintf("%s – %s", format, lastSegmentUI(name))

	var fetchFn func(ctx context.Context) (string, error)
	if asYAML {
		fetchFn = func(ctx context.Context) (string, error) {
			return dao.DescribeYAML(ctx, name)
		}
	} else {
		fetchFn = func(ctx context.Context) (string, error) {
			return dao.DescribeText(ctx, name)
		}
	}

	dv := NewDescribeView(v.app, title, fetchFn)
	v.app.PushOverlay(dv)
	return true
}

// TableDataChanged implements model.TableListener.
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

// renderError clears the table and shows the error message.
func (v *CloudRunView) renderError(err error) {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf(" Error: %v ", err)).
		SetSelectable(false))
}

// lastSegmentUI extracts the last path segment of a resource name for display.
func lastSegmentUI(name string) string {
	parts := splitSlash(name)
	if len(parts) == 0 {
		return name
	}
	return parts[len(parts)-1]
}

func splitSlash(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}
