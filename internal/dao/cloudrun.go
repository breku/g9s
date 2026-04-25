package dao

import (
	"context"
	"fmt"
	"strings"
	"time"

	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
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
	// Name is "projects/P/locations/REGION/services/NAME" — extract the short name.
	name := lastSegment(svc.Name)
	region := locationFromName(svc.Name)
	status := conditionState(svc.TerminalCondition)
	url := svc.Uri
	deployed := formatTime(svc.UpdateTime.AsTime())

	return Row{
		ID:      svc.Name,
		Columns: []string{name, region, status, url, deployed},
	}
}

// lastSegment returns the last "/" segment of a resource name.
func lastSegment(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return name
	}
	return parts[len(parts)-1]
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

// formatTime formats a time.Time as "2006-01-02 15:04" or "—" if zero.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2006-01-02 15:04")
}
