package dao

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FormatTime formats a time.Time in the local timezone as "2006-01-02 15:04",
// or "—" if the value is zero. Exported for use by subpackage DAOs.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// LastSegment returns the last "/" segment of a fully-qualified resource name.
// e.g. "projects/p/locations/l/services/foo" → "foo"
// Exported for use by subpackage DAOs.
func LastSegment(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return name
	}
	return parts[len(parts)-1]
}

// JSONToYAML converts a JSON byte slice to a YAML string by unmarshaling
// into a generic map and re-marshaling as YAML. This indirection makes the
// YAML field names match the JSON representation users see in
// `gcloud ... --format=yaml`, rather than the underlying Go struct field
// names that yaml.Marshal would otherwise emit.
//
// Use this directly when the source is a proto message (marshal via
// protojson first to preserve canonical field names). For plain Go structs
// with json tags, prefer ObjectToYAML which wraps the encoding/json step.
// Errors are wrapped with the "dao:" prefix; callers typically wrap again
// with their own package prefix.
func JSONToYAML(jsonBytes []byte) (string, error) {
	var m interface{}
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return "", fmt.Errorf("dao: unmarshal json: %w", err)
	}
	yamlBytes, err := yaml.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("dao: marshal yaml: %w", err)
	}
	return string(yamlBytes), nil
}

// ObjectToYAML marshals a Go value to YAML by going through encoding/json
// first, so YAML field names follow the value's `json:` struct tags rather
// than its Go field names. Suitable for non-proto API response types
// (e.g. compute API structs). For proto messages use protojson + JSONToYAML
// directly so canonical proto JSON naming is preserved.
func ObjectToYAML(obj any) (string, error) {
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("dao: marshal json: %w", err)
	}
	return JSONToYAML(jsonBytes)
}

// YAMLToJSON is the inverse of JSONToYAML: parses a YAML string into a
// generic value and re-marshals it as JSON bytes. Used by edit paths
// (e.g. cloudrun.UpdateServiceFromYAML) to turn a user-edited YAML buffer
// back into JSON suitable for protojson.Unmarshal or encoding/json.Unmarshal
// into the typed API request.
//
// Errors are wrapped with the "dao:" prefix; callers typically wrap again
// with their own package prefix.
func YAMLToJSON(yamlStr string) ([]byte, error) {
	var m interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &m); err != nil {
		return nil, fmt.Errorf("dao: parse yaml: %w", err)
	}
	jsonBytes, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("dao: marshal json: %w", err)
	}
	return jsonBytes, nil
}
