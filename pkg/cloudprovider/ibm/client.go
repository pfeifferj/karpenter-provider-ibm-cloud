package ibm

import (
	"fmt"
	"os"
)

// Client represents an IBM Cloud API client
type Client struct {
	vpcURL      string
	vpcAuthType string
	vpcAPIKey   string
	ibmAPIKey   string
	region      string
	iamClient   *IAMClient
}

// NewClient creates a new IBM Cloud client using environment variables
func NewClient() (*Client, error) {
	vpcURL := os.Getenv("VPC_URL")
	if vpcURL == "" {
		vpcURL = "https://us-south.iaas.cloud.ibm.com/v1" // default value
	}

	vpcAuthType := os.Getenv("VPC_AUTH_TYPE")
	if vpcAuthType == "" {
		vpcAuthType = "iam" // default value
	}

	vpcAPIKey := os.Getenv("VPC_APIKEY")
	if vpcAPIKey == "" {
		return nil, fmt.Errorf("VPC_APIKEY environment variable is required")
	}

	ibmAPIKey := os.Getenv("IBM_API_KEY")
	if ibmAPIKey == "" {
		return nil, fmt.Errorf("IBM_API_KEY environment variable is required")
	}

	region := os.Getenv("IBM_REGION")
	if region == "" {
		return nil, fmt.Errorf("IBM_REGION environment variable is required")
	}

	client := &Client{
		vpcURL:      vpcURL,
		vpcAuthType: vpcAuthType,
		vpcAPIKey:   vpcAPIKey,
		ibmAPIKey:   ibmAPIKey,
		region:      region,
	}

	// Initialize the IAM client
	client.iamClient = NewIAMClient(ibmAPIKey)

	return client, nil
}

// GetVPCClient returns a configured VPC API client
func (c *Client) GetVPCClient() (*VPCClient, error) {
	return NewVPCClient(c.vpcURL, c.vpcAuthType, c.vpcAPIKey, c.region), nil
}

// GetGlobalCatalogClient returns a configured Global Catalog API client
func (c *Client) GetGlobalCatalogClient() (*GlobalCatalogClient, error) {
	return NewGlobalCatalogClient(c.iamClient), nil
}

// GetRegion returns the configured region
func (c *Client) GetRegion() string {
	return c.region
}
