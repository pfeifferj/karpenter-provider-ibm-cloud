package ibm

// GlobalCatalogClient handles interactions with the IBM Cloud Global Catalog API
type GlobalCatalogClient struct {
	authType string
	apiKey  string
}

func NewGlobalCatalogClient(authType, apiKey string) *GlobalCatalogClient {
	return &GlobalCatalogClient{
		authType: authType,
		apiKey:   apiKey,
	}
}

// TODO: Add methods for Global Catalog operations like:
// - GetInstanceType
// - ListInstanceTypes
