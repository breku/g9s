package model

import (
	"sort"
	"strings"
	"time"

	"github.com/brekol/g9s/internal/dao"
)

// Registry maps resource identifiers to their ResourceMeta.
// TTL rationale per resource type:
//   - cloudrun: services are long-lived and change infrequently; 60 s is a
//     good balance between freshness and API quota consumption.
//
// When adding new resource types, choose a TTL based on:
//   - How often the resource changes in practice.
//   - The cost/quota weight of the List API call.
//   - User expectation of how "live" the data should feel.
var Registry = map[string]ResourceMeta{
	"cloudrun":   {DAO: new(dao.CloudRun), TTL: 60 * time.Second},
	"cloudbuild": {DAO: new(dao.CloudBuild), TTL: 30 * time.Second},
}

// Aliases maps shorthand command names to canonical registry keys.
// Keep entries sorted for deterministic autocomplete ordering.
var Aliases = map[string]string{
	"cloud-run":   "cloudrun",
	"cloudrun":    "cloudrun",
	"run":         "cloudrun",
	"cloud-build": "cloudbuild",
	"cloudbuild":  "cloudbuild",
	"build":       "cloudbuild",
	"cb":          "cloudbuild",
}

// Lookup returns the ResourceMeta for the given resource key.
// The second return value is false if the resource is not registered.
func Lookup(resource string) (ResourceMeta, bool) {
	m, ok := Registry[resource]
	return m, ok
}

// Resolve maps an alias or canonical key to a ResourceMeta.
// Returns false if the input matches no known alias or registry key.
func Resolve(input string) (ResourceMeta, bool) {
	key, ok := Aliases[strings.ToLower(strings.TrimSpace(input))]
	if !ok {
		return ResourceMeta{}, false
	}
	return Lookup(key)
}

// CompleteCommand returns all alias names that have the given prefix,
// sorted alphabetically. Used to populate the cmdbar autocomplete list.
func CompleteCommand(prefix string) []string {
	prefix = strings.ToLower(prefix)
	var matches []string
	for alias := range Aliases {
		if strings.HasPrefix(alias, prefix) {
			matches = append(matches, alias)
		}
	}
	sort.Strings(matches)
	return matches
}
