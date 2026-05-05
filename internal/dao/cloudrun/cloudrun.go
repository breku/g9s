// Package cloudrun provides the DAO for Cloud Run v2 services.
package cloudrun

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/run/apiv2/runpb"
	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/encoding/protojson"
)

// Ensure CloudRun satisfies Accessor and ServiceRow satisfies Row at compile time.
var (
	_ dao.Accessor      = (*CloudRun)(nil)
	_ dao.YAMLDescriber = (*CloudRun)(nil)
	_ dao.Row           = (*ServiceRow)(nil)
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

// FetchPage implements dao.Accessor. Fetches one page of Cloud Run services
// across all regions in the given project. The wildcard "locations/-"
// parent is accepted by the v2 API for ListServices despite what the proto
// docs imply. An empty pageToken requests the first page.
func (c *CloudRun) FetchPage(ctx context.Context, project, pageToken string, pageSize int) (*dao.TableData, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	client, err := gcp.RunServicesClient()
	if err != nil {
		return nil, fmt.Errorf("cloudrun: client: %w", err)
	}

	req := &runpb.ListServicesRequest{
		Parent:    fmt.Sprintf("projects/%s/locations/-", project),
		PageSize:  int32(pageSize),
		PageToken: pageToken,
	}

	var rows []dao.Row
	it := client.ListServices(ctx, req)
	for i := 0; i < pageSize; i++ {
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
		Header:        c.Header(),
		Rows:          rows,
		NextPageToken: it.PageInfo().Token,
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
	client, err := gcp.RunServicesClient()
	if err != nil {
		return nil, fmt.Errorf("cloudrun: client: %w", err)
	}
	return client.GetService(ctx, &runpb.GetServiceRequest{Name: name})
}

// DescribeYAML implements dao.YAMLDescriber. Returns the YAML rendering of a
// Cloud Run service, equivalent to: gcloud run services describe <name> --format=yaml
func (c *CloudRun) DescribeYAML(ctx context.Context, id string) (string, error) {
	svc, err := getService(ctx, id)
	if err != nil {
		return "", err
	}
	jsonBytes, err := protojson.MarshalOptions{}.Marshal(svc)
	if err != nil {
		return "", fmt.Errorf("cloudrun: marshal json: %w", err)
	}
	out, err := dao.JSONToYAML(jsonBytes)
	if err != nil {
		return "", fmt.Errorf("cloudrun: %w", err)
	}
	return out, nil
}

// UpdateServiceFromYAML parses the given YAML representation of a Cloud Run
// service and submits an UpdateService request. Returns a wait function the
// caller invokes (typically in a goroutine via app.TrackOp) to block on the
// long-running deploy and surface its final outcome.
//
// The wait function takes ownership of closing the underlying API client on
// completion, so the caller MUST invoke it exactly once. Returning the
// initial submission error and the wait separately lets the UI report a
// fast "Deploying foo… (running)" the moment the request is accepted, then
// flip to "Deploy succeeded" / "Deploy failed: <err>" minutes later when
// the LRO finishes.
//
// The service name embedded in the YAML identifies the target service.
func (c *CloudRun) UpdateServiceFromYAML(ctx context.Context, yamlStr string) (wait func(context.Context) error, err error) {
	jsonBytes, err := dao.YAMLToJSON(yamlStr)
	if err != nil {
		return nil, fmt.Errorf("cloudrun: %w", err)
	}
	svc := &runpb.Service{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(jsonBytes, svc); err != nil {
		return nil, fmt.Errorf("cloudrun: unmarshal proto: %w", err)
	}
	if svc.Name == "" {
		return nil, fmt.Errorf("cloudrun: service name missing from yaml")
	}

	client, err := gcp.RunServicesClient()
	if err != nil {
		return nil, fmt.Errorf("cloudrun: client: %w", err)
	}

	op, err := client.UpdateService(ctx, &runpb.UpdateServiceRequest{Service: svc})
	if err != nil {
		return nil, fmt.Errorf("cloudrun: update: %w", err)
	}

	wait = func(waitCtx context.Context) error {
		if _, err := op.Wait(waitCtx); err != nil {
			return fmt.Errorf("cloudrun: deploy: %w", err)
		}
		return nil
	}
	return wait, nil
}
