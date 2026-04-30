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

// ResourceMeta binds a resource identifier to its DAO and background
// refresh rate.
//
// RefreshRate is the interval between background fetches while a view is
// active (i.e. between the user opening the view and switching away from
// it). Choose it based on how often the resource changes in practice and
// the cost of the List call.
type ResourceMeta struct {
	DAO         dao.Accessor
	RefreshRate time.Duration
}
