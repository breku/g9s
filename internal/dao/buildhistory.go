package dao

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	cloudbuild "cloud.google.com/go/cloudbuild/apiv1/v2"
	"cloud.google.com/go/cloudbuild/apiv1/v2/cloudbuildpb"
	"github.com/brekol/g9s/internal/gcp"
	"github.com/rs/zerolog/log"
	"google.golang.org/api/iterator"
)

// GetBuild fetches a single build by project and build ID.
// Returns the build proto with the most up-to-date fields, including
// LogsBucket which may be empty on very early-stage builds.
func GetBuild(ctx context.Context, project, buildID string) (*cloudbuildpb.Build, error) {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("buildhistory: credentials: %w", err)
	}
	client, err := cloudbuild.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("buildhistory: new client: %w", err)
	}
	defer client.Close()

	return client.GetBuild(ctx, &cloudbuildpb.GetBuildRequest{
		ProjectId: project,
		Id:        buildID,
	})
}

// LogsBucketForBuild returns the GCS bucket name (without gs:// prefix) for
// a build's logs. It uses the build's LogsBucket field when set, and falls
// back to deriving the default bucket from the LogUrl (which encodes the
// project number) for builds that are still starting up.
func LogsBucketForBuild(b *cloudbuildpb.Build) string {
	if b.LogsBucket != "" {
		return strings.TrimPrefix(b.LogsBucket, "gs://")
	}
	// LogUrl format: https://console.cloud.google.com/cloud-build/builds/<id>?project=<projectNumber>
	// Default bucket: <projectNumber>.cloudbuild-logs.googleusercontent.com
	if b.LogUrl != "" {
		if idx := strings.Index(b.LogUrl, "?project="); idx != -1 {
			projectNumber := b.LogUrl[idx+len("?project="):]
			if projectNumber != "" {
				return projectNumber + ".cloudbuild-logs.googleusercontent.com"
			}
		}
	}
	return ""
}

// Ensure BuildHistory satisfies Accessor and Paginator at compile time.
var (
	_ Accessor  = (*BuildHistory)(nil)
	_ Paginator = (*BuildHistory)(nil)
)

const buildHistoryPageSize = 10

// BuildHistory is the DAO for Cloud Build build executions (history).
type BuildHistory struct{}

// Resource returns the stable identifier for this resource type.
func (b *BuildHistory) Resource() string { return "buildhistory" }

// Header returns the column headers for the build history table.
func (b *BuildHistory) Header() []string {
	return []string{"ID", "TRIGGER", "STATUS", "STARTED", "DURATION", "BRANCH", "IMAGES"}
}

// List fetches the first page of builds (10 most recent).
func (b *BuildHistory) List(ctx context.Context, project string) (*TableData, error) {
	return b.NextPage(ctx, project, "", buildHistoryPageSize)
}

// NextPage fetches a page of builds using the given page token.
// An empty pageToken fetches the first page.
func (b *BuildHistory) NextPage(ctx context.Context, project, pageToken string, pageSize int) (*TableData, error) {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("buildhistory: credentials: %w", err)
	}

	client, err := cloudbuild.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("buildhistory: new client: %w", err)
	}
	defer client.Close()

	req := &cloudbuildpb.ListBuildsRequest{
		ProjectId: project,
		PageSize:  int32(pageSize),
		PageToken: pageToken,
	}

	var rows []Row
	it := client.ListBuilds(ctx, req)
	// Fetch exactly one page — call it.Next() up to pageSize times, then stop.
	for i := 0; i < pageSize; i++ {
		build, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("buildhistory: list: %w", err)
		}
		rows = append(rows, rowFromBuild(build))
	}

	// Peek at the next page token from the iterator's internal pager.
	nextToken := it.PageInfo().Token

	log.Debug().
		Str("resource", "buildhistory").
		Str("project", project).
		Int("rows", len(rows)).
		Str("nextToken", nextToken).
		Msg("page fetched")

	return &TableData{
		Header:        b.Header(),
		Rows:          rows,
		NextPageToken: nextToken,
	}, nil
}

// rowFromBuild converts a Build proto to a table Row.
func rowFromBuild(b *cloudbuildpb.Build) Row {
	id := b.Id
	if len(id) > 8 {
		id = id[:8]
	}

	trigger := b.BuildTriggerId
	if trigger == "" {
		trigger = "manual"
	} else if len(trigger) > 8 {
		trigger = trigger[:8]
	}
	// Use substitution _TRIGGER_NAME if available.
	if name, ok := b.Substitutions["TRIGGER_NAME"]; ok && name != "" {
		trigger = name
	}

	status := buildStatus(b.Status)
	started := "—"
	if b.StartTime != nil {
		started = formatTime(b.StartTime.AsTime())
	} else if b.CreateTime != nil {
		started = formatTime(b.CreateTime.AsTime())
	}

	duration := buildDuration(b)
	branch := buildBranch(b)
	images := buildImages(b)

	loggingMode := "LEGACY"
	if b.Options != nil {
		loggingMode = b.Options.GetLogging().String()
	}

	createTime := ""
	if b.CreateTime != nil {
		createTime = b.CreateTime.AsTime().UTC().Format(time.RFC3339)
	}

	return Row{
		ID:      b.Name,
		Columns: []string{id, trigger, status, started, duration, branch, images},
		Meta: map[string]string{
			"buildId":     b.Id,
			"logsBucket":  strings.TrimPrefix(b.LogsBucket, "gs://"),
			"logUrl":      b.LogUrl,
			"status":      status,
			"project":     b.ProjectId,
			"loggingMode": loggingMode,
			"createTime":  createTime,
		},
	}
}

// buildStatus maps Build_Status to a human-readable string.
func buildStatus(s cloudbuildpb.Build_Status) string {
	switch s {
	case cloudbuildpb.Build_SUCCESS:
		return "Success"
	case cloudbuildpb.Build_FAILURE:
		return "Failure"
	case cloudbuildpb.Build_WORKING:
		return "Working"
	case cloudbuildpb.Build_QUEUED:
		return "Queued"
	case cloudbuildpb.Build_PENDING:
		return "Pending"
	case cloudbuildpb.Build_CANCELLED:
		return "Cancelled"
	case cloudbuildpb.Build_TIMEOUT:
		return "Timeout"
	case cloudbuildpb.Build_INTERNAL_ERROR:
		return "Internal Error"
	case cloudbuildpb.Build_EXPIRED:
		return "Expired"
	default:
		return "Unknown"
	}
}

// buildDuration computes a human-readable duration from start to finish.
func buildDuration(b *cloudbuildpb.Build) string {
	if b.StartTime == nil {
		return "—"
	}
	end := time.Now()
	if b.FinishTime != nil {
		end = b.FinishTime.AsTime()
	}
	d := end.Sub(b.StartTime.AsTime())
	if d < 0 {
		return "—"
	}
	totalSec := int(math.Round(d.Seconds()))
	if totalSec < 60 {
		return fmt.Sprintf("%ds", totalSec)
	}
	m := totalSec / 60
	s := totalSec % 60
	if m < 60 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := m / 60
	m = m % 60
	return fmt.Sprintf("%dh%dm", h, m)
}

// buildBranch extracts the branch name from the build source.
func buildBranch(b *cloudbuildpb.Build) string {
	if b.Source != nil {
		if rs := b.Source.GetRepoSource(); rs != nil {
			if br := rs.GetBranchName(); br != "" {
				return br
			}
			if tag := rs.GetTagName(); tag != "" {
				return "tag:" + tag
			}
		}
	}
	// Fallback: check substitutions.
	if br, ok := b.Substitutions["BRANCH_NAME"]; ok && br != "" {
		return br
	}
	if tag, ok := b.Substitutions["TAG_NAME"]; ok && tag != "" {
		return "tag:" + tag
	}
	return "—"
}

// buildImages returns a comma-separated list of image names (short form).
func buildImages(b *cloudbuildpb.Build) string {
	if len(b.Images) == 0 {
		return "—"
	}
	short := make([]string, len(b.Images))
	for i, img := range b.Images {
		// Take just the image name after the last '/'.
		parts := strings.Split(img, "/")
		short[i] = parts[len(parts)-1]
	}
	result := strings.Join(short, ", ")
	if len(result) > 50 {
		return result[:47] + "..."
	}
	return result
}
