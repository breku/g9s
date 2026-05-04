// Package buildhistory provides the DAO for Cloud Build build executions.
package buildhistory

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"cloud.google.com/go/cloudbuild/apiv1/v2/cloudbuildpb"
	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/gcp"
	"github.com/rs/zerolog/log"
	"google.golang.org/api/iterator"
)

// CancelBuild cancels an in-progress build by project and build ID.
func (b *BuildHistory) CancelBuild(ctx context.Context, project, buildID string) error {
	client, err := gcp.CloudBuildClient()
	if err != nil {
		return fmt.Errorf("buildhistory: client: %w", err)
	}

	_, err = client.CancelBuild(ctx, &cloudbuildpb.CancelBuildRequest{
		ProjectId: project,
		Id:        buildID,
	})
	if err != nil {
		return fmt.Errorf("buildhistory: cancel: %w", err)
	}
	return nil
}

// Ensure BuildHistory satisfies Accessor and Paginator, and BuildRow satisfies Row.
var (
	_ dao.Accessor  = (*BuildHistory)(nil)
	_ dao.Paginator = (*BuildHistory)(nil)
	_ dao.Row       = (*BuildRow)(nil)
)

// PageSize is the number of builds fetched per page. Exported so the UI
// layer can pass it to the paginated ResourceTable constructor.
const PageSize = 10

// BuildHistory is the DAO for Cloud Build build executions (history).
type BuildHistory struct{}

// BuildRow is the typed row for a Cloud Build execution. Fields exposed here
// support the cancel + log overlays without per-action map lookups.
type BuildRow struct {
	id      string
	rowType dao.RowType
	columns []dao.Column

	BuildID     string
	LogsBucket  string
	LogURL      string
	Status      string
	Project     string
	LoggingMode string
	CreateTime  string
}

// GetID implements dao.Row.
func (r *BuildRow) GetID() string { return r.id }

// GetType implements dao.Row.
func (r *BuildRow) GetType() dao.RowType { return r.rowType }

// GetColumns implements dao.Row.
func (r *BuildRow) GetColumns() []dao.Column { return r.columns }

// SetStatusColumn updates the STATUS column text in place. Used by the UI to
// render optimistic state ("Cancelling…") between user action and next poll.
func (r *BuildRow) SetStatusColumn(text string) {
	const statusCol = 2 // ID, TRIGGER, STATUS
	if statusCol < len(r.columns) {
		r.columns[statusCol].Text = text
	}
}

// CopyColumnValue copies the full build ID — useful for `gcloud builds describe`.
func (r *BuildRow) CopyColumnValue() (string, bool) {
	if r.BuildID == "" {
		return "", false
	}
	return r.BuildID, true
}

// Resource returns the stable identifier for this resource type.
func (b *BuildHistory) Resource() string { return "buildhistory" }

// Header returns the column headers for the build history table.
func (b *BuildHistory) Header() []string {
	return []string{"ID", "TRIGGER", "STATUS", "STARTED", "DURATION", "BRANCH", "IMAGES"}
}

// List fetches the first page of builds (10 most recent).
func (b *BuildHistory) List(ctx context.Context, project string) (*dao.TableData, error) {
	return b.NextPage(ctx, project, "", PageSize)
}

// NextPage fetches a page of builds using the given page token.
// An empty pageToken fetches the first page.
func (b *BuildHistory) NextPage(ctx context.Context, project, pageToken string, pageSize int) (*dao.TableData, error) {
	client, err := gcp.CloudBuildClient()
	if err != nil {
		return nil, fmt.Errorf("buildhistory: client: %w", err)
	}

	req := &cloudbuildpb.ListBuildsRequest{
		ProjectId: project,
		PageSize:  int32(pageSize),
		PageToken: pageToken,
	}

	var rows []dao.Row
	it := client.ListBuilds(ctx, req)
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

	nextToken := it.PageInfo().Token

	log.Debug().
		Str("resource", "buildhistory").
		Str("project", project).
		Int("rows", len(rows)).
		Str("nextToken", nextToken).
		Msg("page fetched")

	return &dao.TableData{
		Header:        b.Header(),
		Rows:          rows,
		NextPageToken: nextToken,
	}, nil
}

func rowFromBuild(b *cloudbuildpb.Build) *BuildRow {
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
	if name, ok := b.Substitutions["TRIGGER_NAME"]; ok && name != "" {
		trigger = name
	}

	status := buildStatus(b.Status)

	rowType := dao.RowTypeNotActive
	switch status {
	case "Working", "Queued", "Pending":
		rowType = dao.RowTypeActive
	case "Failure", "Internal Error", "Expired", "Timeout":
		rowType = dao.RowTypeError
	}

	started := "—"
	if b.StartTime != nil {
		started = dao.FormatTime(b.StartTime.AsTime())
	} else if b.CreateTime != nil {
		started = dao.FormatTime(b.CreateTime.AsTime())
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

	return &BuildRow{
		id:      b.Name,
		rowType: rowType,
		columns: []dao.Column{
			{Text: id},
			{Text: trigger},
			{Text: status},
			{Text: started},
			{Text: duration},
			{Text: branch},
			{Text: images},
		},
		BuildID:     b.Id,
		LogsBucket:  strings.TrimPrefix(b.LogsBucket, "gs://"),
		LogURL:      b.LogUrl,
		Status:      status,
		Project:     b.ProjectId,
		LoggingMode: loggingMode,
		CreateTime:  createTime,
	}
}

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
	if br, ok := b.Substitutions["BRANCH_NAME"]; ok && br != "" {
		return br
	}
	if tag, ok := b.Substitutions["TAG_NAME"]; ok && tag != "" {
		return "tag:" + tag
	}
	return "—"
}

func buildImages(b *cloudbuildpb.Build) string {
	if len(b.Images) == 0 {
		return "—"
	}
	short := make([]string, len(b.Images))
	for i, img := range b.Images {
		parts := strings.Split(img, "/")
		short[i] = parts[len(parts)-1]
	}
	result := strings.Join(short, ", ")
	if len(result) > 50 {
		return result[:47] + "..."
	}
	return result
}
