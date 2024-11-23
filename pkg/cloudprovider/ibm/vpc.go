package ibm

// VPCClient handles interactions with the IBM Cloud VPC API
type VPCClient struct {
	baseURL   string
	authType  string
	apiKey    string
	region    string
}

func NewVPCClient(baseURL, authType, apiKey, region string) *VPCClient {
	return &VPCClient{
		baseURL:   baseURL,
		authType:  authType,
		apiKey:    apiKey,
		region:    region,
	}
}

// TODO: Add methods for VPC operations like:
// - CreateInstance
// - DeleteInstance
// - GetInstance
// - ListInstances
