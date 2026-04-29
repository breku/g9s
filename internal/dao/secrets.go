package dao

import (
	"context"
	"fmt"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
)

// Ensure Secrets satisfies Accessor at compile time.
var _ Accessor = (*Secrets)(nil)

// Secrets is the DAO for GCP Secret Manager secrets.
type Secrets struct{}

// Resource returns the stable identifier for this resource type.
func (s *Secrets) Resource() string { return "secrets" }

// Header returns the column headers for the Secrets table view.
func (s *Secrets) Header() []string {
	return []string{"NAME", "REPLICATION", "LABELS", "CREATED"}
}

// List fetches all secrets in the given project.
func (s *Secrets) List(ctx context.Context, project string) (*TableData, error) {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("secrets: credentials: %w", err)
	}

	client, err := secretmanager.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("secrets: new client: %w", err)
	}
	defer client.Close()

	req := &secretmanagerpb.ListSecretsRequest{
		Parent: fmt.Sprintf("projects/%s", project),
	}

	var rows []Row
	it := client.ListSecrets(ctx, req)
	for {
		secret, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("secrets: list: %w", err)
		}
		rows = append(rows, rowFromSecret(secret))
	}

	return &TableData{
		Header: s.Header(),
		Rows:   rows,
	}, nil
}

// rowFromSecret converts a Secret proto to a table Row.
func rowFromSecret(s *secretmanagerpb.Secret) Row {
	name := lastSegment(s.Name)
	created := "—"
	if s.CreateTime != nil {
		created = formatTime(s.CreateTime.AsTime())
	}

	replication := "—"
	if r := s.GetReplication(); r != nil {
		switch r.GetReplication().(type) {
		case *secretmanagerpb.Replication_Automatic_:
			replication = "Automatic"
		case *secretmanagerpb.Replication_UserManaged_:
			replication = "User-managed"
		}
	}

	labels := "—"
	if len(s.Labels) > 0 {
		labels = ""
		first := true
		for k, v := range s.Labels {
			if !first {
				labels += ", "
			}
			labels += k + "=" + v
			first = false
		}
	}

	return Row{
		ID:   s.Name, // projects/<p>/secrets/<name>
		Type: RowTypeActive,
		Meta: map[string]string{
			"name": name,
		},
		Columns: []Column{
			{Text: name},
			{Text: replication},
			{Text: labels},
			{Text: created},
		},
	}
}

// AccessLatestSecret fetches the plaintext payload of the latest enabled
// version of the given secret. Returns the raw bytes as a string.
//
// secretName must be the fully-qualified resource name:
//
//	projects/<project>/secrets/<name>
func AccessLatestSecret(ctx context.Context, secretName string) (string, error) {
	opts, err := gcp.ClientOptions(ctx)
	if err != nil {
		return "", fmt.Errorf("secrets: credentials: %w", err)
	}
	client, err := secretmanager.NewClient(ctx, opts...)
	if err != nil {
		return "", fmt.Errorf("secrets: new client: %w", err)
	}
	defer client.Close()

	resp, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: secretName + "/versions/latest",
	})
	if err != nil {
		return "", fmt.Errorf("secrets: access latest: %w", err)
	}
	return string(resp.Payload.Data), nil
}
