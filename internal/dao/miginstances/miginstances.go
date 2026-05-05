// Package miginstances provides the DAO for instances managed by a single
// Managed Instance Group (MIG). Unlike most DAOs, MIGInstances is
// parameterised by its parent MIG (Project, Location, Name, Scope) and so
// is not registered in model.Registry — it is constructed imperatively by
// the MIGsView drill-down handler with parent context already in hand.
package miginstances

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/dao/migs"
	"github.com/brekol/g9s/internal/dao/vms"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
)

// Compile-time assertions.
var (
	_ dao.Accessor      = (*MIGInstances)(nil)
	_ dao.YAMLDescriber = (*MIGInstances)(nil)
	_ dao.Row           = (*ManagedInstanceRow)(nil)
)

// MIGInstances is the DAO for instances belonging to a single MIG. The
// parent MIG is identified by Project, Location (zone or region), Name and
// Scope; FetchPage routes to the zonal or regional ListManagedInstances
// RPC based on Scope.
type MIGInstances struct {
	Project  string
	Location string // zone for zonal MIGs, region for regional MIGs
	Name     string // MIG name
	Scope    migs.Scope
}

// ManagedInstanceRow is the typed row for one managed instance. NumericID
// is the underlying compute instance numeric ID (for log filtering); empty
// when the instance has not yet been created.
type ManagedInstanceRow struct {
	id      string
	rowType dao.RowType
	columns []dao.Column

	Project   string
	Zone      string // resolved per-instance from the instance self-link
	Name      string
	NumericID string
}

// GetID implements dao.Row. Returns the instance self-link when available,
// otherwise the synthesised "name@zone" so the row remains uniquely keyed
// even before the instance has been created.
func (r *ManagedInstanceRow) GetID() string { return r.id }

// GetType implements dao.Row.
func (r *ManagedInstanceRow) GetType() dao.RowType { return r.rowType }

// GetColumns implements dao.Row.
func (r *ManagedInstanceRow) GetColumns() []dao.Column { return r.columns }

// CopyColumnValue copies the instance name (gcloud-friendly).
func (r *ManagedInstanceRow) CopyColumnValue() (string, bool) {
	if r.Name == "" {
		return "", false
	}
	return r.Name, true
}

// Resource returns the stable identifier for this resource type.
func (m *MIGInstances) Resource() string { return "miginstances" }

// Header returns the column headers, mirroring `gcloud compute
// instance-groups managed list-instances`.
func (m *MIGInstances) Header() []string {
	return []string{"NAME", "ZONE", "STATUS", "HEALTH", "ACTION", "TEMPLATE", "VERSION", "LAST_ERROR"}
}

// FetchPage implements dao.Accessor. The project arg from the model layer
// is ignored in favour of m.Project, since the parent MIG's project was
// captured at construction time.
func (m *MIGInstances) FetchPage(ctx context.Context, _, pageToken string, pageSize int) (*dao.TableData, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	max := uint32(pageSize)

	var rows []dao.Row

	switch m.Scope {
	case migs.ScopeRegional:
		client, err := gcp.RegionInstanceGroupManagersClient()
		if err != nil {
			return nil, fmt.Errorf("miginstances: client: %w", err)
		}
		req := &computepb.ListManagedInstancesRegionInstanceGroupManagersRequest{
			Project:              m.Project,
			Region:               m.Location,
			InstanceGroupManager: m.Name,
			MaxResults:           &max,
		}
		if pageToken != "" {
			req.PageToken = &pageToken
		}
		it := client.ListManagedInstances(ctx, req)
		for len(rows) < pageSize {
			mi, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("miginstances: list regional: %w", err)
			}
			rows = append(rows, rowFromManagedInstance(mi))
		}
		return &dao.TableData{
			Header:        m.Header(),
			Rows:          rows,
			NextPageToken: it.PageInfo().Token,
		}, nil
	default:
		client, err := gcp.InstanceGroupManagersClient()
		if err != nil {
			return nil, fmt.Errorf("miginstances: client: %w", err)
		}
		req := &computepb.ListManagedInstancesInstanceGroupManagersRequest{
			Project:              m.Project,
			Zone:                 m.Location,
			InstanceGroupManager: m.Name,
			MaxResults:           &max,
		}
		if pageToken != "" {
			req.PageToken = &pageToken
		}
		it := client.ListManagedInstances(ctx, req)
		for len(rows) < pageSize {
			mi, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("miginstances: list zonal: %w", err)
			}
			rows = append(rows, rowFromManagedInstance(mi))
		}
		return &dao.TableData{
			Header:        m.Header(),
			Rows:          rows,
			NextPageToken: it.PageInfo().Token,
		}, nil
	}
}

// DescribeYAML implements dao.YAMLDescriber. Delegates to vms.GetInstanceYAML
// since a managed instance is just a regular Compute Engine instance once
// it exists. id is the instance self-link.
func (m *MIGInstances) DescribeYAML(ctx context.Context, id string) (string, error) {
	if !strings.Contains(id, "/instances/") {
		return "", fmt.Errorf("miginstances: instance not yet created: %q", id)
	}
	return vms.GetInstanceYAML(ctx, id)
}

func rowFromManagedInstance(mi *computepb.ManagedInstance) *ManagedInstanceRow {
	self := mi.GetInstance()
	name := mi.GetName()
	if name == "" {
		name = lastSegment(self)
	}

	project, zone := parseInstanceSelfLink(self)

	status := mi.GetInstanceStatus()
	if status == "" {
		status = "—"
	}

	action := mi.GetCurrentAction()
	if action == "" {
		action = "NONE"
	}

	template := dao.LastSegment(mi.GetVersion().GetInstanceTemplate())
	if template == "" {
		template = "—"
	}
	version := mi.GetVersion().GetName()
	if version == "" {
		version = "—"
	}

	health := healthAggregate(mi.GetInstanceHealth())
	lastErr := truncate(firstErrorMessage(mi.GetLastAttempt()), 60)
	if lastErr == "" {
		lastErr = "—"
	}

	rowType := dao.RowTypeNotActive
	switch {
	case mi.GetLastAttempt() != nil && len(mi.GetLastAttempt().GetErrors().GetErrors()) > 0:
		rowType = dao.RowTypeError
	case action != "NONE":
		rowType = dao.RowTypeNotActive
	case status == "RUNNING":
		rowType = dao.RowTypeActive
	}

	id := self
	if id == "" {
		id = name + "@" + zone
	}

	numericID := ""
	if n := mi.GetId(); n != 0 {
		numericID = fmt.Sprintf("%d", n)
	}

	if zone == "" {
		zone = "—"
	}

	return &ManagedInstanceRow{
		id:        id,
		rowType:   rowType,
		Project:   project,
		Zone:      zone,
		Name:      name,
		NumericID: numericID,
		columns: []dao.Column{
			{Text: name},
			{Text: zone},
			{Text: status},
			{Text: health},
			{Text: action},
			{Text: template},
			{Text: version},
			{Text: lastErr},
		},
	}
}

// healthAggregate collapses a list of per-health-check states into a single
// label suitable for a column. Empty list → "—".
func healthAggregate(checks []*computepb.ManagedInstanceInstanceHealth) string {
	if len(checks) == 0 {
		return "—"
	}
	allHealthy := true
	anyUnhealthy := false
	first := ""
	for _, c := range checks {
		s := c.GetDetailedHealthState()
		if first == "" {
			first = s
		}
		if s != "HEALTHY" {
			allHealthy = false
		}
		if s == "UNHEALTHY" {
			anyUnhealthy = true
		}
	}
	switch {
	case anyUnhealthy:
		return "UNHEALTHY"
	case allHealthy:
		return "HEALTHY"
	case first != "":
		return first
	default:
		return "—"
	}
}

// firstErrorMessage extracts the first error message from a LastAttempt,
// or "" if there are no errors.
func firstErrorMessage(la *computepb.ManagedInstanceLastAttempt) string {
	if la == nil {
		return ""
	}
	errs := la.GetErrors().GetErrors()
	if len(errs) == 0 {
		return ""
	}
	return errs[0].GetMessage()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return s[:max-1] + "…"
}

// parseInstanceSelfLink extracts (project, zone) from an instance self-link
// of the form .../projects/<p>/zones/<zone>/instances/<name>. Returns
// empty strings when the link is malformed or the instance hasn't been
// created yet (empty self-link).
func parseInstanceSelfLink(self string) (project, zone string) {
	if self == "" {
		return "", ""
	}
	parts := strings.Split(self, "/")
	for i, p := range parts {
		switch p {
		case "projects":
			if i+1 < len(parts) {
				project = parts[i+1]
			}
		case "zones":
			if i+1 < len(parts) {
				zone = parts[i+1]
			}
		}
	}
	return
}

func lastSegment(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "/")
	return parts[len(parts)-1]
}
