package ui

import (
	"context"

	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Hint is a single key binding description shown in the header hint bar.
type Hint struct {
	Key  string // e.g. "l", "PgDn", ":"
	Desc string // e.g. "View logs", "Next page"
}

// HintProvider is an optional interface for resource views and overlays that
// want to advertise their key bindings in the header hint bar.
// Global hints (q, :, /) are always shown by the app; views return only their
// own additions.
type HintProvider interface {
	Hints() []Hint
}

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

// Overlay is implemented by full-screen panels that sit on top of the current
// resource view (e.g. log viewer, detail panels). App.PushOverlay /
// App.PopOverlay manage their lifecycle so individual views don't need to
// manipulate pages directly.
type Overlay interface {
	// Primitive returns the tview primitive to display.
	Primitive() tview.Primitive
	// RenderLoading shows a placeholder while the first fetch is in flight.
	// Called by App.PushOverlay on the main goroutine before Start.
	RenderLoading()
	// Start begins any background work (fetching, streaming).
	// Called in a new goroutine by App.PushOverlay; blocks until the overlay
	// is closed or the context is cancelled.
	Start(ctx context.Context)
	// OnClose registers a callback that the overlay calls when it wants to
	// dismiss itself (e.g. user presses Escape). App.PushOverlay sets this.
	OnClose(func())
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
