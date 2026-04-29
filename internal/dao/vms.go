package dao

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
	"gopkg.in/yaml.v3"
)

// Ensure VMs satisfies Accessor at compile time.
var _ Accessor = (*VMs)(nil)

// VMs is the DAO for Compute Engine VM instances.
type VMs struct{}

// Resource returns the stable identifier for this resource type.
func (v *VMs) Resource() string { return "vms" }

// Header returns the column headers for the VMs table view.
func (v *VMs) Header() []string {
	return []string{"NAME", "ZONE", "MACHINE TYPE", "STATUS", "INTERNAL IP", "EXTERNAL IP", "CREATED"}
}

// List fetches all Compute Engine instances across every zone in the project
// using a single AggregatedList call.
func (v *VMs) List(ctx context.Context, project string) (*TableData, error) {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("vms: credentials: %w", err)
	}

	client, err := compute.NewInstancesRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("vms: new client: %w", err)
	}
	defer client.Close()

	req := &computepb.AggregatedListInstancesRequest{Project: project}

	var rows []Row
	it := client.AggregatedList(ctx, req)
	for {
		pair, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("vms: aggregated list: %w", err)
		}
		// pair.Key is the scope (e.g. "zones/us-central1-a"); pair.Value
		// holds the instances list (or warning if zone has none).
		for _, inst := range pair.Value.GetInstances() {
			rows = append(rows, rowFromInstance(inst))
		}
	}

	return &TableData{
		Header: v.Header(),
		Rows:   rows,
	}, nil
}

// rowFromInstance converts a compute Instance proto to a table Row.
func rowFromInstance(inst *computepb.Instance) Row {
	name := inst.GetName()
	zone := lastSegment(inst.GetZone())
	machine := lastSegment(inst.GetMachineType())
	status := inst.GetStatus() // e.g. "RUNNING", "TERMINATED", "STOPPING"

	internalIP, externalIP := instanceIPs(inst)

	// CreationTimestamp is an RFC3339 string in the API.
	created := "—"
	if ts := inst.GetCreationTimestamp(); ts != "" {
		// Display the date+time portion without the timezone offset to keep
		// the column narrow; full value is available via Describe.
		if i := strings.Index(ts, "T"); i > 0 && len(ts) >= i+6 {
			created = ts[:i] + " " + ts[i+1:i+6]
		} else {
			created = ts
		}
	}

	colType := RowTypeNotActive
	switch status {
	case "RUNNING":
		colType = RowTypeActive
	case "STOPPING", "SUSPENDING", "REPAIRING":
		colType = RowTypeError
	}

	return Row{
		// Use the self link as a stable, fully-qualified ID.
		ID:   inst.GetSelfLink(),
		Type: colType,
		Meta: map[string]string{
			"zone":       zone,
			"name":       name,
			"id":         fmt.Sprintf("%d", inst.GetId()),
			"internalIP": internalIP,
			"externalIP": externalIP,
		},
		Columns: []Column{
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

// instanceIPs returns the first internal and external IPv4 address found on
// the instance's network interfaces.
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

// getInstance fetches a single Compute Engine instance.
func getInstance(ctx context.Context, project, zone, name string) (*computepb.Instance, error) {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("vms: credentials: %w", err)
	}
	client, err := compute.NewInstancesRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("vms: new client: %w", err)
	}
	defer client.Close()
	return client.Get(ctx, &computepb.GetInstanceRequest{
		Project:  project,
		Zone:     zone,
		Instance: name,
	})
}

// DescribeVMText returns a human-readable JSON description of a VM instance.
func DescribeVMText(ctx context.Context, project, zone, name string) (string, error) {
	inst, err := getInstance(ctx, project, zone, name)
	if err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return "", fmt.Errorf("vms: marshal: %w", err)
	}
	return string(b), nil
}

// DescribeVMYAML returns a YAML description of a VM instance.
func DescribeVMYAML(ctx context.Context, project, zone, name string) (string, error) {
	inst, err := getInstance(ctx, project, zone, name)
	if err != nil {
		return "", err
	}
	// proto → JSON (preserves field names) → generic map → YAML.
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

// DeleteVM issues a delete on the given Compute Engine instance. It returns
// as soon as the API accepts the request — it does NOT wait for the LRO to
// finish. The polling loop will reflect the new state on the next tick.
func DeleteVM(ctx context.Context, project, zone, name string) error {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return fmt.Errorf("vms: credentials: %w", err)
	}
	client, err := compute.NewInstancesRESTClient(ctx, opts...)
	if err != nil {
		return fmt.Errorf("vms: new client: %w", err)
	}
	// client closed below once the API call returns; we don't wait on the LRO.

	if _, err := client.Delete(ctx, &computepb.DeleteInstanceRequest{
		Project:  project,
		Zone:     zone,
		Instance: name,
	}); err != nil {
		_ = client.Close()
		return fmt.Errorf("vms: delete: %w", err)
	}
	go func() { _ = client.Close() }()
	return nil
}
