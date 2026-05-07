package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/brekol/g9s/internal/dao/vms"
	"github.com/gdamore/tcell/v2"
)

// VMsView is the tview page for Compute Engine VM instances.
//
// All lifecycle, rendering, and Filterable/ResourceView/TableListener glue is
// inherited from the embedded *ResourceTable; only resource-specific concerns
// (typed DAO, key bindings, hints, delete/log actions) live here.
type VMsView struct {
	*ResourceTable

	app *App
	dao *vms.VMs
}

// Ensure interfaces are satisfied at compile time.
var (
	_ ResourceView = (*VMsView)(nil)
	_ KeyHandler   = (*VMsView)(nil)
	_ HintProvider = (*VMsView)(nil)
)

// NewVMsView creates a VMsView for the given project.
func NewVMsView(a *App, project string) *VMsView {
	d := new(vms.VMs)
	return &VMsView{
		ResourceTable: NewResourceView(a, project, "vms", "VMs", "VM instances", d),
		app:           a,
		dao:           d,
	}
}

// Hints implements HintProvider. Only resource-specific bindings; generic
// d/y/c are advertised by the global dispatcher.
func (v *VMsView) Hints() []Hint {
	return []Hint{
		{Key: "s", Desc: "SSH"},
		{Key: "l", Desc: "Logs"},
		{Key: "Ctrl-D", Desc: "Delete"},
	}
}

// HandleKey implements KeyHandler.
func (v *VMsView) HandleKey(event *tcell.EventKey) bool {
	if event.Key() == tcell.KeyCtrlD {
		return v.confirmDelete()
	}
	if event.Rune() == 'l' {
		return v.openLogs()
	}
	if event.Rune() == 's' {
		return v.sshIntoInstance()
	}
	return false
}

// confirmDelete pushes a ConfirmOverlay; on Enter it dispatches the delete
// via app.TrackOp so the user sees "Delete foo… (running)" / "succeeded" /
// "failed: …" on the status bar even after switching away from this view.
func (v *VMsView) confirmDelete() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	ir, ok := row.(*vms.InstanceRow)
	if !ok || ir.Project == "" || ir.Zone == "" || ir.Name == "" {
		return true
	}

	prompt := fmt.Sprintf("Delete instance [yellow]%s[white] in zone [yellow]%s[white]?", ir.Name, ir.Zone)
	title := fmt.Sprintf("VM – %s", ir.Name)
	project, zone, name := ir.Project, ir.Zone, ir.Name
	co := NewConfirmOverlay(v.app, title, prompt, func() {
		v.app.TrackOp("Delete VM "+name, func(ctx context.Context) error {
			return v.dao.Delete(ctx, project, zone, name)
		})
	})
	v.app.PushOverlay(co)
	return true
}

// openLogs pushes a LogView overlay streaming Cloud Logging entries for the
// selected VM, scoped by numeric instance_id (precise, zone-independent).
func (v *VMsView) openLogs() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	ir, ok := row.(*vms.InstanceRow)
	if !ok {
		return true
	}
	openInstanceLogs(v.app, ir.Project, ir.NumericID, ir.Name)
	return true
}

// sshIntoInstance suspends the TUI and execs `gcloud compute ssh` against
// the selected instance. Stderr is restored to the real terminal (from
// OriginalStderr) for the duration of the child process so SSH host-key
// confirmations and auth prompts are visible; the redirect to the log file
// is restored on return so the resumed TUI is not corrupted by any later
// stderr noise from gRPC/oauth2.
//
// Errors (missing gcloud, non-zero exit, gcloud auth failure, VM not
// running, etc.) surface on the status bar; the full error is also written
// to the log file by zerolog.
func (v *VMsView) sshIntoInstance() bool {
	row := v.SelectedRow()
	if row == nil {
		return true
	}
	ir, ok := row.(*vms.InstanceRow)
	if !ok || ir.Project == "" || ir.Zone == "" || ir.Name == "" {
		v.app.Status(StatusInfo, "No instance selected, or instance not yet created.")
		return true
	}

	project, zone, name := ir.Project, ir.Zone, ir.Name
	v.app.Status(StatusInfo, fmt.Sprintf("SSH %s… (running)", name))

	var runErr error
	ok = v.app.tview.Suspend(func() {
		cmd := exec.Command("gcloud", "compute", "ssh", name,
			"--zone", zone,
			"--project", project,
		)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		// Route the child's stderr to the real terminal so OpenSSH's
		// host-key prompt and any gcloud diagnostics are visible. Fall
		// back to os.Stderr (which cmd/root has redirected to the log
		// file) only if OriginalStderr was not captured at startup.
		if OriginalStderr != nil {
			cmd.Stderr = OriginalStderr
		} else {
			cmd.Stderr = os.Stderr
		}
		runErr = cmd.Run()
	})
	if !ok {
		v.app.Status(StatusError, "tview.Suspend returned false (already suspended)")
		return true
	}
	if runErr != nil {
		v.app.Status(StatusError, fmt.Sprintf("SSH %s failed: %v", name, runErr))
		return true
	}
	v.app.Status(StatusInfo, fmt.Sprintf("SSH %s exited", name))
	return true
}
