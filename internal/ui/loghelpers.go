package ui

import (
	"fmt"
	"time"
)

// openInstanceLogs pushes a streaming LogView overlay for a Compute Engine
// VM instance, scoped by its numeric instance_id (precise, zone-independent).
// Used by both VMsView and the miginstances drill-down so the per-instance
// log UX stays in one place.
//
// project must be a non-empty GCP project ID; numericID is the Compute
// Engine instance numeric ID as a decimal string; name is a display label
// for the overlay border (typically the instance name).
func openInstanceLogs(a *App, project, numericID, name string) {
	if project == "" || numericID == "" {
		a.Status(StatusInfo, "No instance selected, or instance not yet created.")
		return
	}
	filter := fmt.Sprintf(`resource.type="gce_instance" AND resource.labels.instance_id="%s"`, numericID)
	cfg := LogViewConfig{
		Title:       fmt.Sprintf("Logs – %s", name),
		Streaming:   true,
		Project:     project,
		LogFilter:   filter,
		LogSince:    time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
		LogPageSize: 200,
	}
	lv := NewLogViewFromConfig(a, cfg)
	a.PushOverlay(lv)
}
