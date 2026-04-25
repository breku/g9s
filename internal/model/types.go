// Package model connects DAOs to the UI via an observer pattern.
// The Table model polls a DAO on a configurable interval and notifies
// registered TableListener implementations when data changes.
package model

import (
	"time"

	"github.com/brekol/g9s/internal/dao"
)

// TableListener is the observer interface implemented by views.
// All methods are called from the model's background goroutine; views MUST
// dispatch UI mutations via app.QueueUpdateDraw.
type TableListener interface {
	// TableDataChanged is called when the model has fresh data.
	TableDataChanged(data *dao.TableData)

	// TableLoadFailed is called when the DAO returns an error.
	TableLoadFailed(err error)
}

// ResourceMeta binds a resource identifier to its DAO and cache TTL.
// TTL controls how long a fetched result is considered fresh before the next
// real GCP API call is made. The polling interval in model.Table governs how
// often the cache is checked; the TTL governs when the cache is bypassed.
type ResourceMeta struct {
	DAO dao.Accessor
	TTL time.Duration
}
