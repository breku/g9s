package dao

import (
	"context"
	"fmt"
	"strings"

	cloudbuild "cloud.google.com/go/cloudbuild/apiv1/v2"
	"cloud.google.com/go/cloudbuild/apiv1/v2/cloudbuildpb"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
)

// Ensure CloudBuild satisfies Accessor at compile time.
var _ Accessor = (*CloudBuild)(nil)

// CloudBuild is the DAO for Cloud Build triggers.
type CloudBuild struct{}

// Resource returns the stable identifier for this resource type.
func (c *CloudBuild) Resource() string { return "cloudbuild" }

// Header returns the column headers for the Cloud Build triggers table.
func (c *CloudBuild) Header() []string {
	return []string{"NAME", "DESCRIPTION", "STATUS", "EVENT", "REPOSITORY", "CREATED"}
}

// List fetches all Cloud Build triggers in the given project (global location).
func (c *CloudBuild) List(ctx context.Context, project string) (*TableData, error) {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloudbuild: credentials: %w", err)
	}

	client, err := cloudbuild.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("cloudbuild: new client: %w", err)
	}
	defer client.Close()

	req := &cloudbuildpb.ListBuildTriggersRequest{
		Parent: fmt.Sprintf("projects/%s/locations/global", project),
	}

	var rows []Row
	it := client.ListBuildTriggers(ctx, req)
	for {
		trigger, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cloudbuild: list: %w", err)
		}
		rows = append(rows, rowFromTrigger(trigger))
	}

	return &TableData{
		Header: c.Header(),
		Rows:   rows,
	}, nil
}

// rowFromTrigger converts a BuildTrigger proto to a table Row.
func rowFromTrigger(t *cloudbuildpb.BuildTrigger) Row {
	name := t.Name
	if name == "" {
		name = t.Id
	}

	status := "Enabled"
	if t.Disabled {
		status = "Disabled"
	}

	colType := RowTypeNotActive
	if !t.Disabled {
		colType = RowTypeActive
	}

	event := triggerEvent(t)
	repo := triggerRepo(t)
	created := "—"
	if t.CreateTime != nil {
		created = formatTime(t.CreateTime.AsTime())
	}

	desc := t.Description
	if len(desc) > 60 {
		desc = desc[:57] + "..."
	}

	return Row{
		ID:   t.ResourceName,
		Type: colType,
		Columns: []Column{
			{Text: name},
			{Text: desc},
			{Text: status},
			{Text: event},
			{Text: repo},
			{Text: created},
		},
	}
}

// triggerEvent determines the event source type of the trigger.
func triggerEvent(t *cloudbuildpb.BuildTrigger) string {
	if t.WebhookConfig != nil {
		return "Webhook"
	}
	if t.PubsubConfig != nil {
		return "Pub/Sub"
	}
	if t.Github != nil {
		if t.Github.GetPush() != nil {
			return "Push"
		}
		if t.Github.GetPullRequest() != nil {
			return "Pull Request"
		}
		return "GitHub"
	}
	if t.TriggerTemplate != nil {
		return "Push (CSR)"
	}
	if t.RepositoryEventConfig != nil {
		return repoEventType(t.RepositoryEventConfig)
	}
	return "Manual"
}

// repoEventType extracts the event type from RepositoryEventConfig.
func repoEventType(cfg *cloudbuildpb.RepositoryEventConfig) string {
	if cfg.GetPush() != nil {
		return "Push"
	}
	if cfg.GetPullRequest() != nil {
		return "Pull Request"
	}
	return "Repo Event"
}

// triggerRepo extracts the repository name from the trigger config.
func triggerRepo(t *cloudbuildpb.BuildTrigger) string {
	if t.Github != nil {
		owner := t.Github.Owner
		name := t.Github.Name
		if owner != "" && name != "" {
			return owner + "/" + name
		}
		if name != "" {
			return name
		}
	}
	if t.TriggerTemplate != nil && t.TriggerTemplate.RepoName != "" {
		return t.TriggerTemplate.RepoName
	}
	if t.RepositoryEventConfig != nil && t.RepositoryEventConfig.Repository != "" {
		// Format: projects/P/locations/L/connections/C/repositories/R
		parts := strings.Split(t.RepositoryEventConfig.Repository, "/")
		if len(parts) >= 2 {
			return parts[len(parts)-1]
		}
		return t.RepositoryEventConfig.Repository
	}
	if t.SourceToBuild != nil && t.SourceToBuild.Uri != "" {
		return t.SourceToBuild.Uri
	}
	return "—"
}
