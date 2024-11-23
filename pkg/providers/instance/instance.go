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

package instance

import (
	"context"
	"fmt"
	"strings"

	"github.com/IBM/vpc-go-sdk/vpcv1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
)

type IBMCloudInstanceProvider struct {
	client *ibm.Client
}

func NewProvider() (*IBMCloudInstanceProvider, error) {
	client, err := ibm.NewClient()
	if err != nil {
		return nil, fmt.Errorf("creating IBM Cloud client: %w", err)
	}
	return &IBMCloudInstanceProvider{
		client: client,
	}, nil
}

func (p *IBMCloudInstanceProvider) Create(ctx context.Context, nodeClaim *v1.NodeClaim) (*corev1.Node, error) {
	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return nil, fmt.Errorf("getting VPC client: %w", err)
	}

	// Extract instance profile name from node claim
	instanceProfile := nodeClaim.Labels["node.kubernetes.io/instance-type"]
	if instanceProfile == "" {
		return nil, fmt.Errorf("instance profile not specified in node claim")
	}

	zone := nodeClaim.Labels["topology.kubernetes.io/zone"]
	if zone == "" {
		return nil, fmt.Errorf("zone not specified in node claim")
	}

	// Create instance prototype
	instancePrototype := &vpcv1.InstancePrototype{
		Name: &nodeClaim.Name,
		Zone: &vpcv1.ZoneIdentity{
			Name: &zone,
		},
		Profile: &vpcv1.InstanceProfileIdentity{
			Name: &instanceProfile,
		},
		// Add other required instance options like VPC, subnet, image, etc.
		// These would typically come from the node claim or configuration
	}

	instance, err := vpcClient.CreateInstance(ctx, instancePrototype)
	if err != nil {
		return nil, fmt.Errorf("creating instance: %w", err)
	}

	// Create node object
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nodeClaim.Name,
			Labels:      nodeClaim.Labels,
			Annotations: nodeClaim.Annotations,
		},
		Spec: corev1.NodeSpec{
			ProviderID: fmt.Sprintf("ibm://%s", *instance.ID),
		},
	}

	return node, nil
}

func (p *IBMCloudInstanceProvider) Delete(ctx context.Context, node *corev1.Node) error {
	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return fmt.Errorf("getting VPC client: %w", err)
	}

	// Extract instance ID from provider ID
	providerID := node.Spec.ProviderID
	if !strings.HasPrefix(providerID, "ibm://") {
		return fmt.Errorf("invalid provider ID format: %s", providerID)
	}
	instanceID := strings.TrimPrefix(providerID, "ibm://")

	err = vpcClient.DeleteInstance(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("deleting instance: %w", err)
	}

	return nil
}

func (p *IBMCloudInstanceProvider) GetInstance(ctx context.Context, node *corev1.Node) (*Instance, error) {
	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return nil, fmt.Errorf("getting VPC client: %w", err)
	}

	// Extract instance ID from provider ID
	providerID := node.Spec.ProviderID
	if !strings.HasPrefix(providerID, "ibm://") {
		return nil, fmt.Errorf("invalid provider ID format: %s", providerID)
	}
	instanceID := strings.TrimPrefix(providerID, "ibm://")

	instance, err := vpcClient.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("getting instance: %w", err)
	}

	// Set default tags
	userTags := map[string]string{
		"tag1": "value1",
		"tag2": "value2",
	}

	// Convert VPC instance to provider instance
	return &Instance{
		ID:           *instance.ID,
		Type:         *instance.Profile.Name,
		Zone:         *instance.Zone.Name,
		Region:       p.client.GetRegion(),
		CapacityType: "on-demand", // IBM Cloud currently only supports on-demand instances
		ImageID:      *instance.Image.ID,
		Tags:         userTags,
	}, nil
}
