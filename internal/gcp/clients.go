package gcp

import (
	"context"
	"sync"

	cloudbuild "cloud.google.com/go/cloudbuild/apiv1/v2"
	compute "cloud.google.com/go/compute/apiv1"
	logging "cloud.google.com/go/logging/apiv2"
	run "cloud.google.com/go/run/apiv2"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/storage"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
)

// This file holds a process-wide pool of cached GCP clients. Each accessor
// constructs its client lazily on first call and returns the same pointer
// for the lifetime of the process. Errors from construction are cached too,
// so a misconfigured ADC fails fast and consistently rather than retrying
// on every poll tick.
//
// Why globals: g9s is a single-process, single-credential TUI. The DAO
// types are stateless empty structs by design; threading a client through
// every constructor would force fallible view constructors and add no
// testability benefit (no fakes available for these clients).
//
// Lifetime: clients live until process exit. No Close() — the OS reclaims
// gRPC connections when the process terminates. This is the same pattern
// k9s uses for its kubeclient.
//
// Context: client construction uses context.Background() so a per-view
// ctx cancellation (e.g. switching views) doesn't tear down the cached
// client. The caller's ctx is still passed to the actual RPC call, so
// in-flight cancellation semantics are preserved.

// gcpClient is a generic once-and-cache cell for a GCP client of type T.
// The first call to get() runs make(); every subsequent call returns the
// memoised value/error pair.
type gcpClient[T any] struct {
	once sync.Once
	val  T
	err  error
}

func (c *gcpClient[T]) get(make func() (T, error)) (T, error) {
	c.once.Do(func() { c.val, c.err = make() })
	return c.val, c.err
}

var (
	credsOnce sync.Once
	credsVal  *google.Credentials
	credsErr  error
)

// defaultCreds returns ADC credentials, cached for the process lifetime.
func defaultCreds() (*google.Credentials, error) {
	credsOnce.Do(func() {
		credsVal, credsErr = google.FindDefaultCredentials(context.Background())
	})
	return credsVal, credsErr
}

func credsOption() ([]option.ClientOption, error) {
	creds, err := defaultCreds()
	if err != nil {
		return nil, err
	}
	return []option.ClientOption{option.WithCredentials(creds)}, nil
}

var (
	cloudBuild         gcpClient[*cloudbuild.Client]
	cloudRun           gcpClient[*run.ServicesClient]
	computeInstances   gcpClient[*compute.InstancesClient]
	instanceGroupMgrs  gcpClient[*compute.InstanceGroupManagersClient]
	regionInstanceGMgr gcpClient[*compute.RegionInstanceGroupManagersClient]
	secretManager      gcpClient[*secretmanager.Client]
	cloudLogging       gcpClient[*logging.Client]
	gcs                gcpClient[*storage.Client]
)

// CloudBuildClient returns the process-wide cached Cloud Build client.
func CloudBuildClient() (*cloudbuild.Client, error) {
	return cloudBuild.get(func() (*cloudbuild.Client, error) {
		opts, err := credsOption()
		if err != nil {
			return nil, err
		}
		return cloudbuild.NewClient(context.Background(), opts...)
	})
}

// RunServicesClient returns the process-wide cached Cloud Run services client.
func RunServicesClient() (*run.ServicesClient, error) {
	return cloudRun.get(func() (*run.ServicesClient, error) {
		opts, err := credsOption()
		if err != nil {
			return nil, err
		}
		return run.NewServicesClient(context.Background(), opts...)
	})
}

// ComputeInstancesClient returns the process-wide cached Compute Instances client.
func ComputeInstancesClient() (*compute.InstancesClient, error) {
	return computeInstances.get(func() (*compute.InstancesClient, error) {
		opts, err := credsOption()
		if err != nil {
			return nil, err
		}
		return compute.NewInstancesRESTClient(context.Background(), opts...)
	})
}

// InstanceGroupManagersClient returns the process-wide cached zonal Managed
// Instance Group client. Used together with RegionInstanceGroupManagersClient
// to cover both zonal and regional MIGs (each have their own API surface; the
// aggregated list lives on the zonal client).
func InstanceGroupManagersClient() (*compute.InstanceGroupManagersClient, error) {
	return instanceGroupMgrs.get(func() (*compute.InstanceGroupManagersClient, error) {
		opts, err := credsOption()
		if err != nil {
			return nil, err
		}
		return compute.NewInstanceGroupManagersRESTClient(context.Background(), opts...)
	})
}

// RegionInstanceGroupManagersClient returns the process-wide cached regional
// Managed Instance Group client. Used by DescribeYAML to fetch a single
// regional MIG; aggregated listing comes from InstanceGroupManagersClient.
func RegionInstanceGroupManagersClient() (*compute.RegionInstanceGroupManagersClient, error) {
	return regionInstanceGMgr.get(func() (*compute.RegionInstanceGroupManagersClient, error) {
		opts, err := credsOption()
		if err != nil {
			return nil, err
		}
		return compute.NewRegionInstanceGroupManagersRESTClient(context.Background(), opts...)
	})
}

// SecretManagerClient returns the process-wide cached Secret Manager client.
func SecretManagerClient() (*secretmanager.Client, error) {
	return secretManager.get(func() (*secretmanager.Client, error) {
		opts, err := credsOption()
		if err != nil {
			return nil, err
		}
		return secretmanager.NewClient(context.Background(), opts...)
	})
}

// LoggingClient returns the process-wide cached Cloud Logging client.
func LoggingClient() (*logging.Client, error) {
	return cloudLogging.get(func() (*logging.Client, error) {
		opts, err := credsOption()
		if err != nil {
			return nil, err
		}
		return logging.NewClient(context.Background(), opts...)
	})
}

// StorageClient returns the process-wide cached GCS client.
func StorageClient() (*storage.Client, error) {
	return gcs.get(func() (*storage.Client, error) {
		opts, err := credsOption()
		if err != nil {
			return nil, err
		}
		return storage.NewClient(context.Background(), opts...)
	})
}
