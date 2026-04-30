package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/dao/cloudrun"
	"github.com/brekol/g9s/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// CloudRunView is the tview page for Cloud Run services.
type CloudRunView struct {
	*ResourceTable

	app     *App
	project string
	mdl     *model.Table
	dao     *cloudrun.CloudRun
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
		dao:           new(cloudrun.CloudRun),
	}
	v.mdl.AddListener(v)
	return v
}

// Primitive implements ResourceView.
func (v *CloudRunView) Primitive() tview.Primitive { return v.Table }

// Watch implements ResourceView.
func (v *CloudRunView) Watch(ctx context.Context) error { return v.mdl.Watch(ctx) }

// Stop implements ResourceView.
func (v *CloudRunView) Stop() { v.mdl.Stop() }

// DAO implements ResourceView.
func (v *CloudRunView) DAO() dao.Accessor { return v.dao }

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

// Hints implements HintProvider. Returns only resource-specific bindings;
// generic d/y/c are advertised by the global dispatcher.
func (v *CloudRunView) Hints() []Hint {
	return []Hint{
		{Key: "e", Desc: "Edit"},
		{Key: "l", Desc: "Logs"},
	}
}

// HandleKey implements KeyHandler.
func (v *CloudRunView) HandleKey(event *tcell.EventKey) bool {
	switch event.Rune() {
	case 'e':
		return v.editService()
	case 'l':
		return v.openLogs()
	}
	return false
}

// editService suspends the TUI, opens $EDITOR (default: vim) on the selected
// service's YAML, and submits the changes when the editor exits cleanly.
// The submission and the long-running deploy are tracked via app.TrackOp,
// which surfaces "Deploying foo… (running)" / "Deploy succeeded" /
// "Deploy failed: <err>" on the status bar — outliving any view switch.
//
// On a fast-fail (parse error, no diff, immediate API rejection) the status
// bar is updated synchronously before the function returns. The full error
// is always written to the log file regardless of where it surfaces.
func (v *CloudRunView) editService() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	name := row.GetID()
	short := lastSegmentUI(name)

	// Fetch current YAML before suspending so any error is visible in the TUI.
	yamlStr, err := v.dao.DescribeYAML(v.app.ctx, name)
	if err != nil {
		v.app.Status(StatusError, fmt.Sprintf("Fetch %s for edit failed: %v", short, err))
		return true
	}

	// Run the editor inside Suspend so tcell releases the terminal.
	var editedYAML string
	var changed bool
	ok := v.app.tview.Suspend(func() {
		editedYAML, changed, err = runEditor(short, yamlStr)
	})
	if !ok {
		v.app.Status(StatusError, "tview.Suspend returned false (already suspended)")
		return true
	}
	if err != nil {
		v.app.Status(StatusError, fmt.Sprintf("Edit %s failed: %v", short, err))
		return true
	}
	if !changed {
		v.app.Status(StatusInfo, fmt.Sprintf("No changes to %s", short))
		return true
	}

	// Submit the request synchronously (so a parse/validation error from
	// the API surfaces immediately), then track the LRO for the actual
	// deploy outcome on the App ctx so it survives view switches.
	wait, err := v.dao.UpdateServiceFromYAML(v.app.ctx, editedYAML)
	if err != nil {
		v.app.Status(StatusError, fmt.Sprintf("Deploy %s submit failed: %v", short, err))
		return true
	}
	v.app.TrackOp("Deploy "+short, wait)
	return true
}

// runEditor writes the YAML to a temp file, opens $EDITOR on it, and
// returns the (possibly-changed) contents. The bool reports whether the
// user actually modified the file. Pure I/O — no API calls.
func runEditor(shortName, original string) (string, bool, error) {
	tmp, err := os.CreateTemp("", "g9s-cloudrun-"+shortName+"-*.yaml")
	if err != nil {
		return "", false, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(original); err != nil {
		tmp.Close()
		return "", false, fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", false, fmt.Errorf("close temp file: %w", err)
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
		return "", false, fmt.Errorf("editor exited with error: %w", err)
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", false, fmt.Errorf("read edited file: %w", err)
	}
	return string(edited), string(edited) != original, nil
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
