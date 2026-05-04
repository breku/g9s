// Package vms provides the DAO for Compute Engine VM instances.
package vms

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
	"gopkg.in/yaml.v3"
)

// Ensure VMs satisfies Accessor and InstanceRow satisfies Row at compile time.
var (
	_ dao.Accessor      = (*VMs)(nil)
	_ dao.YAMLDescriber = (*VMs)(nil)
	_ dao.Row           = (*InstanceRow)(nil)
)

// VMs is the DAO for Compute Engine VM instances.
type VMs struct{}

// InstanceRow is the typed row for a Compute Engine instance.
// Project is parsed from the self-link in the constructor; Zone, Name and the
// numeric instance ID are surfaced for delete/describe/log handlers.
type InstanceRow struct {
	id      string
	rowType dao.RowType
	columns []dao.Column

	Project    string
	Zone       string
	Name       string
	NumericID  string
	InternalIP string
	ExternalIP string
}

// GetID implements dao.Row. Returns the instance self-link.
func (r *InstanceRow) GetID() string { return r.id }

// GetType implements dao.Row.
func (r *InstanceRow) GetType() dao.RowType { return r.rowType }

// GetColumns implements dao.Row.
func (r *InstanceRow) GetColumns() []dao.Column { return r.columns }

// CopyColumnValue copies the external IP if present, otherwise the internal IP,
// otherwise the instance name.
func (r *InstanceRow) CopyColumnValue() (string, bool) {
	switch {
	case r.ExternalIP != "":
		return r.ExternalIP, true
	case r.InternalIP != "":
		return r.InternalIP, true
	case r.Name != "":
		return r.Name, true
	}
	return "", false
}

// Resource returns the stable identifier for this resource type.
func (v *VMs) Resource() string { return "vms" }

// Header returns the column headers for the VMs table view.
func (v *VMs) Header() []string {
	return []string{"NAME", "ZONE", "MACHINE TYPE", "STATUS", "INTERNAL IP", "EXTERNAL IP", "CREATED"}
}

// List fetches all Compute Engine instances across every zone in the project.
func (v *VMs) List(ctx context.Context, project string) (*dao.TableData, error) {
	client, err := gcp.ComputeInstancesClient()
	if err != nil {
		return nil, fmt.Errorf("vms: client: %w", err)
	}

	req := &computepb.AggregatedListInstancesRequest{Project: project}

	var rows []dao.Row
	it := client.AggregatedList(ctx, req)
	for {
		pair, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("vms: aggregated list: %w", err)
		}
		for _, inst := range pair.Value.GetInstances() {
			rows = append(rows, rowFromInstance(inst))
		}
	}

	return &dao.TableData{
		Header: v.Header(),
		Rows:   rows,
	}, nil
}

func rowFromInstance(inst *computepb.Instance) *InstanceRow {
	name := inst.GetName()
	zone := dao.LastSegment(inst.GetZone())
	machine := dao.LastSegment(inst.GetMachineType())
	status := inst.GetStatus()

	internalIP, externalIP := instanceIPs(inst)

	created := "—"
	if ts := inst.GetCreationTimestamp(); ts != "" {
		if i := strings.Index(ts, "T"); i > 0 && len(ts) >= i+6 {
			created = ts[:i] + " " + ts[i+1:i+6]
		} else {
			created = ts
		}
	}

	colType := dao.RowTypeNotActive
	switch status {
	case "RUNNING":
		colType = dao.RowTypeActive
	case "STOPPING", "SUSPENDING", "REPAIRING":
		colType = dao.RowTypeError
	}

	return &InstanceRow{
		id:      inst.GetSelfLink(),
		rowType: colType,
		Project: func() string {
			p, _, _ := parseSelfLink(inst.GetSelfLink())
			return p
		}(),
		Zone:       zone,
		Name:       name,
		NumericID:  fmt.Sprintf("%d", inst.GetId()),
		InternalIP: internalIP,
		ExternalIP: externalIP,
		columns: []dao.Column{
			{Text: name},
			{Text: zone},
			{Text: machine},
			{Text: status},
			{Text: orDash(internalIP)},
			{Text: orDash(externalIP)},
			{Text: created},
		},
	}
}

func instanceIPs(inst *computepb.Instance) (internal, external string) {
	for _, nic := range inst.GetNetworkInterfaces() {
		if internal == "" {
			internal = nic.GetNetworkIP()
		}
		if external == "" {
			for _, ac := range nic.GetAccessConfigs() {
				if ip := ac.GetNatIP(); ip != "" {
					external = ip
					break
				}
			}
		}
		if internal != "" && external != "" {
			break
		}
	}
	return internal, external
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func getInstance(ctx context.Context, project, zone, name string) (*computepb.Instance, error) {
	client, err := gcp.ComputeInstancesClient()
	if err != nil {
		return nil, fmt.Errorf("vms: client: %w", err)
	}
	return client.Get(ctx, &computepb.GetInstanceRequest{
		Project:  project,
		Zone:     zone,
		Instance: name,
	})
}

// DescribeYAML implements dao.YAMLDescriber. id is the instance self-link.
func (v *VMs) DescribeYAML(ctx context.Context, id string) (string, error) {
	project, zone, name := parseSelfLink(id)
	if project == "" || zone == "" || name == "" {
		return "", fmt.Errorf("vms: cannot parse self-link: %q", id)
	}
	inst, err := getInstance(ctx, project, zone, name)
	if err != nil {
		return "", err
	}
	jsonBytes, err := json.Marshal(inst)
	if err != nil {
		return "", fmt.Errorf("vms: marshal json: %w", err)
	}
	var m interface{}
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return "", fmt.Errorf("vms: unmarshal json: %w", err)
	}
	yamlBytes, err := yaml.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("vms: marshal yaml: %w", err)
	}
	return string(yamlBytes), nil
}

// Delete issues a delete on the given Compute Engine instance.
// Returns as soon as the API accepts the request — does NOT wait for the LRO.
func (v *VMs) Delete(ctx context.Context, project, zone, name string) error {
	client, err := gcp.ComputeInstancesClient()
	if err != nil {
		return fmt.Errorf("vms: client: %w", err)
	}

	if _, err := client.Delete(ctx, &computepb.DeleteInstanceRequest{
		Project:  project,
		Zone:     zone,
		Instance: name,
	}); err != nil {
		return fmt.Errorf("vms: delete: %w", err)
	}
	return nil
}

// parseSelfLink extracts (project, zone, name) from a Compute self-link.
// Format: .../projects/<project>/zones/<zone>/instances/<name>
func parseSelfLink(self string) (project, zone, name string) {
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
		case "instances":
			if i+1 < len(parts) {
				name = parts[i+1]
			}
		}
	}
	return
}
