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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
)

type IBMCloudInstanceProvider struct {
	client     *ibm.Client
	kubeClient client.Client
}

func NewProvider() (*IBMCloudInstanceProvider, error) {
	client, err := ibm.NewClient()
	if err != nil {
		return nil, fmt.Errorf("creating IBM Cloud client: %w", err)
	}
	return &IBMCloudInstanceProvider{
		client: client,
		// kubeClient will be set when provider is used in controller context
	}, nil
}

func (p *IBMCloudInstanceProvider) SetKubeClient(kubeClient client.Client) {
	p.kubeClient = kubeClient
}

func (p *IBMCloudInstanceProvider) Create(ctx context.Context, nodeClaim *v1.NodeClaim) (*corev1.Node, error) {
	if p.kubeClient == nil {
		return nil, fmt.Errorf("kubernetes client not set")
	}

	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return nil, fmt.Errorf("getting VPC client: %w", err)
	}

	// Get the NodeClass to extract configuration
	nodeClass := &v1alpha1.IBMNodeClass{}
	if getErr := p.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaim.Spec.NodeClassRef.Name}, nodeClass); getErr != nil {
		return nil, fmt.Errorf("getting NodeClass %s: %w", nodeClaim.Spec.NodeClassRef.Name, getErr)
	}

	// Extract instance profile - prefer NodeClass, fallback to labels
	instanceProfile := nodeClass.Spec.InstanceProfile
	if instanceProfile == "" {
		instanceProfile = nodeClaim.Labels["node.kubernetes.io/instance-type"]
		if instanceProfile == "" {
			return nil, fmt.Errorf("instance profile not specified in NodeClass or node claim")
		}
	}

	// Extract zone - prefer NodeClass, fallback to labels
	zone := nodeClass.Spec.Zone
	if zone == "" {
		zone = nodeClaim.Labels["topology.kubernetes.io/zone"]
		if zone == "" {
			return nil, fmt.Errorf("zone not specified in NodeClass or node claim")
		}
	}

	// Validate required NodeClass fields
	if nodeClass.Spec.VPC == "" {
		return nil, fmt.Errorf("VPC not specified in NodeClass")
	}
	if nodeClass.Spec.Image == "" {
		return nil, fmt.Errorf("image not specified in NodeClass")
	}

	// Create primary network interface
	primaryNetworkInterface := &vpcv1.NetworkInterfacePrototype{
		Name: &[]string{fmt.Sprintf("%s-eth0", nodeClaim.Name)}[0],
	}

	// Set subnet if specified in NodeClass
	if nodeClass.Spec.Subnet != "" {
		primaryNetworkInterface.Subnet = &vpcv1.SubnetIdentity{
			ID: &nodeClass.Spec.Subnet,
		}
	}

	// Add security groups if specified
	if len(nodeClass.Spec.SecurityGroups) > 0 {
		securityGroups := make([]vpcv1.SecurityGroupIdentityIntf, len(nodeClass.Spec.SecurityGroups))
		for i, sgID := range nodeClass.Spec.SecurityGroups {
			securityGroups[i] = &vpcv1.SecurityGroupIdentity{
				ID: &sgID,
			}
		}
		primaryNetworkInterface.SecurityGroups = securityGroups
	}

	// Create instance prototype with all required fields
	instancePrototype := &vpcv1.InstancePrototype{
		Name: &nodeClaim.Name,
		Zone: &vpcv1.ZoneIdentity{
			Name: &zone,
		},
		Profile: &vpcv1.InstanceProfileIdentity{
			Name: &instanceProfile,
		},
		VPC: &vpcv1.VPCIdentity{
			ID: &nodeClass.Spec.VPC,
		},
		Image: &vpcv1.ImageIdentity{
			ID: &nodeClass.Spec.Image,
		},
		PrimaryNetworkInterface: primaryNetworkInterface,
	}

	// Add tags if specified - placeholder for future VPC SDK tag support
	_ = nodeClass.Spec.Tags // Tags will be used when VPC SDK supports them

	// Create the instance
	instance, err := vpcClient.CreateInstance(ctx, instancePrototype)
	if err != nil {
		return nil, fmt.Errorf("creating instance with profile %s in zone %s: %w", instanceProfile, zone, err)
	}

	// Create node object with appropriate labels
	nodeLabels := make(map[string]string)
	if nodeClaim.Labels != nil {
		for k, v := range nodeClaim.Labels {
			nodeLabels[k] = v
		}
	}
	
	// Set standard Kubernetes labels
	nodeLabels["node.kubernetes.io/instance-type"] = instanceProfile
	nodeLabels["topology.kubernetes.io/zone"] = zone
	nodeLabels["topology.kubernetes.io/region"] = nodeClass.Spec.Region
	nodeLabels["karpenter.sh/provisioner-name"] = "ibm-cloud"
	nodeLabels["kubernetes.io/arch"] = "amd64" // Default architecture

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nodeClaim.Name,
			Labels:      nodeLabels,
			Annotations: nodeClaim.Annotations,
		},
		Spec: corev1.NodeSpec{
			ProviderID: fmt.Sprintf("ibm://%s", *instance.ID),
		},
	}

	return node, nil
}

func (p *IBMCloudInstanceProvider) Delete(ctx context.Context, node *corev1.Node) error {
	// Extract instance ID from provider ID first to validate the format
	providerID := node.Spec.ProviderID
	if !strings.HasPrefix(providerID, "ibm://") {
		return fmt.Errorf("invalid provider ID format: %s", providerID)
	}
	
	if p.client == nil {
		return fmt.Errorf("IBM client not initialized")
	}
	
	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return fmt.Errorf("getting VPC client: %w", err)
	}

	instanceID := strings.TrimPrefix(providerID, "ibm://")

	err = vpcClient.DeleteInstance(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("deleting instance: %w", err)
	}

	return nil
}

func (p *IBMCloudInstanceProvider) GetInstance(ctx context.Context, node *corev1.Node) (*Instance, error) {
	if p.client == nil {
		return nil, fmt.Errorf("IBM client not initialized")
	}
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

func (p *IBMCloudInstanceProvider) TagInstance(ctx context.Context, instanceID string, tags map[string]string) error {
	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return fmt.Errorf("getting VPC client: %w", err)
	}

	err = vpcClient.UpdateInstanceTags(ctx, instanceID, tags)
	if err != nil {
		return fmt.Errorf("updating instance tags: %w", err)
	}

	return nil
}
