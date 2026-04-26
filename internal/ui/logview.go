package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	logging "cloud.google.com/go/logging/apiv2"
	"cloud.google.com/go/logging/apiv2/loggingpb"
	"cloud.google.com/go/storage"
	"github.com/brekol/g9s/internal/gcp"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// streamingStatuses is the set of build statuses that mean the build is still
// running and the log file is still being written to.
var streamingStatuses = map[string]bool{
	"Working": true,
	"Queued":  true,
	"Pending": true,
}

// Ensure LogView satisfies Overlay at compile time.
var _ Overlay = (*LogView)(nil)

// LogView is a full-screen overlay that displays a Cloud Build log.
// For GCS-backed builds it reads log-<id>.txt; for CLOUD_LOGGING_ONLY builds
// it reads from the Cloud Logging API. For running builds it polls every 2s.
// Press Escape or 'q' to close.
type LogView struct {
	*tview.TextView

	app     *App
	buildID string
	// GCS fields (LEGACY / GCS_ONLY builds)
	bucket string
	object string
	// Cloud Logging fields (CLOUD_LOGGING_ONLY builds)
	project     string
	loggingMode string
	createTime  string // RFC3339, used as timestamp lower bound for log filter

	streaming bool
	onClose   func()
	cancel    context.CancelFunc
}

// NewLogView creates a LogView for the given build.
// bucket is the GCS bucket name (without gs://); pass "" for CLOUD_LOGGING_ONLY builds.
// loggingMode should be the Build.Options.Logging string (e.g. "CLOUD_LOGGING_ONLY", "LEGACY").
// createTime is RFC3339 build creation time, used to scope Cloud Logging queries.
func NewLogView(a *App, buildID, bucket, status, project, loggingMode, createTime string) *LogView {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(false)
	tv.SetBorder(true)
	tv.SetBorderColor(tcell.ColorGreen)
	tv.SetTitle(fmt.Sprintf(" Build %s logs ", buildID))
	tv.SetTitleColor(tcell.ColorWhite)
	tv.SetTitleAlign(tview.AlignCenter)
	tv.SetBackgroundColor(tcell.ColorDefault)

	lv := &LogView{
		TextView:    tv,
		app:         a,
		buildID:     buildID,
		bucket:      bucket,
		object:      fmt.Sprintf("log-%s.txt", buildID),
		project:     project,
		loggingMode: loggingMode,
		createTime:  createTime,
		streaming:   streamingStatuses[status],
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

// Primitive implements Overlay.
func (lv *LogView) Primitive() tview.Primitive { return lv.TextView }

// RenderLoading implements Overlay. Shows a placeholder before the first fetch completes.
func (lv *LogView) RenderLoading() {
	lv.TextView.SetText(fmt.Sprintf(" Loading logs for build %s…", lv.buildID))
}

// OnClose implements Overlay. Called by App.PushOverlay to register the
// dismiss callback.
func (lv *LogView) OnClose(fn func()) { lv.onClose = fn }

// Start implements Overlay. Fetches the log and streams if the build is running.
// Blocks until closed or ctx is cancelled.
func (lv *LogView) Start(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	lv.cancel = cancel
	defer cancel()

	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		lv.appendError(fmt.Errorf("credentials: %w", err))
		return
	}

	if lv.loggingMode == "CLOUD_LOGGING_ONLY" {
		lv.streamCloudLogging(ctx, opts)
		return
	}

	offset := lv.fetch(ctx, opts)

	if !lv.streaming {
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			newOffset := lv.fetchFrom(ctx, opts, offset)
			if newOffset > offset {
				offset = newOffset
			}
			if !lv.streaming {
				return
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

// streamCloudLogging fetches log entries from the Cloud Logging API.
// For running builds it polls every 2s, appending only new entries.
func (lv *LogView) streamCloudLogging(ctx context.Context, opts []option.ClientOption) {
	client, err := logging.NewClient(ctx, opts...)
	if err != nil {
		lv.appendError(fmt.Errorf("logging client: %w", err))
		return
	}
	defer client.Close()

	since := lv.createTime
	if since == "" {
		since = time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	}

	lastInsertID := ""
	first := true

	fetch := func() {
		filter := fmt.Sprintf(
			`logName="projects/%s/logs/cloudbuild" AND resource.type="build" AND resource.labels.build_id="%s" AND timestamp>="%s"`,
			lv.project, lv.buildID, since,
		)

		it := client.ListLogEntries(ctx, &loggingpb.ListLogEntriesRequest{
			ResourceNames: []string{"projects/" + lv.project},
			Filter:        filter,
			OrderBy:       "timestamp asc",
			PageSize:      1000,
		})

		var newLines []string
		found := (lastInsertID == "")
		for {
			entry, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				if ctx.Err() == nil {
					log.Error().Err(err).Str("build", lv.buildID).Msg("logview: cloud logging fetch")
				}
				break
			}
			if !found {
				if entry.InsertId == lastInsertID {
					found = true
				}
				continue
			}
			text := entry.GetTextPayload()
			if text != "" {
				if !strings.HasSuffix(text, "\n") {
					text += "\n"
				}
				newLines = append(newLines, text)
				lastInsertID = entry.InsertId
			}
		}

		if len(newLines) == 0 && first {
			lv.app.tview.QueueUpdateDraw(func() {
				if lv.streaming {
					lv.TextView.SetText(" Waiting for logs…")
				} else {
					lv.TextView.SetText(" No log entries found for this build.")
				}
			})
			first = false
			return
		}

		if len(newLines) > 0 {
			content := stripAnsi(strings.Join(newLines, ""))
			lv.app.tview.QueueUpdateDraw(func() {
				if first {
					lv.TextView.SetText(content)
					first = false
				} else {
					fmt.Fprint(lv.TextView, content)
				}
				lv.TextView.ScrollToEnd()
			})
		}
	}

	fetch()

	if !lv.streaming {
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetch()
			if !lv.streaming {
				return
			}
		}
	}
}

// fetch reads the entire GCS log object and writes it to the view.
// Returns the total number of bytes read (0 if the object doesn't exist yet).
func (lv *LogView) fetch(ctx context.Context, opts []option.ClientOption) int64 {
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		lv.appendError(fmt.Errorf("storage client: %w", err))
		return 0
	}
	defer client.Close()

	log.Debug().Str("bucket", lv.bucket).Str("object", lv.object).Msg("logview: opening object")
	rc, err := client.Bucket(lv.bucket).Object(lv.object).NewReader(ctx)
	if err != nil {
		log.Debug().Err(err).Str("bucket", lv.bucket).Str("object", lv.object).Msg("logview: open error")
		if isNotExist(err) {
			lv.app.tview.QueueUpdateDraw(func() {
				if lv.streaming {
					lv.TextView.SetText(" Waiting for logs…")
				} else {
					lv.TextView.SetText(" No logs available for this build.")
				}
			})
			return 0
		}
		lv.appendError(fmt.Errorf("open log: %w", err))
		return 0
	}
	defer rc.Close()

	buf, err := io.ReadAll(rc)
	if err != nil {
		lv.appendError(fmt.Errorf("read log: %w", err))
		return 0
	}

	content := stripAnsi(string(buf))
	lv.app.tview.QueueUpdateDraw(func() {
		lv.TextView.SetText(content)
		lv.TextView.ScrollToEnd()
	})
	return int64(len(buf))
}

// fetchFrom reads bytes starting at byteOffset and appends them to the view.
// Returns the new offset after the read.
func (lv *LogView) fetchFrom(ctx context.Context, opts []option.ClientOption, byteOffset int64) int64 {
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		log.Error().Err(err).Msg("logview: storage client")
		return byteOffset
	}
	defer client.Close()

	obj := client.Bucket(lv.bucket).Object(lv.object)
	rc, err := obj.NewRangeReader(ctx, byteOffset, -1)
	if err != nil {
		return byteOffset
	}
	defer rc.Close()

	buf, err := io.ReadAll(rc)
	if err != nil || len(buf) == 0 {
		return byteOffset
	}

	content := stripAnsi(string(buf))
	lv.app.tview.QueueUpdateDraw(func() {
		fmt.Fprint(lv.TextView, content)
		lv.TextView.ScrollToEnd()
	})

	return byteOffset + int64(len(buf))
}

// appendError writes an error message into the text view.
func (lv *LogView) appendError(err error) {
	log.Error().Err(err).Str("build", lv.buildID).Msg("logview fetch failed")
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

// isNotExist returns true if the error indicates a GCS object does not exist.
func isNotExist(err error) bool {
	return errors.Is(err, storage.ErrObjectNotExist)
}
