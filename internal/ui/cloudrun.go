package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/atotto/clipboard"
	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/dao/cloudrun"
	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// CloudRunView is the tview page for Cloud Run services.
type CloudRunView struct {
	*ResourceTable

	app     *App
	project string
	mdl     *model.Table
}

// Ensure interfaces are satisfied at compile time.
var (
	_ ResourceView = (*CloudRunView)(nil)
	_ KeyHandler   = (*CloudRunView)(nil)
	_ HintProvider = (*CloudRunView)(nil)
)

// NewCloudRunView creates a CloudRunView for the given project.
func NewCloudRunView(a *App, project string) *CloudRunView {
	v := &CloudRunView{
		ResourceTable: NewResourceTable("Cloud Run"),
		app:           a,
		project:       project,
		mdl:           model.NewTable("cloudrun", project),
	}
	v.mdl.AddListener(v)
	return v
}

// Primitive implements ResourceView.
func (v *CloudRunView) Primitive() tview.Primitive { return v.Table }

// Watch implements ResourceView.
func (v *CloudRunView) Watch(ctx context.Context) error { return v.mdl.Watch(ctx) }

// RenderLoading implements ResourceView.
func (v *CloudRunView) RenderLoading() {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(" Loading Cloud Run services… ").
		SetSelectable(false))
}

// SetFilter implements Filterable.
func (v *CloudRunView) SetFilter(f string) {
	v.ResourceTable.SetFilter(f)
}

// Hints implements HintProvider.
func (v *CloudRunView) Hints() []Hint {
	return []Hint{
		{Key: "d", Desc: "Describe"},
		{Key: "y", Desc: "YAML"},
		{Key: "e", Desc: "Edit"},
		{Key: "l", Desc: "Logs"},
		{Key: "c", Desc: "Copy URL"},
	}
}

// HandleKey implements KeyHandler.
func (v *CloudRunView) HandleKey(event *tcell.EventKey) bool {
	switch event.Rune() {
	case 'd':
		return v.openDescribe(false)
	case 'y':
		return v.openDescribe(true)
	case 'e':
		return v.editService()
	case 'l':
		return v.openLogs()
	case 'c':
		return v.copyURL()
	}
	return false
}

// editService suspends the TUI, opens $EDITOR (default: vim) on the selected
// service's YAML, and applies the changes when the editor exits cleanly.
// On API or parse error, an error overlay is pushed when the TUI resumes.
func (v *CloudRunView) editService() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	name := row.GetID()
	short := lastSegmentUI(name)

	// Fetch current YAML before suspending so any error is visible in the TUI.
	yamlStr, err := cloudrun.DescribeYAML(v.app.ctx, name)
	if err != nil {
		log.Error().Err(err).Str("service", name).Msg("cloudrun: fetch for edit failed")
		v.showEditError(short, err)
		return true
	}

	// Run the editor + API call inside Suspend so tcell releases the terminal.
	var editErr error
	ok := v.app.tview.Suspend(func() {
		editErr = runEditorAndUpdate(v.app.ctx, short, yamlStr)
	})
	if !ok {
		log.Warn().Msg("cloudrun: tview.Suspend returned false (already suspended)")
		return true
	}
	if editErr != nil {
		log.Error().Err(editErr).Str("service", name).Msg("cloudrun: edit failed")
		v.showEditError(short, editErr)
		return true
	}
	log.Info().Str("service", name).Msg("cloudrun: service updated")
	return true
}

// showEditError displays the error in a DescribeView overlay so the user can
// scroll through long messages (e.g. validation errors from the API).
func (v *CloudRunView) showEditError(shortName string, err error) {
	msg := err.Error()
	dv := NewDescribeView(v.app, fmt.Sprintf("Edit failed – %s", shortName),
		func(context.Context) (string, error) {
			return "[red]" + msg + "[white]", nil
		})
	v.app.PushOverlay(dv)
}

// runEditorAndUpdate writes the YAML to a temp file, opens $EDITOR on it,
// and on clean exit applies the resulting YAML via the Cloud Run API.
// If the file is unchanged, the API call is skipped.
func runEditorAndUpdate(ctx context.Context, shortName, original string) error {
	tmp, err := os.CreateTemp("", "g9s-cloudrun-"+shortName+"-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(original); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("read edited file: %w", err)
	}
	if string(edited) == original {
		// Nothing changed — skip the API call.
		return nil
	}

	return cloudrun.UpdateServiceFromYAML(ctx, string(edited))
}

// copyURL copies the selected service's URL to the system clipboard.
func (v *CloudRunView) copyURL() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	url, ok := row.CopyColumnValue()
	if !ok {
		return true
	}
	if err := clipboard.WriteAll(url); err != nil {
		log.Error().Err(err).Str("url", url).Msg("cloudrun: copy URL failed")
	}
	return true
}

// openDescribe pushes a DescribeView overlay for the selected service.
// yaml=true renders YAML format; yaml=false renders human-readable JSON.
func (v *CloudRunView) openDescribe(asYAML bool) bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	name := row.GetID()
	format := "Describe"
	if asYAML {
		format = "YAML"
	}
	title := fmt.Sprintf("%s – %s", format, lastSegmentUI(name))

	var fetchFn func(ctx context.Context) (string, error)
	if asYAML {
		fetchFn = func(ctx context.Context) (string, error) {
			return cloudrun.DescribeYAML(ctx, name)
		}
	} else {
		fetchFn = func(ctx context.Context) (string, error) {
			return cloudrun.DescribeText(ctx, name)
		}
	}

	dv := NewDescribeView(v.app, title, fetchFn)
	v.app.PushOverlay(dv)
	return true
}

// openLogs pushes a LogView overlay streaming all Cloud Run logs for the selected service.
func (v *CloudRunView) openLogs() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	// row ID format: projects/<project>/locations/<location>/services/<name>
	id := row.GetID()
	svcName := lastSegmentUI(id)
	project := projectFromResourceName(id)
	region := regionFromResourceName(id)

	filter := fmt.Sprintf(`resource.type="cloud_run_revision" AND resource.labels.service_name="%s"`, svcName)
	if region != "" {
		filter += fmt.Sprintf(` AND resource.labels.location="%s"`, region)
	}

	cfg := LogViewConfig{
		Title:     fmt.Sprintf("Logs – %s", svcName),
		Streaming: true,
		Project:   project,
		LogFilter: filter,
		// 24h window mirroring gcloud default; desc fetch makes it fast.
		LogSince:    time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
		LogPageSize: 200,
	}
	lv := NewLogViewFromConfig(v.app, cfg)
	v.app.PushOverlay(lv)
	return true
}

// TableDataChanged implements model.TableListener.
func (v *CloudRunView) TableDataChanged(data *dao.TableData) {
	v.app.tview.QueueUpdateDraw(func() {
		v.Render(data)
	})
}

// TableLoadFailed implements model.TableListener.
func (v *CloudRunView) TableLoadFailed(err error) {
	v.app.tview.QueueUpdateDraw(func() {
		v.renderError(err)
	})
}

// renderError clears the table and shows the error message.
func (v *CloudRunView) renderError(err error) {
	v.Clear()
	v.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf(" Error: %v ", err)).
		SetSelectable(false))
}

// lastSegmentUI extracts the last path segment of a resource name for display.
func lastSegmentUI(name string) string {
	parts := splitSlash(name)
	if len(parts) == 0 {
		return name
	}
	return parts[len(parts)-1]
}

func splitSlash(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// projectFromResourceName extracts the project ID from a GCP resource name.
// Format: projects/<project>/...
func projectFromResourceName(name string) string {
	parts := splitSlash(name)
	// index 0 = "projects", index 1 = project ID
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// regionFromResourceName extracts the location/region from a GCP resource name.
// Format: projects/<project>/locations/<location>/...
func regionFromResourceName(name string) string {
	parts := splitSlash(name)
	// index 2 = "locations", index 3 = location value
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}
