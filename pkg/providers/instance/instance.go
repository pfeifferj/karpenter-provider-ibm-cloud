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
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/image"
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

	// Set subnet - either from NodeClass or auto-select from VPC
	var subnetID string
	if nodeClass.Spec.Subnet != "" {
		subnetID = nodeClass.Spec.Subnet
	} else {
		// Auto-select subnet from VPC in the specified zone
		subnet, subnetErr := p.selectSubnetForZone(ctx, vpcClient, nodeClass.Spec.VPC, zone)
		if subnetErr != nil {
			return nil, fmt.Errorf("auto-selecting subnet for zone %s: %w", zone, subnetErr)
		}
		subnetID = *subnet.ID
	}
	
	primaryNetworkInterface.Subnet = &vpcv1.SubnetIdentity{
		ID: &subnetID,
	}

	// Add security groups - either from NodeClass or VPC default
	if len(nodeClass.Spec.SecurityGroups) > 0 {
		securityGroups := make([]vpcv1.SecurityGroupIdentityIntf, len(nodeClass.Spec.SecurityGroups))
		for i, sgID := range nodeClass.Spec.SecurityGroups {
			securityGroups[i] = &vpcv1.SecurityGroupIdentity{
				ID: &sgID,
			}
		}
		primaryNetworkInterface.SecurityGroups = securityGroups
	} else {
		// Use VPC default security group if none specified
		defaultSG, sgErr := p.getDefaultSecurityGroup(ctx, vpcClient, nodeClass.Spec.VPC)
		if sgErr != nil {
			return nil, fmt.Errorf("getting default security group for VPC %s: %w", nodeClass.Spec.VPC, sgErr)
		}
		primaryNetworkInterface.SecurityGroups = []vpcv1.SecurityGroupIdentityIntf{
			&vpcv1.SecurityGroupIdentity{ID: defaultSG.ID},
		}
	}

	// Resolve image identifier to image ID
	imageResolver := image.NewResolver(vpcClient, nodeClass.Spec.Region)
	imageID, err := imageResolver.ResolveImage(ctx, nodeClass.Spec.Image)
	if err != nil {
		return nil, fmt.Errorf("resolving image %s: %w", nodeClass.Spec.Image, err)
	}

	// Create boot volume attachment for the instance
	bootVolumeAttachment := &vpcv1.VolumeAttachmentPrototypeInstanceByImageContext{
		Volume: &vpcv1.VolumePrototypeInstanceByImageContext{
			Name: &[]string{fmt.Sprintf("%s-boot", nodeClaim.Name)}[0],
			Profile: &vpcv1.VolumeProfileIdentity{
				Name: &[]string{"general-purpose"}[0], // Default boot volume profile
			},
			Capacity: &[]int64{100}[0], // Default 100GB boot volume
		},
		DeleteVolumeOnInstanceDelete: &[]bool{true}[0],
	}

	// Create instance prototype with all required fields using InstancePrototypeInstanceByImage
	// This satisfies the oneOf constraint in the IBM Cloud VPC API
	instancePrototype := &vpcv1.InstancePrototypeInstanceByImage{
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
			ID: &imageID,
		},
		PrimaryNetworkInterface: primaryNetworkInterface,
		BootVolumeAttachment:    bootVolumeAttachment,
	}

	// Add placement target if specified
	if nodeClass.Spec.PlacementTarget != "" {
		instancePrototype.PlacementTarget = &vpcv1.InstancePlacementTargetPrototype{
			ID: &nodeClass.Spec.PlacementTarget,
		}
	}

	// Add resource group if specified
	if nodeClass.Spec.ResourceGroup != "" {
		instancePrototype.ResourceGroup = &vpcv1.ResourceGroupIdentity{
			ID: &nodeClass.Spec.ResourceGroup,
		}
	}

	// Add UserData if specified
	if nodeClass.Spec.UserData != "" {
		instancePrototype.UserData = &nodeClass.Spec.UserData
	}

	// Add SSH keys if specified
	if len(nodeClass.Spec.SSHKeys) > 0 {
		keys := make([]vpcv1.KeyIdentityIntf, len(nodeClass.Spec.SSHKeys))
		for i, keyID := range nodeClass.Spec.SSHKeys {
			keys[i] = &vpcv1.KeyIdentity{
				ID: &keyID,
			}
		}
		instancePrototype.Keys = keys
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

// selectSubnetForZone selects an available subnet in the specified VPC and zone
func (p *IBMCloudInstanceProvider) selectSubnetForZone(ctx context.Context, vpcClient *ibm.VPCClient, vpcID, zone string) (*vpcv1.Subnet, error) {
	// List subnets in the VPC
	options := &vpcv1.ListSubnetsOptions{
		ResourceGroupID: nil, // Get subnets from all resource groups
	}

	subnets, _, err := vpcClient.ListSubnetsWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("listing subnets: %w", err)
	}

	// Filter subnets by VPC and zone
	var availableSubnets []vpcv1.Subnet
	for _, subnet := range subnets.Subnets {
		// Check if subnet belongs to the correct VPC
		if subnet.VPC != nil && subnet.VPC.ID != nil && *subnet.VPC.ID == vpcID {
			// Check if subnet is in the correct zone
			if subnet.Zone != nil && subnet.Zone.Name != nil && *subnet.Zone.Name == zone {
				// Check if subnet is available (status is available)
				if subnet.Status != nil && *subnet.Status == "available" {
					availableSubnets = append(availableSubnets, subnet)
				}
			}
		}
	}

	if len(availableSubnets) == 0 {
		return nil, fmt.Errorf("no available subnets found in VPC %s zone %s", vpcID, zone)
	}

	// Return the first available subnet (could implement more sophisticated selection)
	return &availableSubnets[0], nil
}

// getDefaultSecurityGroup gets the default security group for a VPC
func (p *IBMCloudInstanceProvider) getDefaultSecurityGroup(ctx context.Context, vpcClient *ibm.VPCClient, vpcID string) (*vpcv1.SecurityGroup, error) {
	// List security groups for the VPC
	options := &vpcv1.ListSecurityGroupsOptions{
		VPCID: &vpcID,
	}

	securityGroups, _, err := vpcClient.ListSecurityGroupsWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("listing security groups: %w", err)
	}

	// Find the default security group
	for _, sg := range securityGroups.SecurityGroups {
		if sg.Name != nil && *sg.Name == "default" {
			return &sg, nil
		}
	}

	// If no default found, return the first available security group
	if len(securityGroups.SecurityGroups) > 0 {
		return &securityGroups.SecurityGroups[0], nil
	}

	return nil, fmt.Errorf("no security groups found for VPC %s", vpcID)
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
	
	remainder := strings.TrimPrefix(providerID, "ibm://")
	
	// Handle two provider ID formats:
	// 1. Direct VPC instance: ibm://<instance-id>
	// 2. IKS managed node: ibm://<account-id>///cluster-id/worker-id
	var instanceID string
	if strings.Contains(remainder, "///") {
		// IKS format: extract cluster-id and worker-id to get VPC instance ID
		parts := strings.Split(remainder, "/")
		if len(parts) < 4 {
			return nil, fmt.Errorf("invalid IKS provider ID format: %s", providerID)
		}
		clusterID := parts[3]
		workerID := parts[4]
		
		// Use IKS API to get the underlying VPC instance ID
		var iksErr error
		instanceID, iksErr = p.getVPCInstanceIDFromIKSWorker(ctx, clusterID, workerID)
		if iksErr != nil {
			return nil, fmt.Errorf("failed to get VPC instance ID for IKS worker %s in cluster %s: %w", workerID, clusterID, iksErr)
		}
	} else {
		// Direct VPC instance format
		instanceID = remainder
	}
	
	// Validate instance ID format (IBM Cloud instance IDs are UUIDs)
	if len(instanceID) < 32 || strings.Contains(instanceID, "/") {
		return nil, fmt.Errorf("invalid instance ID format: %s", instanceID)
	}

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

// getVPCInstanceIDFromIKSWorker retrieves the VPC instance ID for an IKS worker
// This method maps IKS worker IDs to their underlying VPC instance IDs
func (p *IBMCloudInstanceProvider) getVPCInstanceIDFromIKSWorker(ctx context.Context, clusterID, workerID string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("IBM client not initialized")
	}

	// Get IKS client
	iksClient := p.client.GetIKSClient()
	if iksClient == nil {
		return "", fmt.Errorf("IKS client not available")
	}

	// Get VPC instance ID from IKS worker
	instanceID, err := iksClient.GetVPCInstanceIDFromWorker(ctx, clusterID, workerID)
	if err != nil {
		return "", fmt.Errorf("getting VPC instance ID from IKS worker: %w", err)
	}

	return instanceID, nil
}
