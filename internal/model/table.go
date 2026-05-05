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
//   - Watch fetches the first page once synchronously, then ticks at the
//     resource's RefreshRate, refetching the first page each time and
//     merging it into whatever pages the user has loaded via PgDn.
//   - LoadNextPage fetches the next page using the cursor returned with
//     the previous page and notifies listeners with the accumulated data.
//   - Stop cancels the polling goroutine. The App calls this when the
//     user navigates away so background fetches don't accumulate.
//   - Watch is re-entrant: a second call cancels the previous polling
//     goroutine and starts a new one bound to the new ctx. This lets the
//     App resume polling on switch-back with a fresh context.
//
// Pagination is owned by the model: the UI layer just renders whatever
// TableData arrives via TableDataChanged and calls LoadNextPage on PgDn.
// This keeps the merge-on-refresh logic (so periodic polls don't snap the
// user back to page 1) in one place and lets every resource paginate
// uniformly with no per-view boilerplate.
//
// There is no cache between views: switching back to a view always
// re-fetches the first page. The trade-off is a brief "Loading…" frame on
// every switch-back in exchange for guaranteed-fresh data and zero
// cache-coherence machinery.
//
// All TableListener callbacks are invoked from a background goroutine; views
// must dispatch tview mutations via app.QueueUpdateDraw.
type Table struct {
	resource string
	project  string

	// localMeta, when non-nil, supplies the DAO + RefreshRate for this Table
	// in place of the global Registry. Used by parameterised drill-down
	// resources (e.g. miginstances scoped to a parent MIG) that have no
	// project-only constructor and therefore can't live in the Registry.
	localMeta *ResourceMeta

	mu        sync.Mutex
	listeners []TableListener
	cancel    context.CancelFunc // cancels the active polling goroutine; nil when stopped
	watchCtx  context.Context    // ctx tied to the active Watch; nil when stopped

	// Pagination state. All access guarded by mu so LoadNextPage (called
	// from the UI goroutine) and the polling goroutine don't race.
	allRows     []dao.Row
	header      []string
	nextCursor  string
	paginated   bool // true once the user has loaded at least one extra page
	loadingPage bool // true while a LoadNextPage fetch is in flight
}

// NewTable creates a Table model for the given resource and project.
// The resource key is looked up in the global Registry to find its DAO and
// RefreshRate. For parameterised drill-down resources that can't live in
// the Registry, use NewTableWithMeta instead.
func NewTable(resource, project string) *Table {
	return &Table{
		resource: resource,
		project:  project,
	}
}

// NewTableWithMeta creates a Table whose DAO and RefreshRate are supplied
// inline rather than looked up from the global Registry. Used by
// parameterised drill-down resources (e.g. miginstances) that are
// constructed imperatively with parent context already in hand and so
// don't fit the Registry's project-only-constructor shape.
//
// The resource key is still used for log messages; choose a descriptive
// name like "miginstances".
func NewTableWithMeta(resource, project string, meta ResourceMeta) *Table {
	return &Table{
		resource:  resource,
		project:   project,
		localMeta: &meta,
	}
}

// resolveMeta returns the effective ResourceMeta: the inline meta when set,
// otherwise the global Registry entry. Returns ok=false when neither is
// available.
func (t *Table) resolveMeta() (ResourceMeta, bool) {
	if t.localMeta != nil {
		return *t.localMeta, true
	}
	return Lookup(t.resource)
}

// AddListener registers a TableListener to receive model events.
func (t *Table) AddListener(l TableListener) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.listeners = append(t.listeners, l)
}

// Project returns the GCP project this table is bound to.
func (t *Table) Project() string { return t.project }

// HasNextPage reports whether a LoadNextPage call would attempt a fetch.
// Useful for UI hints (e.g. rendering a "PageDown to load more…" footer).
func (t *Table) HasNextPage() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.nextCursor != "" && !t.loadingPage
}

// Watch performs an immediate first-page refresh and then polls on the
// resource's configured RefreshRate until the derived context is cancelled.
//
// Re-entrant: if a previous Watch is still running, it is cancelled first
// and the pagination accumulator is reset so the new view starts fresh.
// Returns an error only if the initial fetch fails; subsequent tick failures
// are delivered to listeners via TableLoadFailed.
func (t *Table) Watch(ctx context.Context) error {
	meta, ok := t.resolveMeta()
	if !ok {
		return nil
	}

	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	subCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	t.watchCtx = subCtx
	// Reset pagination state for the new Watch session.
	t.allRows = nil
	t.header = nil
	t.nextCursor = ""
	t.paginated = false
	t.loadingPage = false
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
		t.watchCtx = nil
	}
}

// LoadNextPage fetches the next page using the cursor from the previous
// FetchPage call and notifies listeners with the accumulated rows. No-op
// when there is no next page or another page fetch is already in flight.
//
// Safe to call from any goroutine; the actual fetch runs in a background
// goroutine so the caller (typically a UI input handler) doesn't block.
func (t *Table) LoadNextPage() {
	meta, ok := t.resolveMeta()
	if !ok {
		return
	}

	t.mu.Lock()
	if t.nextCursor == "" || t.loadingPage {
		t.mu.Unlock()
		return
	}
	t.loadingPage = true
	cursor := t.nextCursor
	ctx := t.watchCtx
	t.mu.Unlock()

	if ctx == nil {
		// Watch hasn't been called or was Stopped. Fall back to a fresh
		// context so the request still has a chance to complete; the
		// caller will see the result via TableDataChanged.
		ctx = context.Background()
	}

	go func() {
		data, err := meta.DAO.FetchPage(ctx, t.project, cursor, DefaultPageSize)
		if err != nil {
			log.Error().Err(err).Str("resource", t.resource).Msg("table: next page failed")
			t.mu.Lock()
			t.loadingPage = false
			t.mu.Unlock()
			t.fireLoadFailed(err)
			return
		}

		t.mu.Lock()
		t.loadingPage = false
		if len(t.header) == 0 {
			t.header = data.Header
		}
		t.allRows = append(t.allRows, data.Rows...)
		t.nextCursor = data.NextPageToken
		t.paginated = true
		snapshot := t.snapshotLocked()
		t.mu.Unlock()

		t.fireDataChanged(snapshot)
	}()
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

// refresh fetches the first page and either (a) replaces the accumulator
// when the user hasn't paginated yet, or (b) merges the fresh first page
// into the accumulator so the user isn't snapped back to page 1 on each
// poll tick. In the merge case, nextCursor is left untouched because it
// already points past pages the user has loaded; using the token returned
// with page 1 would re-fetch already-visible pages.
func (t *Table) refresh(ctx context.Context, meta ResourceMeta) error {
	log.Debug().
		Str("resource", t.resource).
		Str("project", t.project).
		Int("pageSize", DefaultPageSize).
		Msg("fetching from API")

	data, err := meta.DAO.FetchPage(ctx, t.project, "", DefaultPageSize)
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.header = data.Header
	if t.paginated {
		t.allRows = mergeFirstPage(t.allRows, data.Rows)
		// Preserve t.nextCursor — it points past pages we've loaded.
	} else {
		t.allRows = data.Rows
		t.nextCursor = data.NextPageToken
	}
	snapshot := t.snapshotLocked()
	t.mu.Unlock()

	t.fireDataChanged(snapshot)
	return nil
}

// snapshotLocked returns a TableData built from the current accumulator
// state. Caller must hold t.mu.
func (t *Table) snapshotLocked() *dao.TableData {
	rows := make([]dao.Row, len(t.allRows))
	copy(rows, t.allRows)
	return &dao.TableData{
		Header:        t.header,
		Rows:          rows,
		NextPageToken: t.nextCursor,
	}
}

// mergeFirstPage updates the accumulator with a freshly polled first page
// while preserving rows the user has loaded by paginating. Existing rows
// whose IDs appear in freshRows are replaced in place (status updates flow
// through). Rows in freshRows whose IDs aren't yet known are prepended in
// their original order.
func mergeFirstPage(existing, freshRows []dao.Row) []dao.Row {
	freshByID := make(map[string]dao.Row, len(freshRows))
	for _, row := range freshRows {
		freshByID[row.GetID()] = row
	}
	knownIDs := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		knownIDs[row.GetID()] = struct{}{}
	}

	merged := make([]dao.Row, 0, len(existing)+len(freshRows))
	seen := make(map[string]struct{}, len(existing)+len(freshRows))

	// Prepend new-to-us rows from the fresh first page in their original order.
	for _, row := range freshRows {
		if _, known := knownIDs[row.GetID()]; !known {
			merged = append(merged, row)
			seen[row.GetID()] = struct{}{}
		}
	}

	// Then walk the existing accumulator, replacing with the fresh copy
	// when one exists so statuses (e.g. QUEUED → SUCCESS) update.
	for _, row := range existing {
		id := row.GetID()
		if _, dup := seen[id]; dup {
			continue
		}
		if fresh, ok := freshByID[id]; ok {
			merged = append(merged, fresh)
		} else {
			merged = append(merged, row)
		}
		seen[id] = struct{}{}
	}
	return merged
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
