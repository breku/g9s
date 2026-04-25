// Package gcp provides GCP client helpers using Application Default Credentials.
package gcp

import (
	"context"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
)

// ClientOptions returns common gRPC/HTTP client options using ADC.
func ClientOptions(ctx context.Context) ([]option.ClientOption, error) {
	creds, err := google.FindDefaultCredentials(ctx)
	if err != nil {
		return nil, err
	}
	return []option.ClientOption{option.WithCredentials(creds)}, nil
}
