package ui

import (
	"context"

	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Filterable is implemented by resource views that support row filtering.
type Filterable interface {
	SetFilter(string)
}

// KeyHandler is an optional interface for resource views that handle their
// own key bindings (e.g. 'l' for logs in BuildHistoryView).
// HandleKey returns true if the event was consumed, false to let the app
// handle it.
type KeyHandler interface {
	HandleKey(event *tcell.EventKey) bool
}

// ResourceView is the common interface for all resource views. It unifies
// the operations app.go needs so that routing is generic (no per-resource
// show methods).
type ResourceView interface {
	Filterable
	model.TableListener

	// Primitive returns the tview primitive to mount in the pages layout.
	Primitive() tview.Primitive
	// Watch starts the background polling loop. Blocks until ctx is cancelled.
	Watch(ctx context.Context) error
	// RenderLoading shows a placeholder while the first fetch is in flight.
	RenderLoading()
}

// newResourceView is a factory that creates the correct ResourceView for a
// given registry key. Returns nil if the key is unknown.
func newResourceView(a *App, resource, project string) ResourceView {
	switch resource {
	case "cloudrun":
		return NewCloudRunView(a, project)
	case "cloudbuild":
		return NewCloudBuildView(a, project)
	case "buildhistory":
		return NewBuildHistoryView(a, project)
	default:
		return nil
	}
}
