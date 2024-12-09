package ibm

import (
	"context"
	"fmt"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"
)

// vpcClientInterface defines the interface for the VPC client
type vpcClientInterface interface {
	CreateInstanceWithContext(context.Context, *vpcv1.CreateInstanceOptions) (*vpcv1.Instance, *core.DetailedResponse, error)
	DeleteInstanceWithContext(context.Context, *vpcv1.DeleteInstanceOptions) (*core.DetailedResponse, error)
	GetInstanceWithContext(context.Context, *vpcv1.GetInstanceOptions) (*vpcv1.Instance, *core.DetailedResponse, error)
	ListInstancesWithContext(context.Context, *vpcv1.ListInstancesOptions) (*vpcv1.InstanceCollection, *core.DetailedResponse, error)
	UpdateInstanceWithContext(context.Context, *vpcv1.UpdateInstanceOptions) (*vpcv1.Instance, *core.DetailedResponse, error)
}

// VPCClient handles interactions with the IBM Cloud VPC API
type VPCClient struct {
	baseURL  string
	authType string
	apiKey   string
	region   string
	client   vpcClientInterface
}

func NewVPCClient(baseURL, authType, apiKey, region string) *VPCClient {
	return &VPCClient{
		baseURL:  baseURL,
		authType: authType,
		apiKey:   apiKey,
		region:   region,
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

func (c *VPCClient) UpdateInstanceTags(ctx context.Context, id string, tags map[string]string) error {
	if c.client == nil {
		return fmt.Errorf("VPC client not initialized")
	}

	// Convert tags map to patch data
	patchData := make(map[string]interface{})
	tagsList := make([]string, 0, len(tags))
	for key, value := range tags {
		tagsList = append(tagsList, fmt.Sprintf("%s:%s", key, value))
	}
	patchData["user_tags"] = tagsList

	options := &vpcv1.UpdateInstanceOptions{
		ID:            &id,
		InstancePatch: patchData,
	}

	_, _, err := c.client.UpdateInstanceWithContext(ctx, options)
	if err != nil {
		return fmt.Errorf("updating instance tags: %w", err)
	}

	return nil
}
