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
// Cloud Logging API for the given project, returning entries strictly newer
// than the (since, seenInsertIDs) cursor.
//
// since is an RFC3339Nano timestamp lower bound (inclusive in the API filter,
// but entries at exactly that timestamp are deduplicated against
// seenInsertIDs). seenInsertIDs is the set of InsertIds that share the
// `since` timestamp from the previous page — they're skipped to avoid
// duplicating already-displayed lines when a new entry arrives at the same
// nanosecond as the previous newest.
//
// Returns the new lines, the InsertIds at the new latest timestamp (for the
// next call's seenInsertIDs), and the new latest timestamp.
func FetchCloudLoggingPage(ctx context.Context, project, filter, since string, seenInsertIDs map[string]struct{}) (lines []string, newLatestIDs map[string]struct{}, newLatestTimestamp string, err error) {
	client, err := gcp.LoggingClient()
	if err != nil {
		return nil, seenInsertIDs, since, fmt.Errorf("logs: logging client: %w", err)
	}

	fullFilter := filter
	if since != "" {
		fullFilter = fmt.Sprintf(`%s AND timestamp>="%s"`, filter, since)
	}

	it := client.ListLogEntries(ctx, &loggingpb.ListLogEntriesRequest{
		ResourceNames: []string{"projects/" + project},
		Filter:        fullFilter,
		OrderBy:       "timestamp asc",
		PageSize:      500,
	})

	entries, _, fetchErr := it.InternalFetch(500, "")
	if fetchErr != nil {
		if ctx.Err() == nil {
			log.Error().Err(fetchErr).Str("project", project).Msg("logs: cloud logging fetch")
		}
		return nil, seenInsertIDs, since, fmt.Errorf("logs: fetch: %w", fetchErr)
	}

	newLatestTimestamp = since
	newLatestIDs = seenInsertIDs

	for _, entry := range entries {
		// Skip entries already seen on a previous page (same timestamp as cursor).
		if _, seen := seenInsertIDs[entry.InsertId]; seen {
			continue
		}
		text := entryText(entry)
		if text == "" {
			continue
		}
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		lines = append(lines, text)

		ts := ""
		if entry.Timestamp != nil {
			ts = entry.Timestamp.AsTime().UTC().Format(time.RFC3339Nano)
		}
		// Track the InsertIds at the new latest timestamp so the next poll
		// can dedup entries with the exact same timestamp.
		if ts != "" {
			if ts != newLatestTimestamp {
				newLatestTimestamp = ts
				newLatestIDs = map[string]struct{}{entry.InsertId: {}}
			} else {
				if newLatestIDs == nil {
					newLatestIDs = map[string]struct{}{}
				}
				newLatestIDs[entry.InsertId] = struct{}{}
			}
		}
	}

	return lines, newLatestIDs, newLatestTimestamp, nil
}

// FetchCloudLoggingInitial fetches the most recent log entries fast by querying
// in descending order and reversing the result for display.
//
// Returns the rendered lines (oldest first), the timestamp of the newest entry
// seen (suitable as the `since` lower bound for polling), and the set of
// InsertIds at that newest timestamp (so polling can dedup).
func FetchCloudLoggingInitial(ctx context.Context, project, filter, since string, pageSize int32) (lines []string, newestTimestamp string, newestInsertIDs map[string]struct{}, err error) {
	client, err := gcp.LoggingClient()
	if err != nil {
		return nil, since, nil, fmt.Errorf("logs: logging client: %w", err)
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
		return nil, since, nil, fmt.Errorf("logs: initial fetch: %w", err)
	}

	if len(entries) == 0 {
		return nil, since, nil, nil
	}

	// Newest entry sits first in desc order. Capture its timestamp and all
	// InsertIds that share that exact timestamp so polling can dedup them.
	if entries[0].Timestamp != nil {
		newestTimestamp = entries[0].Timestamp.AsTime().UTC().Format(time.RFC3339Nano)
	}
	newestInsertIDs = map[string]struct{}{}
	for _, e := range entries {
		ts := ""
		if e.Timestamp != nil {
			ts = e.Timestamp.AsTime().UTC().Format(time.RFC3339Nano)
		}
		if ts != newestTimestamp {
			break
		}
		newestInsertIDs[e.InsertId] = struct{}{}
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

	return lines, newestTimestamp, newestInsertIDs, nil
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
