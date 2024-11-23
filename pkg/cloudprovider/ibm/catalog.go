package ibm

import (
	"context"
	"fmt"

	"github.com/IBM/platform-services-go-sdk/globalcatalogv1"
)

// GlobalCatalogClient handles interactions with the IBM Cloud Global Catalog API
type GlobalCatalogClient struct {
	authType string
	apiKey   string
	client   *globalcatalogv1.GlobalCatalogV1
}

func NewGlobalCatalogClient(authType, apiKey string) *GlobalCatalogClient {
	return &GlobalCatalogClient{
		authType: authType,
		apiKey:   apiKey,
	}
}

func (c *GlobalCatalogClient) GetInstanceType(ctx context.Context, id string) (*globalcatalogv1.CatalogEntry, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Global Catalog client not initialized")
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
	if c.client == nil {
		return nil, fmt.Errorf("Global Catalog client not initialized")
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
