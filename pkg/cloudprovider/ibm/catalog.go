package ibm

import (
	"context"
	"fmt"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/platform-services-go-sdk/globalcatalogv1"
)

// iamClientInterface defines the interface for the IAM client
type iamClientInterface interface {
	GetToken(context.Context) (string, error)
}

// globalCatalogClientInterface defines the interface for the Global Catalog client
type globalCatalogClientInterface interface {
	GetCatalogEntryWithContext(context.Context, *globalcatalogv1.GetCatalogEntryOptions) (*globalcatalogv1.CatalogEntry, *core.DetailedResponse, error)
	ListCatalogEntriesWithContext(context.Context, *globalcatalogv1.ListCatalogEntriesOptions) (*globalcatalogv1.EntrySearchResult, *core.DetailedResponse, error)
}

// GlobalCatalogClient handles interactions with the IBM Cloud Global Catalog API
type GlobalCatalogClient struct {
	iamClient iamClientInterface
	client    globalCatalogClientInterface
}

func NewGlobalCatalogClient(iamClient *IAMClient) *GlobalCatalogClient {
	return &GlobalCatalogClient{
		iamClient: iamClient,
	}
}

func (c *GlobalCatalogClient) ensureClient(ctx context.Context) error {
	if c.client != nil {
		return nil
	}

	// Get a fresh token
	token, err := c.iamClient.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("getting IAM token: %w", err)
	}

	// Initialize the Global Catalog client with the token
	authenticator := &core.BearerTokenAuthenticator{
		BearerToken: token,
	}

	options := &globalcatalogv1.GlobalCatalogV1Options{
		Authenticator: authenticator,
	}

	client, err := globalcatalogv1.NewGlobalCatalogV1(options)
	if err != nil {
		return fmt.Errorf("initializing Global Catalog client: %w", err)
	}

	c.client = client
	return nil
}

func (c *GlobalCatalogClient) GetInstanceType(ctx context.Context, id string) (*globalcatalogv1.CatalogEntry, error) {
	if err := c.ensureClient(ctx); err != nil {
		return nil, err
	}

	options := &globalcatalogv1.GetCatalogEntryOptions{
		ID: &id,
	}

	entry, _, err := c.client.GetCatalogEntryWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("getting catalog entry: %w", err)
	}

	return entry, nil
}

func (c *GlobalCatalogClient) ListInstanceTypes(ctx context.Context) ([]globalcatalogv1.CatalogEntry, error) {
	if err := c.ensureClient(ctx); err != nil {
		return nil, err
	}

	// Filter for instance profiles
	q := "kind:instance-profile"
	includeStr := "metadata"

	options := &globalcatalogv1.ListCatalogEntriesOptions{
		Q:       &q,
		Include: &includeStr,
	}

	entries, _, err := c.client.ListCatalogEntriesWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("listing catalog entries: %w", err)
	}

	return entries.Resources, nil
}
