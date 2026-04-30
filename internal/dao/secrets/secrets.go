// Package secrets provides the DAO for GCP Secret Manager secrets.
package secrets

import (
	"context"
	"fmt"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/brekol/g9s/internal/dao"
	"github.com/brekol/g9s/internal/gcp"
	"google.golang.org/api/iterator"
)

// Ensure Secrets satisfies Accessor at compile time.
var _ dao.Accessor = (*Secrets)(nil)

// Secrets is the DAO for GCP Secret Manager secrets.
type Secrets struct{}

// Resource returns the stable identifier for this resource type.
func (s *Secrets) Resource() string { return "secrets" }

// Header returns the column headers for the Secrets table view.
func (s *Secrets) Header() []string {
	return []string{"NAME", "REPLICATION", "LABELS", "CREATED"}
}

// List fetches all secrets in the given project.
func (s *Secrets) List(ctx context.Context, project string) (*dao.TableData, error) {
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

	var rows []dao.Row
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

	return &dao.TableData{
		Header: s.Header(),
		Rows:   rows,
	}, nil
}

func rowFromSecret(s *secretmanagerpb.Secret) dao.Row {
	name := dao.LastSegment(s.Name)
	created := "—"
	if s.CreateTime != nil {
		created = dao.FormatTime(s.CreateTime.AsTime())
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

	return dao.Row{
		ID:   s.Name,
		Type: dao.RowTypeActive,
		Meta: map[string]string{
			"name": name,
		},
		Columns: []dao.Column{
			{Text: name},
			{Text: replication},
			{Text: labels},
			{Text: created},
		},
	}
}

// AccessLatestSecret fetches the plaintext payload of the latest enabled
// version of the given secret.
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
