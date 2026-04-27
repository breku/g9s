package dao

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/yaml.v3"
)

// Ensure CloudRun satisfies Accessor at compile time.
var _ Accessor = (*CloudRun)(nil)

// CloudRun is the DAO for Cloud Run v2 Services.
type CloudRun struct{}

// Resource returns the stable identifier for this resource type.
func (c *CloudRun) Resource() string { return "cloudrun" }

// Header returns the column headers for the Cloud Run table view.
func (c *CloudRun) Header() []string {
	return []string{"NAME", "REGION", "STATUS", "URL", "LAST DEPLOYED"}
}

// List fetches all Cloud Run services across all regions in the given project.
func (c *CloudRun) List(ctx context.Context, project string) (*TableData, error) {
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

	var rows []Row
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

	return &TableData{
		Header: c.Header(),
		Rows:   rows,
	}, nil
}

// rowFromService converts a Cloud Run Service proto to a table Row.
func rowFromService(svc *runpb.Service) Row {
	name := lastSegment(svc.Name)
	region := locationFromName(svc.Name)
	status := conditionState(svc.TerminalCondition)
	url := svc.Uri
	deployed := formatTime(svc.UpdateTime.AsTime())

	colType := RowTypeNotActive
	if status == "Ready" {
		colType = RowTypeActive
	} else if status == "Failed" {
		colType = RowTypeError
	}

	return Row{
		ID:   svc.Name,
		Type: colType,
		Columns: []Column{
			{Text: name},
			{Text: region},
			{Text: status},
			{Text: url},
			{Text: deployed},
		},
	}
}

// locationFromName extracts the location/region from a fully-qualified name.
// Format: projects/{p}/locations/{location}/services/{service}
func locationFromName(name string) string {
	parts := strings.Split(name, "/")
	// index 3 is the location value
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
	// proto → compact JSON → map → YAML
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
