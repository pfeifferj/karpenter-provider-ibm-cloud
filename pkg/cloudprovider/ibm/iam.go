package ibm

import (
	"context"
	"fmt"
	"time"

	"github.com/IBM/go-sdk-core/v5/core"
)

// TokenResponse represents the response from a token request
type TokenResponse struct {
	AccessToken string
	ExpiresIn   int64
}

// Authenticator defines the interface for token management
type Authenticator interface {
	RequestToken() (*TokenResponse, error)
}

// iamAuthenticator wraps the IBM authenticator to match our interface
type iamAuthenticator struct {
	auth *core.IamAuthenticator
}

func (a *iamAuthenticator) RequestToken() (*TokenResponse, error) {
	token, err := a.auth.RequestToken()
	if err != nil {
		return nil, err
	}
	return &TokenResponse{
		AccessToken: token.AccessToken,
		ExpiresIn:   token.ExpiresIn,
	}, nil
}

// IAMClient handles interactions with the IBM Cloud IAM API
type IAMClient struct {
	apiKey        string
	Authenticator Authenticator // Exported for testing
	token         string
	expiry        time.Time
}

func NewIAMClient(apiKey string) *IAMClient {
	return &IAMClient{
		apiKey: apiKey,
		Authenticator: &iamAuthenticator{
			auth: &core.IamAuthenticator{
				ApiKey: apiKey,
			},
		},
	}
}

// GetToken returns a valid IAM token, fetching a new one if necessary
func (c *IAMClient) GetToken(ctx context.Context) (string, error) {
	// Check if we have a valid cached token
	if c.token != "" && time.Now().Before(c.expiry) {
		return c.token, nil
	}

	// Get token from authenticator
	tokenResponse, err := c.Authenticator.RequestToken()
	if err != nil {
		return "", fmt.Errorf("getting IAM token: %w", err)
	}

	// Store token with expiry
	c.token = tokenResponse.AccessToken
	// Convert expiration to time.Duration (expires_in is in seconds)
	expiresIn := time.Duration(tokenResponse.ExpiresIn-300) * time.Second // Refresh 5 minutes before expiry
	c.expiry = time.Now().Add(expiresIn)

	return c.token, nil
}
