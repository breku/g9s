// Package logs provides log fetching helpers for Cloud Logging and GCS.
package logs

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"cloud.google.com/go/logging/apiv2/loggingpb"
	"github.com/brekol/g9s/internal/gcp"
	"github.com/rs/zerolog/log"
)

// FetchCloudLoggingPage fetches log text entries matching filter from the
// Cloud Logging API for the given project.
func FetchCloudLoggingPage(ctx context.Context, project, filter, since, afterInsertID string) (lines []string, lastInsertID, lastTimestamp string, err error) {
	client, err := gcp.LoggingClient()
	if err != nil {
		return nil, afterInsertID, since, fmt.Errorf("logs: logging client: %w", err)
	}

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
func FetchCloudLoggingInitial(ctx context.Context, project, filter, since string, pageSize int32) (lines []string, oldestTimestamp, newestInsertID string, err error) {
	client, err := gcp.LoggingClient()
	if err != nil {
		return nil, since, "", fmt.Errorf("logs: logging client: %w", err)
	}

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

	newestInsertID = entries[0].InsertId
	oldestTimestamp = since
	if entries[len(entries)-1].Timestamp != nil {
		oldestTimestamp = entries[len(entries)-1].Timestamp.AsTime().UTC().Format(time.RFC3339Nano)
	}

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

// FetchGCSRange reads bytes from a GCS object starting at byteOffset.
// Pass byteOffset=0 to read the whole object.
func FetchGCSRange(ctx context.Context, bucket, object string, byteOffset int64) ([]byte, int64, error) {
	client, err := gcp.StorageClient()
	if err != nil {
		return nil, byteOffset, fmt.Errorf("logs: storage client: %w", err)
	}

	log.Debug().Str("bucket", bucket).Str("object", object).Int64("offset", byteOffset).Msg("logs: reading GCS object")

	var rc io.ReadCloser
	if byteOffset == 0 {
		rc, err = client.Bucket(bucket).Object(object).NewReader(ctx)
	} else {
		rc, err = client.Bucket(bucket).Object(object).NewRangeReader(ctx, byteOffset, -1)
	}
	if err != nil {
		return nil, byteOffset, err
	}
	defer rc.Close()

	buf, err := io.ReadAll(rc)
	if err != nil {
		return nil, byteOffset, fmt.Errorf("logs: read: %w", err)
	}

	return buf, byteOffset + int64(len(buf)), nil
}

// entryText extracts a human-readable line from a log entry.
func entryText(entry *loggingpb.LogEntry) string {
	if t := entry.GetTextPayload(); t != "" {
		return t
	}

	if hr := entry.GetHttpRequest(); hr != nil {
		ts := ""
		if entry.Timestamp != nil {
			ts = entry.Timestamp.AsTime().UTC().Format("2006-01-02T15:04:05Z") + "  "
		}
		status := fmt.Sprintf("%d", hr.Status)
		latency := hr.Latency.AsDuration().Truncate(time.Millisecond).String()
		return fmt.Sprintf("%s%s %s %s %s (%s)", ts, hr.RequestMethod, hr.RequestUrl, status, hr.UserAgent, latency)
	}

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
