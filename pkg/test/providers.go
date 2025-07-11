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

package test

import (
	"context"
	"fmt"
	"strings"

	"github.com/IBM/vpc-go-sdk/vpcv1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/fake"
	commonTypes "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/types"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/vpc/subnet"
)

// TestVPCInstanceProvider is a test wrapper for VPC instance operations
type TestVPCInstanceProvider struct {
	vpcAPI     *fake.VPCAPI
	kubeClient client.Client
}

// NewTestVPCInstanceProvider creates a new test VPC instance provider
func NewTestVPCInstanceProvider(vpcAPI *fake.VPCAPI, kubeClient client.Client) *TestVPCInstanceProvider {
	return &TestVPCInstanceProvider{
		vpcAPI:     vpcAPI,
		kubeClient: kubeClient,
	}
}

// Create creates a VPC instance using the fake API
func (p *TestVPCInstanceProvider) Create(ctx context.Context, nodeClaim *karpv1.NodeClaim) (*corev1.Node, error) {
	// Get the node class
	var nodeClass v1alpha1.IBMNodeClass
	nodeClassRef := nodeClaim.Spec.NodeClassRef
	err := p.kubeClient.Get(ctx, types.NamespacedName{
		Name: nodeClassRef.Name,
	}, &nodeClass)
	if err != nil {
		return nil, err
	}

	// Create instance prototype
	prototype := &vpcv1.InstancePrototypeInstanceByImage{
		Name: &nodeClaim.Name,
		Image: &vpcv1.ImageIdentity{
			ID: &nodeClass.Spec.Image,
		},
		Profile: &vpcv1.InstanceProfileIdentity{
			Name: &nodeClass.Spec.InstanceProfile,
		},
		Zone: &vpcv1.ZoneIdentity{
			Name: &nodeClass.Spec.Zone,
		},
		VPC: &vpcv1.VPCIdentity{
			ID: &nodeClass.Spec.VPC,
		},
		PrimaryNetworkInterface: &vpcv1.NetworkInterfacePrototype{
			Subnet: &vpcv1.SubnetIdentity{
				ID: &nodeClass.Spec.Subnet,
			},
		},
	}

	// Create the instance using fake API
	instance, err := p.vpcAPI.CreateInstance(ctx, prototype)
	if err != nil {
		return nil, err
	}

	// Convert to Kubernetes Node
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeClaim.Name,
			Labels: map[string]string{
				"node.kubernetes.io/instance-type": nodeClass.Spec.InstanceProfile,
				"topology.kubernetes.io/zone":      nodeClass.Spec.Zone,
			},
		},
		Spec: corev1.NodeSpec{
			ProviderID: fmt.Sprintf("ibm:///%s/%s", nodeClass.Spec.Region, *instance.ID),
		},
	}

	return node, nil
}

// Delete deletes a VPC instance using the fake API
func (p *TestVPCInstanceProvider) Delete(ctx context.Context, node *corev1.Node) error {
	instanceID := extractInstanceIDFromProviderID(node.Spec.ProviderID)
	if instanceID == "" {
		return fmt.Errorf("could not extract instance ID from provider ID: %s", node.Spec.ProviderID)
	}

	return p.vpcAPI.DeleteInstance(ctx, instanceID)
}

// Get retrieves a VPC instance using the fake API
func (p *TestVPCInstanceProvider) Get(ctx context.Context, providerID string) (*corev1.Node, error) {
	instanceID := extractInstanceIDFromProviderID(providerID)
	if instanceID == "" {
		return nil, fmt.Errorf("could not extract instance ID from provider ID: %s", providerID)
	}

	instance, err := p.vpcAPI.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}

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

// UpdateTags updates instance tags using the fake API
func (p *TestVPCInstanceProvider) UpdateTags(ctx context.Context, providerID string, tags map[string]string) error {
	instanceID := extractInstanceIDFromProviderID(providerID)
	if instanceID == "" {
		return fmt.Errorf("could not extract instance ID from provider ID: %s", providerID)
	}

	// For the fake API, we can simulate tag updates
	// In a real implementation, this would call the VPC API to update tags
	return nil
}

// List returns all VPC instances using the fake API
func (p *TestVPCInstanceProvider) List(ctx context.Context) ([]*corev1.Node, error) {
	collection, err := p.vpcAPI.ListInstances(ctx, "", "")
	if err != nil {
		return nil, err
	}

	var nodes []*corev1.Node
	for _, instance := range collection.Instances {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: *instance.Name,
				Labels: map[string]string{
					"node.kubernetes.io/instance-type": *instance.Profile.Name,
					"topology.kubernetes.io/zone":      *instance.Zone.Name,
				},
			},
			Spec: corev1.NodeSpec{
				ProviderID: fmt.Sprintf("ibm:///%s/%s", *instance.Zone.Name, *instance.ID),
			},
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// SetKubeClient sets the Kubernetes client
func (p *TestVPCInstanceProvider) SetKubeClient(client client.Client) {
	p.kubeClient = client
}

// TestSubnetProvider is a test wrapper for subnet operations
type TestSubnetProvider struct {
	vpcAPI *fake.VPCAPI
}

// NewTestSubnetProvider creates a new test subnet provider
func NewTestSubnetProvider(vpcAPI *fake.VPCAPI) *TestSubnetProvider {
	return &TestSubnetProvider{
		vpcAPI: vpcAPI,
	}
}

// ListSubnets lists subnets using the fake API
func (p *TestSubnetProvider) ListSubnets(ctx context.Context, vpcID string) ([]subnet.SubnetInfo, error) {
	collection, err := p.vpcAPI.ListSubnets(ctx, vpcID)
	if err != nil {
		return nil, err
	}

	var subnets []subnet.SubnetInfo
	for _, s := range collection.Subnets {
		subnets = append(subnets, subnet.SubnetInfo{
			ID:           *s.ID,
			Zone:         *s.Zone.Name,
			CIDR:         *s.Ipv4CIDRBlock,
			AvailableIPs: int32(*s.AvailableIpv4AddressCount),
			State:        *s.Status,
		})
	}

	return subnets, nil
}

// GetSubnet gets a subnet using the fake API
func (p *TestSubnetProvider) GetSubnet(ctx context.Context, subnetID string) (*subnet.SubnetInfo, error) {
	s, err := p.vpcAPI.GetSubnet(ctx, subnetID)
	if err != nil {
		return nil, err
	}

	return &subnet.SubnetInfo{
		ID:           *s.ID,
		Zone:         *s.Zone.Name,
		CIDR:         *s.Ipv4CIDRBlock,
		AvailableIPs: int32(*s.AvailableIpv4AddressCount),
		State:        *s.Status,
	}, nil
}

// SelectSubnets selects subnets based on strategy using the fake API
func (p *TestSubnetProvider) SelectSubnets(ctx context.Context, vpcID string, strategy *v1alpha1.PlacementStrategy) ([]subnet.SubnetInfo, error) {
	// For testing, return all subnets in the VPC
	return p.ListSubnets(ctx, vpcID)
}

// TestWorkerPoolProvider is a test wrapper for worker pool operations
type TestWorkerPoolProvider struct {
	iksAPI *fake.IKSAPI
}

// NewTestWorkerPoolProvider creates a new test worker pool provider
func NewTestWorkerPoolProvider(iksAPI *fake.IKSAPI) *TestWorkerPoolProvider {
	return &TestWorkerPoolProvider{
		iksAPI: iksAPI,
	}
}

// ResizeWorkerPool resizes a worker pool using the fake API
func (p *TestWorkerPoolProvider) ResizeWorkerPool(ctx context.Context, clusterID, poolID string, size int) error {
	return p.iksAPI.ResizeWorkerPool(ctx, clusterID, poolID, size, "Karpenter resize")
}

// ListWorkerPools lists worker pools using the fake API
func (p *TestWorkerPoolProvider) ListWorkerPools(ctx context.Context, clusterID string) ([]commonTypes.WorkerPool, error) {
	pools, err := p.iksAPI.ListWorkerPools(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	var result []commonTypes.WorkerPool
	for _, pool := range pools {
		result = append(result, commonTypes.WorkerPool{
			ID:          pool.ID,
			Name:        pool.Name,
			Flavor:      pool.Flavor,
			Zone:        pool.Zone,
			SizePerZone: pool.SizePerZone,
			ActualSize:  pool.ActualSize,
			State:       string(pool.State),
			Labels:      pool.Labels,
		})
	}

	return result, nil
}

// GetWorkerPool gets a worker pool using the fake API
func (p *TestWorkerPoolProvider) GetWorkerPool(ctx context.Context, clusterID, poolID string) (*commonTypes.WorkerPool, error) {
	pool, err := p.iksAPI.GetWorkerPool(ctx, clusterID, poolID)
	if err != nil {
		return nil, err
	}

	return &commonTypes.WorkerPool{
		ID:          pool.ID,
		Name:        pool.Name,
		Flavor:      pool.Flavor,
		Zone:        pool.Zone,
		SizePerZone: pool.SizePerZone,
		ActualSize:  pool.ActualSize,
		State:       string(pool.State),
		Labels:      pool.Labels,
	}, nil
}

// Create provisions a new instance via IKS worker pool operations
func (p *TestWorkerPoolProvider) Create(ctx context.Context, nodeClaim *karpv1.NodeClaim) (*corev1.Node, error) {
	// In IKS mode, we would resize an existing worker pool or create a new one
	// For this test implementation, we'll simulate creating a node
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeClaim.Name,
			Labels: map[string]string{
				"node.kubernetes.io/instance-type": "bx2-4x16", // mock instance type
				"topology.kubernetes.io/zone":      "us-south-1", // mock zone
			},
		},
		Spec: corev1.NodeSpec{
			ProviderID: fmt.Sprintf("ibm:///us-south/worker-%s", nodeClaim.Name),
		},
	}
	return node, nil
}

// Delete removes an instance via IKS worker pool operations
func (p *TestWorkerPoolProvider) Delete(ctx context.Context, node *corev1.Node) error {
	// In IKS mode, we would resize down the worker pool
	// For this test implementation, we'll just simulate success
	return nil
}

// Get retrieves an instance via IKS APIs
func (p *TestWorkerPoolProvider) Get(ctx context.Context, providerID string) (*corev1.Node, error) {
	// Extract worker ID from provider ID and mock retrieval
	workerID := extractInstanceIDFromProviderID(providerID)
	if workerID == "" {
		return nil, fmt.Errorf("invalid provider ID: %s", providerID)
	}
	
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: workerID,
		},
		Spec: corev1.NodeSpec{
			ProviderID: providerID,
		},
	}
	return node, nil
}

// List returns all instances managed by IKS worker pools
func (p *TestWorkerPoolProvider) List(ctx context.Context) ([]*corev1.Node, error) {
	return []*corev1.Node{}, nil
}

// GetPool retrieves information about a worker pool (implements IKSWorkerPoolProvider interface)
func (p *TestWorkerPoolProvider) GetPool(ctx context.Context, clusterID, poolID string) (*commonTypes.WorkerPool, error) {
	return p.GetWorkerPool(ctx, clusterID, poolID)
}

// ListPools returns all worker pools for a cluster (implements IKSWorkerPoolProvider interface)
func (p *TestWorkerPoolProvider) ListPools(ctx context.Context, clusterID string) ([]*commonTypes.WorkerPool, error) {
	pools, err := p.ListWorkerPools(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	
	// Convert slice of values to slice of pointers
	result := make([]*commonTypes.WorkerPool, len(pools))
	for i := range pools {
		result[i] = &pools[i]
	}
	return result, nil
}

// ResizePool resizes a worker pool by the specified amount (implements IKSWorkerPoolProvider interface) 
func (p *TestWorkerPoolProvider) ResizePool(ctx context.Context, clusterID, poolID string, newSize int) error {
	return p.ResizeWorkerPool(ctx, clusterID, poolID, newSize)
}

// Helper functions
func extractInstanceIDFromProviderID(providerID string) string {
	// Extract instance ID from provider ID format: ibm:///region/instance-id
	parts := strings.Split(providerID, "/")
	if len(parts) >= 4 && parts[0] == "ibm:" {
		return parts[len(parts)-1]
	}
	return ""
}

