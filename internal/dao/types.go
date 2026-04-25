// Package dao provides data-access objects for GCP resources.
// Each resource type has its own DAO that implements the Accessor interface.
// Optional capability interfaces (Describer, Lister, etc.) are opted-into
// by concrete DAOs and discovered via runtime type assertions.
package dao

import (
	"context"
)

// Row represents a single resource row returned by a DAO.
// Columns must match the headers returned by the same DAO's Header() method.
type Row struct {
	// ID is the fully-qualified resource name, used to identify the row.
	ID      string
	Columns []string
}

// TableData is the full data set produced by a DAO: column headers + rows.
type TableData struct {
	Header        []string
	Rows          []Row
	NextPageToken string // non-empty when more pages are available
}

// Accessor is the single required interface every DAO must satisfy.
// It provides list semantics and self-identification.
type Accessor interface {
	// Resource returns a short, stable identifier for the resource type
	// (e.g. "cloudrun", "gce-instances"). Used as the registry key.
	Resource() string

	// Header returns the column names for this resource's table view.
	Header() []string

	// List fetches all instances of the resource in the given project.
	// Returns structured TableData ready for the model layer.
	List(ctx context.Context, project string) (*TableData, error)
}

// Describer is an optional interface for DAOs that can produce a
// human-readable detail/describe output for a single resource instance.
type Describer interface {
	Describe(ctx context.Context, project, resourceID string) (string, error)
}

// Paginator is an optional interface for DAOs that support cursor-based
// pagination. List() on such a DAO returns only the first page; callers
// use NextPage to fetch subsequent pages using the token from TableData.
type Paginator interface {
	// NextPage fetches the next page of results using the given page token.
	// pageSize is the maximum number of rows to return.
	NextPage(ctx context.Context, project, pageToken string, pageSize int) (*TableData, error)
}
