package model

import (
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
	"cloudrun": {DAO: new(dao.CloudRun), TTL: 60 * time.Second},
}

// Lookup returns the ResourceMeta for the given resource key.
// The second return value is false if the resource is not registered.
func Lookup(resource string) (ResourceMeta, bool) {
	m, ok := Registry[resource]
	return m, ok
}
