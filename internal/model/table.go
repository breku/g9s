package model

import (
	"context"
	"sync"
	"time"

	"github.com/brekol/g9s/internal/cache"
	"github.com/brekol/g9s/internal/dao"
	"github.com/rs/zerolog/log"
)

const defaultRefreshRate = 30 * time.Second

// shared is the process-wide cache instance. All Table models share it so
// that switching between views of the same resource serves the cached copy
// instantly rather than issuing a duplicate API call.
var shared = cache.New()

// Table is the universal polling model for any GCP resource that can be
// displayed as a table. On each tick it checks the shared TTL cache first;
// only on a cache miss (or expiry) does it call the DAO and hit the GCP API.
//
// All TableListener callbacks are invoked from the background goroutine; views
// must dispatch tview mutations via app.QueueUpdateDraw.
type Table struct {
	resource    string
	project     string
	refreshRate time.Duration

	mu        sync.RWMutex
	listeners []TableListener
}

// NewTable creates a Table model for the given resource and project.
func NewTable(resource, project string) *Table {
	return &Table{
		resource:    resource,
		project:     project,
		refreshRate: defaultRefreshRate,
	}
}

// SetRefreshRate overrides the polling interval.
func (t *Table) SetRefreshRate(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.refreshRate = d
}

// AddListener registers a TableListener to receive model events.
func (t *Table) AddListener(l TableListener) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.listeners = append(t.listeners, l)
}

// Watch performs an immediate refresh and then polls on the refresh interval
// until ctx is cancelled. The first call serves from cache if the entry is
// still fresh, so the UI can paint instantly when revisiting a view.
// Returns an error only if the initial fetch fails with no cached fallback.
func (t *Table) Watch(ctx context.Context) error {
	if err := t.refresh(ctx); err != nil {
		t.fireLoadFailed(err)
		return err
	}
	go t.updater(ctx)
	return nil
}

// updater runs the polling loop in a background goroutine.
func (t *Table) updater(ctx context.Context) {
	rate := t.rate()
	ticker := time.NewTicker(rate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := t.refresh(ctx); err != nil {
				t.fireLoadFailed(err)
			}
		}
	}
}

// refresh serves from the TTL cache when the entry is still fresh, and calls
// the DAO only on a cache miss or expiry.
func (t *Table) refresh(ctx context.Context) error {
	meta, ok := Lookup(t.resource)
	if !ok {
		return nil
	}

	// Cache hit — serve immediately without touching the GCP API.
	if data, hit := shared.Get(t.resource, t.project); hit {
		log.Debug().
			Str("resource", t.resource).
			Str("project", t.project).
			Msg("cache hit")
		t.fireDataChanged(data)
		return nil
	}

	// Cache miss — call the DAO.
	log.Debug().
		Str("resource", t.resource).
		Str("project", t.project).
		Msg("cache miss — fetching from API")

	data, err := meta.DAO.List(ctx, t.project)
	if err != nil {
		return err
	}

	shared.Set(t.resource, t.project, data, meta.TTL)
	t.fireDataChanged(data)
	return nil
}

func (t *Table) rate() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.refreshRate
}

func (t *Table) fireDataChanged(data *dao.TableData) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, l := range t.listeners {
		l.TableDataChanged(data)
	}
}

func (t *Table) fireLoadFailed(err error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, l := range t.listeners {
		l.TableLoadFailed(err)
	}
}
