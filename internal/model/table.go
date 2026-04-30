package model

import (
	"context"
	"sync"
	"time"

	"github.com/brekol/g9s/internal/dao"
	"github.com/rs/zerolog/log"
)

// Table is the universal polling model for any GCP resource that can be
// displayed as a table.
//
// Each view owns its Table and drives its lifecycle:
//   - Watch fetches once synchronously, then ticks at the resource's
//     RefreshRate, calling the DAO directly each time.
//   - Stop cancels the polling goroutine. The App calls this when the user
//     navigates away so background fetches don't accumulate.
//   - Watch is re-entrant: a second call cancels the previous polling
//     goroutine and starts a new one bound to the new ctx. This lets the
//     App resume polling on switch-back with a fresh context.
//
// There is no cache: switching back to a view always re-fetches. The
// trade-off is a brief "Loading…" frame on every switch-back in exchange
// for guaranteed-fresh data and zero cache-coherence machinery.
//
// All TableListener callbacks are invoked from a background goroutine; views
// must dispatch tview mutations via app.QueueUpdateDraw.
type Table struct {
	resource string
	project  string

	mu        sync.Mutex
	listeners []TableListener
	cancel    context.CancelFunc // cancels the active polling goroutine; nil when stopped
}

// NewTable creates a Table model for the given resource and project.
func NewTable(resource, project string) *Table {
	return &Table{
		resource: resource,
		project:  project,
	}
}

// AddListener registers a TableListener to receive model events.
func (t *Table) AddListener(l TableListener) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.listeners = append(t.listeners, l)
}

// Project returns the GCP project this table is bound to. Used by callers
// that need to invoke project-scoped DAO methods (e.g. Paginator.NextPage)
// without separately tracking the project.
func (t *Table) Project() string { return t.project }

// Watch performs an immediate refresh and then polls on the resource's
// configured RefreshRate until the derived context is cancelled.
//
// Re-entrant: if a previous Watch is still running, it is cancelled first.
// Returns an error only if the initial fetch fails; subsequent tick failures
// are delivered to listeners via TableLoadFailed.
func (t *Table) Watch(ctx context.Context) error {
	meta, ok := Lookup(t.resource)
	if !ok {
		return nil
	}

	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	subCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	t.mu.Unlock()

	if err := t.refresh(subCtx, meta); err != nil {
		t.fireLoadFailed(err)
		return err
	}
	go t.updater(subCtx, meta)
	return nil
}

// Stop cancels the active polling goroutine, if any. Safe to call multiple
// times. After Stop, Watch may be called again to resume polling.
func (t *Table) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
}

// updater runs the polling loop in a background goroutine, ticking at the
// resource's RefreshRate. Exits cleanly when ctx is cancelled.
func (t *Table) updater(ctx context.Context, meta ResourceMeta) {
	ticker := time.NewTicker(meta.RefreshRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := t.refresh(ctx, meta); err != nil {
				t.fireLoadFailed(err)
			}
		}
	}
}

// refresh calls the DAO and notifies listeners on success.
func (t *Table) refresh(ctx context.Context, meta ResourceMeta) error {
	log.Debug().
		Str("resource", t.resource).
		Str("project", t.project).
		Msg("fetching from API")

	data, err := meta.DAO.List(ctx, t.project)
	if err != nil {
		return err
	}
	t.fireDataChanged(data)
	return nil
}

func (t *Table) fireDataChanged(data *dao.TableData) {
	t.mu.Lock()
	listeners := append([]TableListener(nil), t.listeners...)
	t.mu.Unlock()
	for _, l := range listeners {
		l.TableDataChanged(data)
	}
}

func (t *Table) fireLoadFailed(err error) {
	t.mu.Lock()
	listeners := append([]TableListener(nil), t.listeners...)
	t.mu.Unlock()
	for _, l := range listeners {
		l.TableLoadFailed(err)
	}
}
