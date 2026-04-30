// Package vms provides the DAO for Compute Engine VM instances.
package vms

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
	"gopkg.in/yaml.v3"
)

// Ensure VMs satisfies Accessor at compile time.
var _ dao.Accessor = (*VMs)(nil)

// VMs is the DAO for Compute Engine VM instances.
type VMs struct{}

// Resource returns the stable identifier for this resource type.
func (v *VMs) Resource() string { return "vms" }

// Header returns the column headers for the VMs table view.
func (v *VMs) Header() []string {
	return []string{"NAME", "ZONE", "MACHINE TYPE", "STATUS", "INTERNAL IP", "EXTERNAL IP", "CREATED"}
}

// List fetches all Compute Engine instances across every zone in the project.
func (v *VMs) List(ctx context.Context, project string) (*dao.TableData, error) {
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

func rowFromInstance(inst *computepb.Instance) dao.Row {
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

	return dao.Row{
		ID:   inst.GetSelfLink(),
		Type: colType,
		Meta: map[string]string{
			"zone":       zone,
			"name":       name,
			"id":         fmt.Sprintf("%d", inst.GetId()),
			"internalIP": internalIP,
			"externalIP": externalIP,
		},
		Columns: []dao.Column{
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

// DeleteVM issues a delete on the given Compute Engine instance.
// Returns as soon as the API accepts the request — does NOT wait for the LRO.
func DeleteVM(ctx context.Context, project, zone, name string) error {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return fmt.Errorf("vms: credentials: %w", err)
	}
	client, err := compute.NewInstancesRESTClient(ctx, opts...)
	if err != nil {
		return fmt.Errorf("vms: new client: %w", err)
	}

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
