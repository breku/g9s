package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/brekol/g9s/internal/dao/logs"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// streamingStatuses is the set of build statuses that mean the build is still
// running and the log file is still being written to.
var streamingStatuses = map[string]bool{
	"Working": true,
	"Queued":  true,
	"Pending": true,
}

// Ensure LogView satisfies Overlay, HintProvider and Filterable at compile time.
var (
	_ Overlay      = (*LogView)(nil)
	_ HintProvider = (*LogView)(nil)
	_ Filterable   = (*LogView)(nil)
)

// LogViewConfig holds all parameters needed to create a LogView.
// Use NewLogViewFromConfig for the generic constructor.
type LogViewConfig struct {
	// Title is shown in the overlay border.
	Title string
	// Streaming controls whether the view polls for new content every 2s.
	Streaming bool

	// --- GCS path (optional; for LEGACY/GCS_ONLY Cloud Build logs) ---
	Bucket string // GCS bucket name without gs://
	Object string // GCS object path

	// --- Cloud Logging (used when LogFilter is non-empty) ---
	// Project is the GCP project ID.
	Project string
	// LogFilter is the full Cloud Logging filter expression.
	// When set, Cloud Logging is used regardless of Bucket.
	LogFilter string
	// LogSince is an optional RFC3339 timestamp lower bound appended to LogFilter
	// for the initial fetch. Pass "" for no lower bound.
	LogSince string
	// LogPageSize is how many entries to fetch on initial load (default 200).
	LogPageSize int32
}

// LogView is a full-screen overlay that displays logs.
// It supports two backends: GCS (for Cloud Build LEGACY logs) and
// Cloud Logging API (for CLOUD_LOGGING_ONLY builds and Cloud Run).
// When Streaming is true it polls every 2s. Press Escape or 'q' to close.
// Press '/' to activate the filter bar and filter log lines by substring.
type LogView struct {
	*tview.TextView

	app      *App
	cfg      LogViewConfig
	fullText string // accumulated unfiltered log content
	filter   string // active filter substring (lower-cased)

	onClose func()
	cancel  context.CancelFunc
}

// NewLogViewFromConfig creates a LogView from a LogViewConfig.
func NewLogViewFromConfig(a *App, cfg LogViewConfig) *LogView {
	tv := tview.NewTextView().
		SetDynamicColors(false).
		SetScrollable(true).
		SetWordWrap(false)
	tv.SetBorder(true)
	tv.SetBorderColor(AppTheme.HighlightColor)
	tv.SetTitle(fmt.Sprintf(" %s ", cfg.Title))
	tv.SetTitleColor(tcell.ColorWhite)
	tv.SetTitleAlign(tview.AlignCenter)
	tv.SetBackgroundColor(AppTheme.BackgroundColor)

	lv := &LogView{
		TextView: tv,
		app:      a,
		cfg:      cfg,
	}

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Rune() == 'q' {
			lv.close()
			return nil
		}
		return event
	})

	return lv
}

// NewLogView creates a LogView for the given Cloud Build build.
// bucket is the GCS bucket name (without gs://); pass "" for CLOUD_LOGGING_ONLY builds.
// loggingMode should be the Build.Options.Logging string (e.g. "CLOUD_LOGGING_ONLY", "LEGACY").
// createTime is RFC3339 build creation time, used to scope Cloud Logging queries.
func NewLogView(a *App, buildID, bucket, status, project, loggingMode, createTime string) *LogView {
	cfg := LogViewConfig{
		Title:     fmt.Sprintf("Build %s logs", buildID),
		Streaming: streamingStatuses[status],
		Bucket:    bucket,
		Project:   project,
	}
	if bucket != "" {
		cfg.Object = fmt.Sprintf("log-%s.txt", buildID)
	}
	if loggingMode == "CLOUD_LOGGING_ONLY" || bucket == "" {
		since := createTime
		if since == "" {
			since = "1970-01-01T00:00:00Z"
		}
		cfg.LogFilter = fmt.Sprintf(
			`logName="projects/%s/logs/cloudbuild" AND resource.type="build" AND resource.labels.build_id="%s" AND timestamp>="%s"`,
			project, buildID, since,
		)
	}
	return NewLogViewFromConfig(a, cfg)
}

// Primitive implements Overlay.
func (lv *LogView) Primitive() tview.Primitive { return lv.TextView }

// Hints implements HintProvider.
func (lv *LogView) Hints() []Hint {
	return []Hint{
		{Key: "q/Esc", Desc: "Close"},
		{Key: "/", Desc: "Filter"},
	}
}

// RenderLoading implements Overlay. Shows a placeholder before the first fetch completes.
func (lv *LogView) RenderLoading() {
	lv.TextView.SetText(fmt.Sprintf(" Loading %s…", lv.cfg.Title))
}

// OnClose implements Overlay. Called by App.PushOverlay to register the
// dismiss callback.
func (lv *LogView) OnClose(fn func()) { lv.onClose = fn }

// SetFilter implements Filterable. Filters displayed lines to those containing
// the needle (case-insensitive). An empty string shows all lines.
// Must be called on the tview main goroutine.
func (lv *LogView) SetFilter(f string) {
	lv.filter = strings.ToLower(f)
	lv.applyFilter()
	lv.updateTitle()
}

// appendContent appends new content to the full log buffer and re-renders.
// Must be called on the tview main goroutine.
func (lv *LogView) appendContent(text string) {
	lv.fullText += text
	lv.applyFilter()
}

// setContent replaces the full log buffer and re-renders.
// Must be called on the tview main goroutine.
func (lv *LogView) setContent(text string) {
	lv.fullText = text
	lv.applyFilter()
}

// applyFilter re-renders the TextView from fullText, keeping only lines that
// contain lv.filter (case-insensitive). If filter is empty all lines are shown.
// Must be called on the tview main goroutine.
func (lv *LogView) applyFilter() {
	if lv.filter == "" {
		lv.TextView.SetText(lv.fullText)
		lv.TextView.ScrollToEnd()
		return
	}
	var b strings.Builder
	for _, line := range strings.Split(lv.fullText, "\n") {
		if strings.Contains(strings.ToLower(line), lv.filter) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	lv.TextView.SetText(b.String())
	lv.TextView.ScrollToEnd()
}

// updateTitle refreshes the border title to reflect the active filter.
func (lv *LogView) updateTitle() {
	if lv.filter == "" {
		lv.TextView.SetTitle(fmt.Sprintf(" %s ", lv.cfg.Title))
	} else {
		lv.TextView.SetTitle(fmt.Sprintf(" %s [/%s] ", lv.cfg.Title, lv.filter))
	}
}

// Start implements Overlay. Fetches the log and streams if configured.
// Blocks until closed or ctx is cancelled.
func (lv *LogView) Start(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	lv.cancel = cancel
	defer cancel()

	if lv.cfg.LogFilter != "" {
		lv.streamCloudLogging(ctx)
		return
	}

	offset := lv.fetch(ctx)

	if !lv.cfg.Streaming {
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			newOffset := lv.fetchFrom(ctx, offset)
			if newOffset > offset {
				offset = newOffset
			}
		}
	}
}

// close cancels background work and calls the registered onClose callback
// so App.PopOverlay cleans up pages/mouse/focus.
func (lv *LogView) close() {
	if lv.cancel != nil {
		lv.cancel()
	}
	if lv.onClose != nil {
		lv.onClose()
	}
}

// streamCloudLogging fetches log entries from the Cloud Logging API via the
// logs.FetchCloudLoggingPage function. Polls every 2s when Streaming is true.
// Uses desc-order initial fetch for speed, then asc polling with a timestamp cursor.
func (lv *LogView) streamCloudLogging(ctx context.Context) {
	pageSize := lv.cfg.LogPageSize
	if pageSize == 0 {
		pageSize = 200
	}

	// --- Initial load: fetch desc, reversed — fast even on busy services ---
	lines, pollSince, newestInsertID, err := logs.FetchCloudLoggingInitial(ctx, lv.cfg.Project, lv.cfg.LogFilter, lv.cfg.LogSince, pageSize)
	if err != nil && ctx.Err() == nil {
		log.Error().Err(err).Str("title", lv.cfg.Title).Msg("logview: initial fetch")
	}

	if len(lines) == 0 {
		lv.app.tview.QueueUpdateDraw(func() {
			if lv.cfg.Streaming {
				lv.setContent(" Waiting for logs…")
			} else {
				lv.setContent(" No log entries found.")
			}
		})
	} else {
		content := stripAnsi(strings.Join(lines, ""))
		lv.app.tview.QueueUpdateDraw(func() {
			lv.setContent(content)
		})
	}

	if !lv.cfg.Streaming {
		return
	}

	// --- Poll: asc from the newest entry seen so far ---
	since := pollSince
	lastInsertID := newestInsertID

	poll := func() {
		newLines, newLastID, newSince, pollErr := logs.FetchCloudLoggingPage(ctx, lv.cfg.Project, lv.cfg.LogFilter, since, lastInsertID)
		if pollErr != nil {
			if ctx.Err() == nil {
				log.Error().Err(pollErr).Str("title", lv.cfg.Title).Msg("logview: poll fetch")
			}
			return
		}
		if len(newLines) > 0 {
			since = newSince
			lastInsertID = newLastID
			content := stripAnsi(strings.Join(newLines, ""))
			lv.app.tview.QueueUpdateDraw(func() {
				lv.appendContent(content)
			})
		}
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}

// fetch reads the entire GCS log object and writes it to the view.
// Returns the total number of bytes read (0 if the object doesn't exist yet).
func (lv *LogView) fetch(ctx context.Context) int64 {
	buf, newOffset, err := logs.FetchGCSRange(ctx, lv.cfg.Bucket, lv.cfg.Object, 0)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			lv.app.tview.QueueUpdateDraw(func() {
				if lv.cfg.Streaming {
					lv.setContent(" Waiting for logs…")
				} else {
					lv.setContent(" No logs available.")
				}
			})
			return 0
		}
		lv.appendError(err)
		return 0
	}

	content := stripAnsi(string(buf))
	lv.app.tview.QueueUpdateDraw(func() {
		lv.setContent(content)
	})
	return newOffset
}

// fetchFrom reads bytes starting at byteOffset and appends them to the view.
// Returns the new offset after the read.
func (lv *LogView) fetchFrom(ctx context.Context, byteOffset int64) int64 {
	buf, newOffset, err := logs.FetchGCSRange(ctx, lv.cfg.Bucket, lv.cfg.Object, byteOffset)
	if err != nil || len(buf) == 0 {
		return byteOffset
	}

	content := stripAnsi(string(buf))
	lv.app.tview.QueueUpdateDraw(func() {
		lv.appendContent(content)
	})
	return newOffset
}

// appendError writes an error message into the text view.
func (lv *LogView) appendError(err error) {
	log.Error().Err(err).Str("title", lv.cfg.Title).Msg("logview fetch failed")
	lv.app.tview.QueueUpdateDraw(func() {
		fmt.Fprintf(lv.TextView, "\n[red]Error: %v[white]\n", err)
	})
}

// stripAnsi removes ANSI escape sequences from s so tview doesn't render
// them as literal bytes. Cloud Build logs frequently contain colour codes.
func stripAnsi(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && !isAnsiEnd(s[i]) {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func isAnsiEnd(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
