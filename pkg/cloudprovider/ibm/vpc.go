/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
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
	ListSubnetsWithContext(context.Context, *vpcv1.ListSubnetsOptions) (*vpcv1.SubnetCollection, *core.DetailedResponse, error)
	GetSubnetWithContext(context.Context, *vpcv1.GetSubnetOptions) (*vpcv1.Subnet, *core.DetailedResponse, error)
	GetVPCWithContext(context.Context, *vpcv1.GetVPCOptions) (*vpcv1.VPC, *core.DetailedResponse, error)
	GetImageWithContext(context.Context, *vpcv1.GetImageOptions) (*vpcv1.Image, *core.DetailedResponse, error)
	ListImagesWithContext(context.Context, *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error)
	ListInstanceProfilesWithContext(context.Context, *vpcv1.ListInstanceProfilesOptions) (*vpcv1.InstanceProfileCollection, *core.DetailedResponse, error)
	ListSecurityGroupsWithContext(context.Context, *vpcv1.ListSecurityGroupsOptions) (*vpcv1.SecurityGroupCollection, *core.DetailedResponse, error)
}

// VPCClient handles interactions with the IBM Cloud VPC API
type VPCClient struct {
	baseURL  string
	authType string
	apiKey   string
	region   string
	client   vpcClientInterface
}

func NewVPCClient(baseURL, authType, apiKey, region string) (*VPCClient, error) {
	authenticator := &core.IamAuthenticator{
		ApiKey: apiKey,
	}

	options := &vpcv1.VpcV1Options{
		Authenticator: authenticator,
		URL:           baseURL,
	}

	client, err := vpcv1.NewVpcV1(options)
	if err != nil {
		return nil, fmt.Errorf("creating VPC client: %w", err)
	}

	return &VPCClient{
		baseURL:  baseURL,
		authType: authType,
		apiKey:   apiKey,
		region:   region,
		client:   client,
	}, nil
}

// NewVPCClientWithMock creates a VPC client with a mock SDK client for testing
func NewVPCClientWithMock(mockClient vpcClientInterface) *VPCClient {
	return &VPCClient{
		baseURL:  "test",
		authType: "test",
		apiKey:   "test",
		region:   "test",
		client:   mockClient,
	}
}

func (c *VPCClient) CreateInstance(ctx context.Context, instancePrototype vpcv1.InstancePrototypeIntf) (*vpcv1.Instance, error) {
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

func (c *VPCClient) ListSubnets(ctx context.Context, vpcID string) (*vpcv1.SubnetCollection, error) {
	if c.client == nil {
		return nil, fmt.Errorf("VPC client not initialized")
	}

	options := &vpcv1.ListSubnetsOptions{
		VPCID: &vpcID,
	}

	subnets, _, err := c.client.ListSubnetsWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("listing subnets: %w", err)
	}

	return subnets, nil
}

func (c *VPCClient) GetSubnet(ctx context.Context, subnetID string) (*vpcv1.Subnet, error) {
	if c.client == nil {
		return nil, fmt.Errorf("VPC client not initialized")
	}

	options := &vpcv1.GetSubnetOptions{
		ID: &subnetID,
	}

	subnet, _, err := c.client.GetSubnetWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("getting subnet: %w", err)
	}

	return subnet, nil
}

func (c *VPCClient) GetVPC(ctx context.Context, vpcID string) (*vpcv1.VPC, error) {
	if c.client == nil {
		return nil, fmt.Errorf("VPC client not initialized")
	}

	options := &vpcv1.GetVPCOptions{
		ID: &vpcID,
	}

	vpc, _, err := c.client.GetVPCWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("getting VPC: %w", err)
	}

	return vpc, nil
}

func (c *VPCClient) GetImage(ctx context.Context, imageID string) (*vpcv1.Image, error) {
	if c.client == nil {
		return nil, fmt.Errorf("VPC client not initialized")
	}

	options := &vpcv1.GetImageOptions{
		ID: &imageID,
	}

	image, _, err := c.client.GetImageWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("getting image: %w", err)
	}

	return image, nil
}

// ListImages lists available images with optional filtering
func (c *VPCClient) ListImages(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, error) {
	if c.client == nil {
		return nil, fmt.Errorf("VPC client not initialized")
	}

	images, _, err := c.client.ListImagesWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}

	return images, nil
}

// ListSecurityGroups lists security groups with optional filtering
func (c *VPCClient) ListSecurityGroupsWithContext(ctx context.Context, options *vpcv1.ListSecurityGroupsOptions) (*vpcv1.SecurityGroupCollection, *core.DetailedResponse, error) {
	if c.client == nil {
		return nil, nil, fmt.Errorf("VPC client not initialized")
	}

	return c.client.ListSecurityGroupsWithContext(ctx, options)
}

// ListSubnetsWithContext lists subnets with context
func (c *VPCClient) ListSubnetsWithContext(ctx context.Context, options *vpcv1.ListSubnetsOptions) (*vpcv1.SubnetCollection, *core.DetailedResponse, error) {
	if c.client == nil {
		return nil, nil, fmt.Errorf("VPC client not initialized")
	}

	return c.client.ListSubnetsWithContext(ctx, options)
}

// ListInstanceProfiles lists available instance profiles
func (c *VPCClient) ListInstanceProfiles(options *vpcv1.ListInstanceProfilesOptions) (*vpcv1.InstanceProfileCollection, *core.DetailedResponse, error) {
	if c.client == nil {
		return nil, nil, fmt.Errorf("VPC client not initialized")
	}

	return c.client.ListInstanceProfilesWithContext(context.Background(), options)
}
