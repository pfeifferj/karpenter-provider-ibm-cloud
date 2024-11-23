package ibm

import (
	"context"
	"fmt"

	"github.com/IBM/vpc-go-sdk/vpcv1"
)

// VPCClient handles interactions with the IBM Cloud VPC API
type VPCClient struct {
	baseURL   string
	authType  string
	apiKey    string
	region    string
	client    *vpcv1.VpcV1
}

func NewVPCClient(baseURL, authType, apiKey, region string) *VPCClient {
	return &VPCClient{
		baseURL:   baseURL,
		authType:  authType,
		apiKey:    apiKey,
		region:    region,
	}
}

func (c *VPCClient) CreateInstance(ctx context.Context, instancePrototype *vpcv1.InstancePrototype) (*vpcv1.Instance, error) {
	if c.client == nil {
		return nil, fmt.Errorf("VPC client not initialized")
	}

	options := &vpcv1.CreateInstanceOptions{
		InstancePrototype: instancePrototype,
	}

	instance, _, err := c.client.CreateInstanceWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("creating instance: %w", err)
	}

	return instance, nil
}

func (c *VPCClient) DeleteInstance(ctx context.Context, id string) error {
	if c.client == nil {
		return fmt.Errorf("VPC client not initialized")
	}

	options := &vpcv1.DeleteInstanceOptions{
		ID: &id,
	}

	_, err := c.client.DeleteInstanceWithContext(ctx, options)
	if err != nil {
		return fmt.Errorf("deleting instance: %w", err)
	}

	return nil
}

func (c *VPCClient) GetInstance(ctx context.Context, id string) (*vpcv1.Instance, error) {
	if c.client == nil {
		return nil, fmt.Errorf("VPC client not initialized")
	}

	options := &vpcv1.GetInstanceOptions{
		ID: &id,
	}

	instance, _, err := c.client.GetInstanceWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("getting instance: %w", err)
	}

	return instance, nil
}

func (c *VPCClient) ListInstances(ctx context.Context) ([]vpcv1.Instance, error) {
	if c.client == nil {
		return nil, fmt.Errorf("VPC client not initialized")
	}

	options := &vpcv1.ListInstancesOptions{}

	instances, _, err := c.client.ListInstancesWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}

	return instances.Instances, nil
}
