package ui

import (
	"context"
	"fmt"

	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/dao/secrets"
	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// SecretsView is the tview page for GCP Secret Manager secrets.
type SecretsView struct {
	*ResourceTable

	app *App
	mdl *model.Table
}

var (
	_ ResourceView = (*SecretsView)(nil)
	_ KeyHandler   = (*SecretsView)(nil)
	_ HintProvider = (*SecretsView)(nil)
)

// NewSecretsView creates a SecretsView for the given project.
func NewSecretsView(a *App, project string) *SecretsView {
	v := &SecretsView{
		ResourceTable: NewResourceTable("Secrets"),
		app:           a,
		mdl:           model.NewTable("secrets", project),
	}
	v.mdl.AddListener(v)
	return v
}

// Primitive implements ResourceView.
func (v *SecretsView) Primitive() tview.Primitive { return v.Table }

// Watch implements ResourceView.
func (v *SecretsView) Watch(ctx context.Context) error { return v.mdl.Watch(ctx) }

// RenderLoading implements ResourceView.
func (v *SecretsView) RenderLoading() {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(" Loading secrets… ").
		SetSelectable(false))
}

// SetFilter implements Filterable.
func (v *SecretsView) SetFilter(f string) {
	v.ResourceTable.SetFilter(f)
}

// Hints implements HintProvider.
func (v *SecretsView) Hints() []Hint {
	return []Hint{
		{Key: "v", Desc: "View value"},
	}
}

// HandleKey implements KeyHandler.
func (v *SecretsView) HandleKey(event *tcell.EventKey) bool {
	if event.Rune() == 'v' {
		return v.viewSecret()
	}
	return false
}

// viewSecret pushes a DescribeView overlay showing the latest secret payload.
func (v *SecretsView) viewSecret() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	name := row.ID
	short := row.Meta["name"]
	if short == "" {
		short = lastSegmentUI(name)
	}
	title := fmt.Sprintf("Secret – %s", short)

	dv := NewDescribeView(v.app, title, func(ctx context.Context) (string, error) {
		return secrets.AccessLatestSecret(ctx, name)
	})
	dv.EnableCopy("Copy value")
	v.app.PushOverlay(dv)
	return true
}

// TableDataChanged implements model.TableListener.
func (v *SecretsView) TableDataChanged(data *dao.TableData) {
	v.app.tview.QueueUpdateDraw(func() {
		v.Render(data)
	})
}

// TableLoadFailed implements model.TableListener.
func (v *SecretsView) TableLoadFailed(err error) {
	v.app.tview.QueueUpdateDraw(func() {
		v.renderError(err)
	})
}

// renderError clears the table and shows the error message.
func (v *SecretsView) renderError(err error) {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf(" Error: %v ", err)).
		SetSelectable(false))
}
