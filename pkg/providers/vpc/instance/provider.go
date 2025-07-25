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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/image"
	commonTypes "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/types"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/vpc/bootstrap"
)

// VPCInstanceProvider implements VPC-specific instance provisioning
type VPCInstanceProvider struct {
	client            *ibm.Client
	kubeClient        client.Client
	k8sClient         kubernetes.Interface
	bootstrapProvider *bootstrap.VPCBootstrapProvider
}

// NewVPCInstanceProvider creates a new VPC instance provider
func NewVPCInstanceProvider(client *ibm.Client, kubeClient client.Client) (commonTypes.VPCInstanceProvider, error) {
	if client == nil {
		return nil, fmt.Errorf("IBM client cannot be nil")
	}

	return &VPCInstanceProvider{
		client:            client,
		kubeClient:        kubeClient,
		k8sClient:         nil, // Will be set via dependency injection
		bootstrapProvider: nil, // Will be lazily initialized when needed
	}, nil
}

// NewVPCInstanceProviderWithKubernetesClient creates a new VPC instance provider with proper kubernetes client injection
func NewVPCInstanceProviderWithKubernetesClient(client *ibm.Client, kubeClient client.Client, kubernetesClient kubernetes.Interface) (commonTypes.VPCInstanceProvider, error) {
	if client == nil {
		return nil, fmt.Errorf("IBM client cannot be nil")
	}

	// Create bootstrap provider immediately with proper dependency injection
	bootstrapProvider := bootstrap.NewVPCBootstrapProvider(client, kubernetesClient, kubeClient)

	return &VPCInstanceProvider{
		client:            client,
		kubeClient:        kubeClient,
		k8sClient:         kubernetesClient,
		bootstrapProvider: bootstrapProvider,
	}, nil
}

// Create provisions a new VPC instance
func (p *VPCInstanceProvider) Create(ctx context.Context, nodeClaim *v1.NodeClaim) (*corev1.Node, error) {
	logger := log.FromContext(ctx)

	if p.kubeClient == nil {
		return nil, fmt.Errorf("kubernetes client not set")
	}

	// Get the NodeClass to extract configuration
	nodeClass := &v1alpha1.IBMNodeClass{}
	if getErr := p.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaim.Spec.NodeClassRef.Name}, nodeClass); getErr != nil {
		return nil, fmt.Errorf("getting NodeClass %s: %w", nodeClaim.Spec.NodeClassRef.Name, getErr)
	}

	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return nil, fmt.Errorf("getting VPC client: %w", err)
	}

	// Extract instance profile - prefer NodeClass, fallback to labels
	instanceProfile := nodeClass.Spec.InstanceProfile
	if instanceProfile == "" {
		instanceProfile = nodeClaim.Labels["node.kubernetes.io/instance-type"]
		if instanceProfile == "" {
			return nil, fmt.Errorf("instance profile not specified in NodeClass or node claim")
		}
	}

	// Determine zone and subnet
	zone := nodeClass.Spec.Zone
	if zone == "" {
		return nil, fmt.Errorf("zone not specified in NodeClass")
	}

	subnet := nodeClass.Spec.Subnet
	if subnet == "" {
		return nil, fmt.Errorf("subnet not specified in NodeClass")
	}

	logger.Info("Creating VPC instance", "instance_profile", instanceProfile, "zone", zone, "subnet", subnet)

	// Create primary network interface with subnet
	primaryNetworkInterface := &vpcv1.NetworkInterfacePrototype{
		Subnet: &vpcv1.SubnetIdentity{
			ID: &subnet,
		},
		// Allow IP spoofing 
		AllowIPSpoofing: &[]bool{false}[0],
		Name: &[]string{fmt.Sprintf("%s-primary", nodeClaim.Name)}[0],
	}

	// Add security groups if specified, otherwise use default
	if len(nodeClass.Spec.SecurityGroups) > 0 {
		var securityGroups []vpcv1.SecurityGroupIdentityIntf
		for _, sg := range nodeClass.Spec.SecurityGroups {
			securityGroups = append(securityGroups, &vpcv1.SecurityGroupIdentity{ID: &sg})
		}
		primaryNetworkInterface.SecurityGroups = securityGroups
		logger.Info("Applying security groups to instance", "security_groups", nodeClass.Spec.SecurityGroups, "count", len(securityGroups))
	} else {
		// Get default security group for VPC
		defaultSG, sgErr := p.getDefaultSecurityGroup(ctx, vpcClient, nodeClass.Spec.VPC)
		if sgErr != nil {
			return nil, fmt.Errorf("getting default security group for VPC %s: %w", nodeClass.Spec.VPC, sgErr)
		}
		primaryNetworkInterface.SecurityGroups = []vpcv1.SecurityGroupIdentityIntf{
			&vpcv1.SecurityGroupIdentity{ID: defaultSG.ID},
		}
		logger.Info("Using default security group for instance", "security_group", *defaultSG.ID)
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
		
		// Add availability policy 
		AvailabilityPolicy: &vpcv1.InstanceAvailabilityPolicyPrototype{
			HostFailure: &[]string{"restart"}[0],
		},
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

	// Add SSH keys if specified
	if len(nodeClass.Spec.SSHKeys) > 0 {
		var sshKeys []vpcv1.KeyIdentityIntf
		for _, key := range nodeClass.Spec.SSHKeys {
			sshKeys = append(sshKeys, &vpcv1.KeyIdentity{ID: &key})
		}
		instancePrototype.Keys = sshKeys
	}

	// Generate bootstrap user data using the bootstrap provider
	userData, err := p.generateBootstrapUserData(ctx, nodeClass, types.NamespacedName{
		Name:      nodeClaim.Name,
		Namespace: nodeClaim.Namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("generating bootstrap user data: %w", err)
	}

	// Set user data
	instancePrototype.UserData = &userData

	// Enable metadata service for instance ID retrieval
	instancePrototype.MetadataService = &vpcv1.InstanceMetadataServicePrototype{
		Enabled:         &[]bool{true}[0],
		Protocol:        &[]string{"http"}[0],
		ResponseHopLimit: &[]int64{2}[0],
	}

	// Create the instance
	instance, err := vpcClient.CreateInstance(ctx, instancePrototype)
	if err != nil {
		// Parse the error for better error information
		ibmErr := ibm.ParseError(err)
		logger.Error(err, "Error creating VPC instance",
			"status_code", ibmErr.StatusCode,
			"error_code", ibmErr.Code,
			"retryable", ibmErr.Retryable)
		return nil, fmt.Errorf("creating VPC instance: %w", err)
	}

	logger.Info("VPC instance created successfully", "instance_id", *instance.ID, "name", *instance.Name)
	
	// Verify security groups were applied correctly
	if instance.PrimaryNetworkInterface != nil && instance.PrimaryNetworkInterface.ID != nil {
		logger.Info("Instance created with primary network interface", "interface_id", *instance.PrimaryNetworkInterface.ID)
		// Note: Security groups information may not be available in the instance creation response
		// This would require a separate GetInstance call to verify security groups
	} else {
		logger.Info("Instance created but primary network interface information not available in response")
	}

	// Create Node representation
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeClaim.Name,
			Labels: map[string]string{
				"karpenter.sh/managed":             "true",
				"karpenter.ibm.sh/vpc-id":          nodeClass.Spec.VPC,
				"karpenter.ibm.sh/zone":            zone,
				"karpenter.ibm.sh/region":          nodeClass.Spec.Region,
				"karpenter.ibm.sh/instance-type":   instanceProfile,
				"node.kubernetes.io/instance-type": instanceProfile,
				"topology.kubernetes.io/zone":      zone,
				"topology.kubernetes.io/region":    nodeClass.Spec.Region,
				"karpenter.sh/capacity-type":       "on-demand",
				"karpenter.sh/nodepool":            nodeClaim.Labels["karpenter.sh/nodepool"],
			},
		},
		Spec: corev1.NodeSpec{
			// Use the full instance ID including the zone prefix (e.g., 02c7_uuid)
			// This ensures consistency with how IBM Cloud APIs expect the instance ID
			ProviderID: fmt.Sprintf("ibm:///%s/%s", nodeClass.Spec.Region, *instance.ID),
		},
		Status: corev1.NodeStatus{
			Phase: corev1.NodePending,
			Conditions: []corev1.NodeCondition{
				{
					Type:               corev1.NodeReady,
					Status:             corev1.ConditionUnknown,
					LastHeartbeatTime:  metav1.Now(),
					LastTransitionTime: metav1.Now(),
					Reason:             "NodeCreating",
					Message:            "Node is being created",
				},
			},
		},
	}

	return node, nil
}

// Delete removes a VPC instance
func (p *VPCInstanceProvider) Delete(ctx context.Context, node *corev1.Node) error {
	logger := log.FromContext(ctx)

	instanceID := extractInstanceIDFromProviderID(node.Spec.ProviderID)
	if instanceID == "" {
		return fmt.Errorf("could not extract instance ID from provider ID: %s", node.Spec.ProviderID)
	}

	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return fmt.Errorf("getting VPC client: %w", err)
	}

	logger.Info("Deleting VPC instance", "instance_id", instanceID, "node", node.Name)

	err = vpcClient.DeleteInstance(ctx, instanceID)
	if err != nil {
		// Check if this is a "not found" error, which is acceptable
		if isIBMInstanceNotFoundError(err) {
			logger.Info("VPC instance already deleted", "instance_id", instanceID)
			return nil
		}
		// Parse the error for better error information
		ibmErr := ibm.ParseError(err)
		logger.Error(err, "Error deleting VPC instance",
			"instance_id", instanceID,
			"status_code", ibmErr.StatusCode,
			"error_code", ibmErr.Code,
			"retryable", ibmErr.Retryable)
		return fmt.Errorf("deleting VPC instance %s: %w", instanceID, err)
	}

	logger.Info("VPC instance deleted successfully", "instance_id", instanceID)
	return nil
}

// Get retrieves information about a VPC instance
func (p *VPCInstanceProvider) Get(ctx context.Context, providerID string) (*corev1.Node, error) {
	instanceID := extractInstanceIDFromProviderID(providerID)
	if instanceID == "" {
		return nil, fmt.Errorf("could not extract instance ID from provider ID: %s", providerID)
	}

	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return nil, fmt.Errorf("getting VPC client: %w", err)
	}

	instance, err := vpcClient.GetInstance(ctx, instanceID)
	if err != nil {
		if isIBMInstanceNotFoundError(err) {
			return nil, cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("instance %s not found", instanceID))
		}
		return nil, fmt.Errorf("getting VPC instance %s: %w", instanceID, err)
	}

	// Convert VPC instance to Node representation
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: *instance.Name,
		},
		Spec: corev1.NodeSpec{
			ProviderID: providerID,
		},
	}

	return node, nil
}

// List returns all VPC instances
func (p *VPCInstanceProvider) List(ctx context.Context) ([]*corev1.Node, error) {
	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return nil, fmt.Errorf("getting VPC client: %w", err)
	}

	instances, err := vpcClient.ListInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing VPC instances: %w", err)
	}

	var nodes []*corev1.Node
	for _, instance := range instances {
		if instance.ID != nil && instance.Name != nil {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: *instance.Name,
				},
				Spec: corev1.NodeSpec{
					ProviderID: fmt.Sprintf("ibm:///%s/%s", "region", *instance.ID),
				},
			}
			nodes = append(nodes, node)
		}
	}

	return nodes, nil
}

// UpdateTags updates tags on a VPC instance
func (p *VPCInstanceProvider) UpdateTags(ctx context.Context, providerID string, tags map[string]string) error {
	instanceID := extractInstanceIDFromProviderID(providerID)
	if instanceID == "" {
		return fmt.Errorf("could not extract instance ID from provider ID: %s", providerID)
	}

	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return fmt.Errorf("getting VPC client: %w", err)
	}

	return vpcClient.UpdateInstanceTags(ctx, instanceID, tags)
}

// extractInstanceIDFromProviderID extracts the instance ID from a provider ID
func extractInstanceIDFromProviderID(providerID string) string {
	// Provider ID format: ibm:///region/instance-id
	// Instance ID includes zone prefix (e.g., 02c7_uuid)
	parts := strings.Split(providerID, "/")
	if len(parts) >= 4 {
		return parts[len(parts)-1]
	}
	return ""
}

// getDefaultSecurityGroup gets the default security group for a VPC
func (p *VPCInstanceProvider) getDefaultSecurityGroup(ctx context.Context, vpcClient *ibm.VPCClient, vpcID string) (*vpcv1.SecurityGroup, error) {
	// TODO: implement security group lookup via VPC API
	// The VPCClient would need a ListSecurityGroups method
	return &vpcv1.SecurityGroup{
		ID:   &[]string{"default-sg"}[0],
		Name: &[]string{"default"}[0],
	}, nil
}

// isIBMInstanceNotFoundError checks if the error indicates an instance was not found in IBM Cloud VPC
func isIBMInstanceNotFoundError(err error) bool {
	return ibm.IsNotFound(err)
}

// generateBootstrapUserData generates bootstrap user data using the VPC bootstrap provider
func (p *VPCInstanceProvider) generateBootstrapUserData(ctx context.Context, nodeClass *v1alpha1.IBMNodeClass, nodeClaim types.NamespacedName) (string, error) {
	return p.generateBootstrapUserDataWithInstanceID(ctx, nodeClass, nodeClaim, "")
}

// generateBootstrapUserDataWithInstanceID generates bootstrap user data with a specific instance ID
func (p *VPCInstanceProvider) generateBootstrapUserDataWithInstanceID(ctx context.Context, nodeClass *v1alpha1.IBMNodeClass, nodeClaim types.NamespacedName, instanceID string) (string, error) {
	logger := log.FromContext(ctx)

	// If manual userData is provided, use it as-is (fallback behavior)
	if nodeClass.Spec.UserData != "" {
		logger.Info("Using manual userData from IBMNodeClass")
		return nodeClass.Spec.UserData, nil
	}

	// Initialize bootstrap provider if not already done
	if p.bootstrapProvider == nil {
		if p.k8sClient != nil {
			// Use properly injected kubernetes client
			p.bootstrapProvider = bootstrap.NewVPCBootstrapProvider(p.client, p.k8sClient, p.kubeClient)
		} else {
			// Fallback to creating kubernetes client (for backward compatibility)
			k8sClient, err := p.createKubernetesClient(ctx)
			if err != nil {
				logger.Error(err, "Failed to create kubernetes client, falling back to basic bootstrap")
				return p.getBasicBootstrapScript(nodeClass), nil
			}

			p.k8sClient = k8sClient
			p.bootstrapProvider = bootstrap.NewVPCBootstrapProvider(p.client, k8sClient, p.kubeClient)
		}
	}

	// Generate dynamic bootstrap script with instance ID
	logger.Info("Generating dynamic bootstrap script with automatic cluster discovery", "instanceID", instanceID)
	userData, err := p.bootstrapProvider.GetUserDataWithInstanceID(ctx, nodeClass, nodeClaim, instanceID)
	if err != nil {
		logger.Error(err, "Failed to generate bootstrap user data, falling back to basic bootstrap")
		return p.getBasicBootstrapScript(nodeClass), nil
	}

	logger.Info("Successfully generated dynamic bootstrap script")
	return userData, nil
}

// createKubernetesClient creates a kubernetes.Interface from the in-cluster config
func (p *VPCInstanceProvider) createKubernetesClient(ctx context.Context) (kubernetes.Interface, error) {
	// Since we're running inside the cluster, we can use the in-cluster config
	// This is the same config that the controller-runtime client uses
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("creating in-cluster config: %w", err)
	}

	// Create kubernetes clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes clientset: %w", err)
	}

	return clientset, nil
}

// getBasicBootstrapScript returns a basic bootstrap script when automatic generation fails
func (p *VPCInstanceProvider) getBasicBootstrapScript(nodeClass *v1alpha1.IBMNodeClass) string {
	return fmt.Sprintf(`#!/bin/bash
# Karpenter IBM Cloud Provider - Basic Bootstrap
# This is a fallback script when automatic bootstrap generation fails
echo "$(date): Basic bootstrap for region %s"

# Essential system configuration for kubeadm (CRITICAL FIX)
echo "$(date): Configuring system for kubeadm..."
echo 'net.ipv4.ip_forward = 1' >> /etc/sysctl.conf && sysctl -p
swapoff -a && sed -i '/swap/d' /etc/fstab

echo "$(date): Manual configuration required for cluster joining"
echo "$(date): Set nodeClass.spec.userData with proper bootstrap script"
# To make nodes join the cluster, you need to provide:
# 1. Bootstrap token: kubectl create token --print-join-command
# 2. Internal API endpoint (not external)
# 3. Proper hostname configuration for IBM Cloud
`, nodeClass.Spec.Region)
}
