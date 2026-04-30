// Package cloudbuild provides the DAO for Cloud Build triggers.
package cloudbuild

import (
	"context"
	"fmt"
	"strings"

	cloudbuild "cloud.google.com/go/cloudbuild/apiv1/v2"
	"cloud.google.com/go/cloudbuild/apiv1/v2/cloudbuildpb"
	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
)

// Ensure CloudBuild satisfies Accessor at compile time.
var _ dao.Accessor = (*CloudBuild)(nil)

// CloudBuild is the DAO for Cloud Build triggers.
type CloudBuild struct{}

// Resource returns the stable identifier for this resource type.
func (c *CloudBuild) Resource() string { return "cloudbuild" }

// Header returns the column headers for the Cloud Build triggers table.
func (c *CloudBuild) Header() []string {
	return []string{"NAME", "DESCRIPTION", "STATUS", "EVENT", "REPOSITORY", "CREATED"}
}

// List fetches all Cloud Build triggers in the given project (global location).
func (c *CloudBuild) List(ctx context.Context, project string) (*dao.TableData, error) {
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

	var rows []dao.Row
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

	return &dao.TableData{
		Header: c.Header(),
		Rows:   rows,
	}, nil
}

// rowFromTrigger converts a BuildTrigger proto to a table Row.
func rowFromTrigger(t *cloudbuildpb.BuildTrigger) dao.Row {
	name := t.Name
	if name == "" {
		name = t.Id
	}

	status := "Enabled"
	if t.Disabled {
		status = "Disabled"
	}

	colType := dao.RowTypeNotActive
	if !t.Disabled {
		colType = dao.RowTypeActive
	}

	event := triggerEvent(t)
	repo := triggerRepo(t)
	created := "—"
	if t.CreateTime != nil {
		created = dao.FormatTime(t.CreateTime.AsTime())
	}

	desc := t.Description
	if len(desc) > 60 {
		desc = desc[:57] + "..."
	}

	return dao.Row{
		ID:   t.ResourceName,
		Type: colType,
		Meta: map[string]string{
			"triggerId": t.Id,
			"project":   projectFromResourceName(t.ResourceName),
			"branch":    triggerBranch(t),
		},
		Columns: []dao.Column{
			{Text: name},
			{Text: desc},
			{Text: status},
			{Text: event},
			{Text: repo},
			{Text: created},
		},
	}
}

// triggerBranch extracts the configured branch from a trigger.
func triggerBranch(t *cloudbuildpb.BuildTrigger) string {
	if t.TriggerTemplate != nil && t.TriggerTemplate.GetBranchName() != "" {
		return t.TriggerTemplate.GetBranchName()
	}
	if t.SourceToBuild != nil && t.SourceToBuild.Ref != "" {
		ref := t.SourceToBuild.Ref
		if after, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
			return after
		}
		return ref
	}
	if t.Github != nil {
		if push := t.Github.GetPush(); push != nil && push.GetBranch() != "" {
			branch := push.GetBranch()
			branch = strings.TrimPrefix(branch, "^")
			branch = strings.TrimSuffix(branch, "$")
			return branch
		}
	}
	return ""
}

// projectFromResourceName extracts the project ID from a GCP resource name.
// Format: projects/<project>/...
func projectFromResourceName(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// RunTrigger triggers a Cloud Build run for the given trigger ID and branch.
func RunTrigger(ctx context.Context, project, triggerID, branch string) error {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return fmt.Errorf("cloudbuild: credentials: %w", err)
	}
	client, err := cloudbuild.NewClient(ctx, opts...)
	if err != nil {
		return fmt.Errorf("cloudbuild: new client: %w", err)
	}
	defer client.Close()

	req := &cloudbuildpb.RunBuildTriggerRequest{
		ProjectId: project,
		TriggerId: triggerID,
		Source: &cloudbuildpb.RepoSource{
			Revision: &cloudbuildpb.RepoSource_BranchName{
				BranchName: branch,
			},
		},
	}
	if _, err := client.RunBuildTrigger(ctx, req); err != nil {
		return fmt.Errorf("cloudbuild: run trigger: %w", err)
	}
	return nil
}

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

func repoEventType(cfg *cloudbuildpb.RepositoryEventConfig) string {
	if cfg.GetPush() != nil {
		return "Push"
	}
	if cfg.GetPullRequest() != nil {
		return "Pull Request"
	}
	return "Repo Event"
}

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
