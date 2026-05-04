// Package dao provides data-access objects for GCP resources.
// Each resource type has its own DAO that implements the Accessor interface.
// Optional capability interfaces (Describer, Lister, etc.) are opted-into
// by concrete DAOs and discovered via runtime type assertions.
package dao

import (
	"context"
)

// RowType indicates the semantic state of a column value, used by the UI
// to apply colour coding independent of column position or string content.
type RowType int

const (
	// RowTypeNotActive is the default — rendered in grey.
	RowTypeNotActive RowType = iota
	// RowTypeActive indicates a healthy / running / enabled state — rendered in green.
	RowTypeActive
	// RowTypeError indicates a failed / degraded state — rendered in red.
	RowTypeError
)

// Column is a single cell value in a Row.
type Column struct {
	Text string
}

// Row represents a single resource row returned by a DAO.
// Concrete row types live in per-resource subpackages and carry typed fields
// the UI may need for actions. The shared columns produced by GetColumns()
// must match the headers returned by the same DAO's Header() method.
type Row interface {
	// GetID returns the canonical, fully-qualified resource name used to
	// identify this row across cache reads, log lookups and action calls.
	GetID() string
	// GetType returns the semantic state used for row colouring.
	GetType() RowType
	// GetColumns returns the cell values to render, in header order.
	GetColumns() []Column
	// CopyColumnValue returns the value the 'c' key copies to the clipboard
	// for this resource type. The bool is false when nothing meaningful is
	// available to copy (the UI then no-ops).
	CopyColumnValue() (string, bool)
}

// TableData is the full data set produced by a DAO: column headers + rows.
type TableData struct {
	Header        []string
	Rows          []Row
	NextPageToken string // non-empty when more pages are available
}

// Accessor is the single required interface every DAO must satisfy.
// It provides paginated list semantics and self-identification.
type Accessor interface {
	// Resource returns a short, stable identifier for the resource type
	// (e.g. "cloudrun", "gce-instances"). Used as the registry key.
	Resource() string

	// Header returns the column names for this resource's table view.
	Header() []string

	// FetchPage fetches one page of resources for the given project.
	// An empty pageToken requests the first page; subsequent calls should
	// pass the NextPageToken returned by the previous TableData. pageSize
	// is the maximum number of rows the caller wants in this page; DAOs
	// should treat values <= 0 as "use a sensible default".
	//
	// When the underlying API does not support cursors (e.g. aggregated
	// listings), implementations may emulate pagination by capping the
	// result count and returning an empty NextPageToken on the last page.
	FetchPage(ctx context.Context, project, pageToken string, pageSize int) (*TableData, error)
}

// YAMLDescriber is an optional capability for DAOs that can render a single
// resource as YAML, addressed by canonical ID. The same string is the input
// the user edits when pressing 'e' on resources that also support edit.
type YAMLDescriber interface {
	DescribeYAML(ctx context.Context, id string) (string, error)
}
