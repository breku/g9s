package ui

import (
	"context"
	"fmt"

	"github.com/brekol/g9s/internal/dao/secrets"
	"github.com/gdamore/tcell/v2"
)

// SecretsView is the tview page for GCP Secret Manager secrets.
//
// All lifecycle, rendering, and Filterable/ResourceView/TableListener glue is
// inherited from the embedded *ResourceTable.
type SecretsView struct {
	*ResourceTable

	app *App
	dao *secrets.Secrets
}

var (
	_ ResourceView = (*SecretsView)(nil)
	_ KeyHandler   = (*SecretsView)(nil)
	_ HintProvider = (*SecretsView)(nil)
)

// NewSecretsView creates a SecretsView for the given project.
func NewSecretsView(a *App, project string) *SecretsView {
	d := new(secrets.Secrets)
	return &SecretsView{
		ResourceTable: NewResourceView(a, project, "secrets", "Secrets", "secrets", d),
		app:           a,
		dao:           d,
	}
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
	name := row.GetID()
	short := lastSegmentUI(name)
	if sr, ok := row.(*secrets.SecretRow); ok && sr.Name != "" {
		short = sr.Name
	}
	title := fmt.Sprintf("Secret – %s", short)

	dv := NewDescribeView(v.app, title, func(ctx context.Context) (string, error) {
		return v.dao.AccessLatestSecret(ctx, name)
	})
	dv.EnableCopy("Copy value")
	v.app.PushOverlay(dv)
	return true
}
