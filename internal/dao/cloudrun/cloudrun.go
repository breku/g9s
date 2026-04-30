// Package cloudrun provides the DAO for Cloud Run v2 services.
package cloudrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"
	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/yaml.v3"
)

// Ensure CloudRun satisfies Accessor and ServiceRow satisfies Row at compile time.
var (
	_ dao.Accessor = (*CloudRun)(nil)
	_ dao.Row      = (*ServiceRow)(nil)
)

// CloudRun is the DAO for Cloud Run v2 Services.
type CloudRun struct{}

// ServiceRow is the typed row for a Cloud Run service.
// URL is exposed so the UI handler can copy or open it without map lookups.
type ServiceRow struct {
	id      string
	rowType dao.RowType
	columns []dao.Column
	URL     string
}

// GetID implements dao.Row.
func (r *ServiceRow) GetID() string { return r.id }

// GetType implements dao.Row.
func (r *ServiceRow) GetType() dao.RowType { return r.rowType }

// GetColumns implements dao.Row.
func (r *ServiceRow) GetColumns() []dao.Column { return r.columns }

// CopyColumnValue implements dao.Row. The copy target for a Cloud Run row is
// the service URL — empty (false) when the service has no URL yet.
func (r *ServiceRow) CopyColumnValue() (string, bool) {
	if r.URL == "" {
		return "", false
	}
	return r.URL, true
}

// Resource returns the stable identifier for this resource type.
func (c *CloudRun) Resource() string { return "cloudrun" }

// Header returns the column headers for the Cloud Run table view.
func (c *CloudRun) Header() []string {
	return []string{"NAME", "REGION", "STATUS", "URL", "LAST DEPLOYED", "DEPLOYED BY"}
}

// List fetches all Cloud Run services across all regions in the given project.
func (c *CloudRun) List(ctx context.Context, project string) (*dao.TableData, error) {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloudrun: credentials: %w", err)
	}

	client, err := run.NewServicesClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("cloudrun: new client: %w", err)
	}
	defer client.Close()

	// "-" as location means list across all regions.
	req := &runpb.ListServicesRequest{
		Parent: fmt.Sprintf("projects/%s/locations/-", project),
	}

	var rows []dao.Row
	it := client.ListServices(ctx, req)
	for {
		svc, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cloudrun: list: %w", err)
		}
		rows = append(rows, rowFromService(svc))
	}

	return &dao.TableData{
		Header: c.Header(),
		Rows:   rows,
	}, nil
}

// rowFromService converts a Cloud Run Service proto to a typed ServiceRow.
func rowFromService(svc *runpb.Service) *ServiceRow {
	name := dao.LastSegment(svc.Name)
	region := locationFromName(svc.Name)
	status := conditionState(svc.TerminalCondition)
	url := svc.Uri
	deployed := dao.FormatTime(svc.UpdateTime.AsTime())
	deployedBy := svc.LastModifier
	if deployedBy == "" {
		deployedBy = "—"
	}

	colType := dao.RowTypeNotActive
	if status == "Ready" {
		colType = dao.RowTypeActive
	} else if status == "Failed" {
		colType = dao.RowTypeError
	}

	return &ServiceRow{
		id:      svc.Name,
		rowType: colType,
		URL:     url,
		columns: []dao.Column{
			{Text: name},
			{Text: region},
			{Text: status},
			{Text: url},
			{Text: deployed},
			{Text: deployedBy},
		},
	}
}

// locationFromName extracts the location/region from a fully-qualified name.
// Format: projects/{p}/locations/{location}/services/{service}
func locationFromName(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return "unknown"
}

// conditionState maps a Condition to a human-readable status string.
func conditionState(c *runpb.Condition) string {
	if c == nil {
		return "Unknown"
	}
	switch c.State {
	case runpb.Condition_CONDITION_SUCCEEDED:
		return "Ready"
	case runpb.Condition_CONDITION_FAILED:
		return "Failed"
	case runpb.Condition_CONDITION_RECONCILING:
		return "Deploying"
	default:
		return "Unknown"
	}
}

// getService fetches a single Cloud Run service by fully-qualified name.
func getService(ctx context.Context, name string) (*runpb.Service, error) {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloudrun: credentials: %w", err)
	}
	client, err := run.NewServicesClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("cloudrun: new client: %w", err)
	}
	defer client.Close()
	return client.GetService(ctx, &runpb.GetServiceRequest{Name: name})
}

// DescribeText returns a human-readable JSON description of a Cloud Run service,
// equivalent to: gcloud run services describe <name>
func DescribeText(ctx context.Context, name string) (string, error) {
	svc, err := getService(ctx, name)
	if err != nil {
		return "", err
	}
	b, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(svc)
	if err != nil {
		return "", fmt.Errorf("cloudrun: marshal: %w", err)
	}
	return string(b), nil
}

// DescribeYAML returns a YAML description of a Cloud Run service,
// equivalent to: gcloud run services describe <name> --format=yaml
func DescribeYAML(ctx context.Context, name string) (string, error) {
	svc, err := getService(ctx, name)
	if err != nil {
		return "", err
	}
	jsonBytes, err := protojson.MarshalOptions{}.Marshal(svc)
	if err != nil {
		return "", fmt.Errorf("cloudrun: marshal json: %w", err)
	}
	var m interface{}
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return "", fmt.Errorf("cloudrun: unmarshal json: %w", err)
	}
	yamlBytes, err := yaml.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("cloudrun: marshal yaml: %w", err)
	}
	return string(yamlBytes), nil
}

// UpdateServiceFromYAML parses the given YAML representation of a Cloud Run
// service and applies it via the Services API. The service name embedded in
// the YAML is used to identify the target service.
//
// Returns as soon as the UpdateService request is accepted — it does NOT
// wait for the long-running deploy to finish. Callers that need the final
// outcome should observe the next poll tick.
func UpdateServiceFromYAML(ctx context.Context, yamlStr string) error {
	var m interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &m); err != nil {
		return fmt.Errorf("cloudrun: parse yaml: %w", err)
	}
	jsonBytes, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("cloudrun: marshal json: %w", err)
	}
	svc := &runpb.Service{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(jsonBytes, svc); err != nil {
		return fmt.Errorf("cloudrun: unmarshal proto: %w", err)
	}
	if svc.Name == "" {
		return fmt.Errorf("cloudrun: service name missing from yaml")
	}

	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return fmt.Errorf("cloudrun: credentials: %w", err)
	}
	client, err := run.NewServicesClient(ctx, opts...)
	if err != nil {
		return fmt.Errorf("cloudrun: new client: %w", err)
	}

	_, err = client.UpdateService(ctx, &runpb.UpdateServiceRequest{Service: svc})
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("cloudrun: update: %w", err)
	}
	// Don't wait for the LRO — Cloud Run deploys can take minutes. Polling
	// loop will reflect new state on next tick. Close client in goroutine.
	go func() {
		_ = client.Close()
	}()
	return nil
}
