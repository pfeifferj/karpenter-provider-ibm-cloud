package ibm

import (
	"context"
	"fmt"
	"time"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/platform-services-go-sdk/iamidentityv1"
)

// iamIdentityClientInterface defines the interface for the IAM Identity client
type iamIdentityClientInterface interface {
	CreateAPIKey(options *iamidentityv1.CreateAPIKeyOptions) (*iamidentityv1.APIKey, *core.DetailedResponse, error)
}

// IAMClient handles interactions with the IBM Cloud IAM API
type IAMClient struct {
	apiKey string
	client iamIdentityClientInterface
	token  string
	expiry time.Time
}

func NewIAMClient(apiKey string) *IAMClient {
	return &IAMClient{
		apiKey: apiKey,
	}
}

// GetToken returns a valid IAM token, fetching a new one if necessary
func (c *IAMClient) GetToken(ctx context.Context) (string, error) {
	// Check if we have a valid cached token
	if c.token != "" && time.Now().Before(c.expiry) {
		return c.token, nil
	}

	// Initialize the IAM client if needed
	if c.client == nil {
		options := &iamidentityv1.IamIdentityV1Options{}
		var err error
		client, err := iamidentityv1.NewIamIdentityV1UsingExternalConfig(options)
		if err != nil {
			return "", fmt.Errorf("initializing IAM client: %w", err)
		}
		c.client = client
	}

	// Create API key options
	options := &iamidentityv1.CreateAPIKeyOptions{
		Name:        stringPtr("karpenter-temp-key"),
		Description: stringPtr("Temporary API key for Karpenter global catalog access"),
		Apikey:      stringPtr(c.apiKey),
	}

	// Create API key
	apiKey, _, err := c.client.CreateAPIKey(options)
	if err != nil {
		return "", fmt.Errorf("creating API key: %w", err)
	}

	// Store token with expiry (tokens are valid for 1 hour)
	c.token = *apiKey.Apikey
	c.expiry = time.Now().Add(55 * time.Minute) // Refresh 5 minutes before expiry

	return c.token, nil
}

func stringPtr(s string) *string {
	return &s
}
