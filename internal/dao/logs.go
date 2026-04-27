package dao

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	logging "cloud.google.com/go/logging/apiv2"
	"cloud.google.com/go/logging/apiv2/loggingpb"
	"cloud.google.com/go/storage"
	"github.com/brekol/g9s/internal/gcp"
	"github.com/rs/zerolog/log"
)

// FetchCloudLoggingPage fetches log text entries matching filter from the
// Cloud Logging API for the given project.
// since is an RFC3339 timestamp used as a lower bound (AND timestamp>="since"
// is appended to the filter); pass "" for no lower bound.
// afterInsertID is the insertId of the last entry already seen within the
// current since window; pass "" to get all entries from since onward.
// Returns new lines, the insertId of the last entry, the RFC3339 timestamp
// of the last entry (for advancing the since window on the next poll), and
// any error encountered.
func FetchCloudLoggingPage(ctx context.Context, project, filter, since, afterInsertID string) (lines []string, lastInsertID, lastTimestamp string, err error) {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return nil, afterInsertID, since, fmt.Errorf("logs: credentials: %w", err)
	}

	client, err := logging.NewClient(ctx, opts...)
	if err != nil {
		return nil, afterInsertID, since, fmt.Errorf("logs: logging client: %w", err)
	}
	defer client.Close()

	fullFilter := filter
	if since != "" {
		fullFilter = fmt.Sprintf(`%s AND timestamp>="%s"`, filter, since)
	}

	it := client.ListLogEntries(ctx, &loggingpb.ListLogEntriesRequest{
		ResourceNames: []string{"projects/" + project},
		Filter:        fullFilter,
		OrderBy:       "timestamp asc",
		PageSize:      200,
	})

	// Use InternalFetch to get exactly one page without auto-paginating.
	entries, _, fetchErr := it.InternalFetch(200, "")
	if fetchErr != nil {
		if ctx.Err() == nil {
			log.Error().Err(fetchErr).Str("project", project).Msg("logs: cloud logging fetch")
		}
		return nil, afterInsertID, since, fmt.Errorf("logs: fetch: %w", fetchErr)
	}

	lastInsertID = afterInsertID
	lastTimestamp = since
	found := (afterInsertID == "")

	for _, entry := range entries {
		if !found {
			if entry.InsertId == afterInsertID {
				found = true
			}
			continue
		}
		// Advance timestamp cursor regardless of payload type.
		if entry.Timestamp != nil {
			if t := entry.Timestamp.AsTime(); t.After(time.Time{}) {
				lastTimestamp = t.UTC().Format(time.RFC3339Nano)
			}
		}
		text := entryText(entry)
		if text != "" {
			if !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			lines = append(lines, text)
			lastInsertID = entry.InsertId
		}
	}

	return lines, lastInsertID, lastTimestamp, nil
}

// FetchCloudLoggingInitial fetches the most recent log entries fast by querying
// in descending order and reversing the result for display.
// filter is the base Cloud Logging filter; since is an RFC3339 lower-bound timestamp
// appended to the filter (pass "" for no lower bound).
// pageSize controls how many entries to fetch (recommended: 200).
// Returns lines in ascending order, the timestamp of the oldest entry (to use
// as a lower bound for subsequent polls via FetchCloudLoggingPage), the insertId
// of the newest entry seen, and any error.
func FetchCloudLoggingInitial(ctx context.Context, project, filter, since string, pageSize int32) (lines []string, oldestTimestamp, newestInsertID string, err error) {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return nil, since, "", fmt.Errorf("logs: credentials: %w", err)
	}
	client, err := logging.NewClient(ctx, opts...)
	if err != nil {
		return nil, since, "", fmt.Errorf("logs: logging client: %w", err)
	}
	defer client.Close()

	fullFilter := filter
	if since != "" {
		fullFilter = fmt.Sprintf(`%s AND timestamp>="%s"`, filter, since)
	}

	it := client.ListLogEntries(ctx, &loggingpb.ListLogEntriesRequest{
		ResourceNames: []string{"projects/" + project},
		Filter:        fullFilter,
		OrderBy:       "timestamp desc",
		PageSize:      pageSize,
	})

	// Use InternalFetch to get exactly one page (respects PageSize) without
	// iterating through all pages like Next() would.
	
	entries, _, err := it.InternalFetch(int(pageSize), "")
	if err != nil {
		if ctx.Err() == nil {
			log.Error().Err(err).Str("project", project).Msg("logs: initial fetch")
		}
		return nil, since, "", fmt.Errorf("logs: initial fetch: %w", err)
	}

	if len(entries) == 0 {
		return nil, since, "", nil
	}

	// entries[0] is newest, entries[last] is oldest.
	newestInsertID = entries[0].InsertId
	oldestTimestamp = since
	if entries[len(entries)-1].Timestamp != nil {
		oldestTimestamp = entries[len(entries)-1].Timestamp.AsTime().UTC().Format(time.RFC3339Nano)
	}

	// Reverse to ascending order for display.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	for _, e := range entries {
		text := entryText(e)
		if text == "" {
			continue
		}
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		lines = append(lines, text)
	}

	return lines, oldestTimestamp, newestInsertID, nil
}

// Pass byteOffset=0 to read the whole object.
// Returns the bytes read and the new offset (byteOffset + len(bytes)).
// If the object does not exist, returns nil bytes and the original offset
// with a wrapped storage.ErrObjectNotExist error.
func FetchGCSRange(ctx context.Context, bucket, object string, byteOffset int64) ([]byte, int64, error) {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return nil, byteOffset, fmt.Errorf("logs: credentials: %w", err)
	}

	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, byteOffset, fmt.Errorf("logs: storage client: %w", err)
	}
	defer client.Close()

	log.Debug().Str("bucket", bucket).Str("object", object).Int64("offset", byteOffset).Msg("logs: reading GCS object")

	var rc io.ReadCloser
	if byteOffset == 0 {
		rc, err = client.Bucket(bucket).Object(object).NewReader(ctx)
	} else {
		rc, err = client.Bucket(bucket).Object(object).NewRangeReader(ctx, byteOffset, -1)
	}
	if err != nil {
		return nil, byteOffset, err // caller checks storage.ErrObjectNotExist
	}
	defer rc.Close()

	buf, err := io.ReadAll(rc)
	if err != nil {
		return nil, byteOffset, fmt.Errorf("logs: read: %w", err)
	}

	return buf, byteOffset + int64(len(buf)), nil
}

// entryText extracts a human-readable line from a log entry.
// Priority: textPayload → httpRequest summary → jsonPayload "message" field → severity label.
func entryText(entry *loggingpb.LogEntry) string {
	// 1. Plain text payload — most app logs.
	if t := entry.GetTextPayload(); t != "" {
		return t
	}

	// 2. HTTP request log (Cloud Run request logs).
	if hr := entry.GetHttpRequest(); hr != nil {
		ts := ""
		if entry.Timestamp != nil {
			ts = entry.Timestamp.AsTime().UTC().Format("2006-01-02T15:04:05Z") + "  "
		}
		status := fmt.Sprintf("%d", hr.Status)
		latency := hr.Latency.AsDuration().Truncate(time.Millisecond).String()
		return fmt.Sprintf("%s%s %s %s %s (%s)", ts, hr.RequestMethod, hr.RequestUrl, status, hr.UserAgent, latency)
	}

	// 3. Structured JSON payload — look for common "message" / "msg" fields.
	if jp := entry.GetJsonPayload(); jp != nil {
		for _, key := range []string{"message", "msg", "text", "log"} {
			if v, ok := jp.Fields[key]; ok {
				if s := v.GetStringValue(); s != "" {
					return s
				}
			}
		}
	}

	return ""
}
