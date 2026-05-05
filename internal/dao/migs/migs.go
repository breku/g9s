// Package migs provides the DAO for Compute Engine Managed Instance Groups.
//
// MIGs come in two flavours: zonal (scoped to a single zone) and regional
// (spread across multiple zones in a region). The aggregated list endpoint
// returns both kinds in a single call, but Get is split across two clients
// (InstanceGroupManagersClient for zonal, RegionInstanceGroupManagersClient
// for regional). DescribeYAML inspects the parsed self-link to pick the
// right client.
package migs

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
)

// Ensure MIGs satisfies Accessor and MIGRow satisfies Row at compile time.
var (
	_ dao.Accessor      = (*MIGs)(nil)
	_ dao.YAMLDescriber = (*MIGs)(nil)
	_ dao.Row           = (*MIGRow)(nil)
)

// MIGs is the DAO for Compute Engine Managed Instance Groups.
type MIGs struct{}

// MIGRow is the typed row for a Managed Instance Group. Project, Location
// (zone or region), Name and Scope are surfaced so action handlers can call
// the right client without re-parsing the self-link.
type MIGRow struct {
	id      string
	rowType dao.RowType
	columns []dao.Column

	Project  string
	Location string // zone for zonal MIGs, region for regional MIGs
	Name     string
	Scope    Scope
}

// Scope distinguishes zonal from regional MIGs. It is parsed from the
// self-link in the constructor.
type Scope int

const (
	// ScopeZonal indicates a zonal MIG (scoped to a single zone).
	ScopeZonal Scope = iota
	// ScopeRegional indicates a regional MIG (spread across a region).
	ScopeRegional
)

// GetID implements dao.Row. Returns the MIG self-link.
func (r *MIGRow) GetID() string { return r.id }

// GetType implements dao.Row.
func (r *MIGRow) GetType() dao.RowType { return r.rowType }

// GetColumns implements dao.Row.
func (r *MIGRow) GetColumns() []dao.Column { return r.columns }

// CopyColumnValue copies the MIG name (gcloud-friendly).
func (r *MIGRow) CopyColumnValue() (string, bool) {
	if r.Name == "" {
		return "", false
	}
	return r.Name, true
}

// Resource returns the stable identifier for this resource type.
func (m *MIGs) Resource() string { return "migs" }

// Header returns the column headers for the MIGs table view.
func (m *MIGs) Header() []string {
	return []string{"NAME", "LOCATION", "SCOPE", "SIZE", "TEMPLATE", "STATUS", "CREATED"}
}

// FetchPage implements dao.Accessor. The aggregated-list endpoint returns
// both zonal and regional MIGs, keyed by location, in a single call. We
// flatten and cap at pageSize. An empty pageToken requests the first page.
func (m *MIGs) FetchPage(ctx context.Context, project, pageToken string, pageSize int) (*dao.TableData, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	client, err := gcp.InstanceGroupManagersClient()
	if err != nil {
		return nil, fmt.Errorf("migs: client: %w", err)
	}

	max := uint32(pageSize)
	req := &computepb.AggregatedListInstanceGroupManagersRequest{
		Project:    project,
		MaxResults: &max,
	}
	if pageToken != "" {
		req.PageToken = &pageToken
	}

	var rows []dao.Row
	it := client.AggregatedList(ctx, req)
	for len(rows) < pageSize {
		pair, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("migs: aggregated list: %w", err)
		}
		for _, mig := range pair.Value.GetInstanceGroupManagers() {
			rows = append(rows, rowFromMIG(mig))
			if len(rows) >= pageSize {
				break
			}
		}
	}

	return &dao.TableData{
		Header:        m.Header(),
		Rows:          rows,
		NextPageToken: it.PageInfo().Token,
	}, nil
}

func rowFromMIG(mig *computepb.InstanceGroupManager) *MIGRow {
	name := mig.GetName()
	self := mig.GetSelfLink()
	project, location, scope := parseSelfLink(self)
	scopeText := "zonal"
	if scope == ScopeRegional {
		scopeText = "regional"
	}

	template := dao.LastSegment(mig.GetInstanceTemplate())
	if template == "" {
		template = "—"
	}

	size := fmt.Sprintf("%d", mig.GetTargetSize())

	// Status: derive a single text from the per-instance current actions.
	// "Stable" when none / verifying-only; "Updating" when there's any
	// pending/recreating/restarting/deleting action; otherwise "—".
	status := "Stable"
	rowType := dao.RowTypeActive
	if act := mig.GetCurrentActions(); act != nil {
		busy := act.GetCreating() + act.GetCreatingWithoutRetries() +
			act.GetDeleting() + act.GetRecreating() +
			act.GetRefreshing() + act.GetRestarting() +
			act.GetResuming() + act.GetStarting() +
			act.GetStopping() + act.GetSuspending() +
			act.GetVerifying()
		if busy > 0 {
			status = fmt.Sprintf("Updating (%d)", busy)
			rowType = dao.RowTypeNotActive
		}
	}
	if s := mig.GetStatus(); s != nil && !s.GetIsStable() {
		// IsStable=false overrides the action heuristic above.
		if status == "Stable" {
			status = "Reconciling"
		}
		rowType = dao.RowTypeNotActive
	}

	created := "—"
	if ts := mig.GetCreationTimestamp(); ts != "" {
		if i := strings.Index(ts, "T"); i > 0 && len(ts) >= i+6 {
			created = ts[:i] + " " + ts[i+1:i+6]
		} else {
			created = ts
		}
	}

	return &MIGRow{
		id:       self,
		rowType:  rowType,
		Project:  project,
		Location: location,
		Name:     name,
		Scope:    scope,
		columns: []dao.Column{
			{Text: name},
			{Text: location},
			{Text: scopeText},
			{Text: size},
			{Text: template},
			{Text: status},
			{Text: created},
		},
	}
}

// DescribeYAML implements dao.YAMLDescriber. id is the MIG self-link.
// Routes to the zonal or regional Get RPC based on the parsed self-link.
func (m *MIGs) DescribeYAML(ctx context.Context, id string) (string, error) {
	project, location, scope := parseSelfLink(id)
	name := dao.LastSegment(id)
	if project == "" || location == "" || name == "" {
		return "", fmt.Errorf("migs: cannot parse self-link: %q", id)
	}

	switch scope {
	case ScopeRegional:
		client, err := gcp.RegionInstanceGroupManagersClient()
		if err != nil {
			return "", fmt.Errorf("migs: client: %w", err)
		}
		mig, err := client.Get(ctx, &computepb.GetRegionInstanceGroupManagerRequest{
			Project:              project,
			Region:               location,
			InstanceGroupManager: name,
		})
		if err != nil {
			return "", fmt.Errorf("migs: get regional: %w", err)
		}
		return marshalYAML(mig)
	default:
		client, err := gcp.InstanceGroupManagersClient()
		if err != nil {
			return "", fmt.Errorf("migs: client: %w", err)
		}
		mig, err := client.Get(ctx, &computepb.GetInstanceGroupManagerRequest{
			Project:              project,
			Zone:                 location,
			InstanceGroupManager: name,
		})
		if err != nil {
			return "", fmt.Errorf("migs: get zonal: %w", err)
		}
		return marshalYAML(mig)
	}
}

// marshalYAML renders a value as YAML by going through JSON first, so field
// names match the JSON representation users see in `gcloud ... --format=yaml`
// rather than the underlying Go struct field names.
func marshalYAML(v interface{}) (string, error) {
	out, err := dao.ObjectToYAML(v)
	if err != nil {
		return "", fmt.Errorf("migs: %w", err)
	}
	return out, nil
}

// parseSelfLink extracts (project, location, scope) from a MIG self-link.
// Zonal:    .../projects/<p>/zones/<zone>/instanceGroupManagers/<name>
// Regional: .../projects/<p>/regions/<region>/instanceGroupManagers/<name>
func parseSelfLink(self string) (project, location string, scope Scope) {
	parts := strings.Split(self, "/")
	for i, p := range parts {
		switch p {
		case "projects":
			if i+1 < len(parts) {
				project = parts[i+1]
			}
		case "zones":
			if i+1 < len(parts) {
				location = parts[i+1]
				scope = ScopeZonal
			}
		case "regions":
			if i+1 < len(parts) {
				location = parts[i+1]
				scope = ScopeRegional
			}
		}
	}
	return
}
