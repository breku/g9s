package model

import (
	"sort"
	"strings"
	"time"

	"github.com/brekol/g9s/internal/dao/buildhistory"
	"github.com/brekol/g9s/internal/dao/cloudbuild"
	"github.com/brekol/g9s/internal/dao/cloudrun"
	"github.com/brekol/g9s/internal/dao/secrets"
	"github.com/brekol/g9s/internal/dao/vms"
)

// Registry maps resource identifiers to their ResourceMeta.
//
// RefreshRate is the polling interval used while a view is active. Choose
// based on how often the resource changes in practice and the cost of the
// FetchPage API call. Page size is uniform (model.DefaultPageSize).
var Registry = map[string]ResourceMeta{
	"cloudrun":     {DAO: new(cloudrun.CloudRun), RefreshRate: 30 * time.Second},
	"cloudbuild":   {DAO: new(cloudbuild.CloudBuild), RefreshRate: 30 * time.Second},
	"buildhistory": {DAO: new(buildhistory.BuildHistory), RefreshRate: 5 * time.Second},
	"vms":          {DAO: new(vms.VMs), RefreshRate: 30 * time.Second},
	"secrets":      {DAO: new(secrets.Secrets), RefreshRate: 60 * time.Second},
}

// Aliases maps shorthand command names to canonical registry keys.
// Keep entries sorted for deterministic autocomplete ordering.
var Aliases = map[string]string{
	"cloudrun":     "cloudrun",
	"triggers":     "cloudbuild",
	"buildhistory": "buildhistory",
	"vms":          "vms",
	"secrets":      "secrets",
	"q":            "_quit",
	"quit":         "_quit",
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
