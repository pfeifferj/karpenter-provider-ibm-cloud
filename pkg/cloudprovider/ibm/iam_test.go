package ibm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/platform-services-go-sdk/iamidentityv1"
)

type mockIAMIdentityClient struct {
	createAPIKeyResponse *iamidentityv1.APIKey
	err                  error
}

func (m *mockIAMIdentityClient) CreateAPIKey(_ *iamidentityv1.CreateAPIKeyOptions) (*iamidentityv1.APIKey, *core.DetailedResponse, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.createAPIKeyResponse, &core.DetailedResponse{}, nil
}

func TestNewIAMClient(t *testing.T) {
	testKey := "test-api-key"
	client := NewIAMClient(testKey)

	if client == nil {
		t.Fatal("expected non-nil client")
		return
	}

	if client.apiKey != testKey {
		t.Errorf("expected API key %s, got %s", testKey, client.apiKey)
	}
	if client.token != "" {
		t.Error("expected empty token")
	}
	if !client.expiry.IsZero() {
		t.Error("expected zero expiry time")
	}
}

func TestGetToken(t *testing.T) {
	ctx := context.Background()
	testKey := "test-api-key"
	testToken := "test-token"

	tests := []struct {
		name           string
		existingToken  string
		existingExpiry time.Time
		mockClient     iamIdentityClientInterface
		wantToken      string
		wantErr        bool
	}{
		{
			name:           "valid cached token",
			existingToken:  "cached-token",
			existingExpiry: time.Now().Add(10 * time.Minute),
			wantToken:      "cached-token",
			wantErr:        false,
		},
		{
			name:           "expired token",
			existingToken:  "expired-token",
			existingExpiry: time.Now().Add(-10 * time.Minute),
			mockClient: &mockIAMIdentityClient{
				createAPIKeyResponse: &iamidentityv1.APIKey{
					Apikey: &testToken,
				},
			},
			wantToken: testToken,
			wantErr:   false,
		},
		{
			name: "successful new token",
			mockClient: &mockIAMIdentityClient{
				createAPIKeyResponse: &iamidentityv1.APIKey{
					Apikey: &testToken,
				},
			},
			wantToken: testToken,
			wantErr:   false,
		},
		{
			name: "API key creation error",
			mockClient: &mockIAMIdentityClient{
				err: errors.New("API key creation failed"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewIAMClient(testKey)
			client.token = tt.existingToken
			client.expiry = tt.existingExpiry

			if tt.mockClient != nil {
				client.client = tt.mockClient
			}

			token, err := client.GetToken(ctx)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				if token != "" {
					t.Error("expected empty token")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if token != tt.wantToken {
					t.Errorf("got token %s, want %s", token, tt.wantToken)
				}

				if tt.existingToken == "" {
					// Check that expiry is set correctly for new tokens
					expectedExpiry := time.Now().Add(55 * time.Minute)
					if client.expiry.Sub(expectedExpiry) > 2*time.Second {
						t.Errorf("expiry time not within expected range")
					}
				}
			}
		})
	}
}

func TestStringPtr(t *testing.T) {
	testStr := "test"
	ptr := stringPtr(testStr)

	if ptr == nil {
		t.Fatal("expected non-nil pointer")
		return
	}

	want := testStr
	got := *ptr
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
