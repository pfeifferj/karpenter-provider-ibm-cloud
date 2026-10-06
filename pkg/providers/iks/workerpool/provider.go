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

package workerpool

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/providers/common/instancetype"
	commonTypes "github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/providers/common/types"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/utils/ownership"
)

const (
	// KarpenterManagedLabel is the label applied to dynamically created pools
	KarpenterManagedLabel = ownership.ManagedLabel
)

// IKSWorkerPoolProvider implements IKS-specific worker pool provisioning
type IKSWorkerPoolProvider struct {
	client               *ibm.Client
	kubeClient           client.Client
	apiReader            client.Reader
	iksClient            ibm.IKSClientInterface
	allocationNamespace  string
	instanceTypeProvider instancetype.Provider
}

// NewIKSWorkerPoolProvider creates a new IKS worker pool provider
func NewIKSWorkerPoolProvider(client *ibm.Client, kubeClient client.Client, options ...Option) (commonTypes.IKSWorkerPoolProvider, error) {
	if client == nil {
		return nil, fmt.Errorf("IBM client cannot be nil")
	}

	p := &IKSWorkerPoolProvider{client: client, kubeClient: kubeClient, apiReader: kubeClient, allocationNamespace: ""}
	for _, option := range options {
		option(p)
	}
	return p, nil
}

func (p *IKSWorkerPoolProvider) Create(ctx context.Context, nodeClaim *v1.NodeClaim, instanceTypes []*cloudprovider.InstanceType) (*corev1.Node, error) {
	return p.createAllocation(ctx, nodeClaim)
}

func (p *IKSWorkerPoolProvider) Delete(ctx context.Context, node *corev1.Node) error {
	return p.deleteAllocation(ctx, node)
}

func (p *IKSWorkerPoolProvider) Get(ctx context.Context, providerID string) (*corev1.Node, error) {
	return p.getWorker(ctx, providerID)
}

func (p *IKSWorkerPoolProvider) List(ctx context.Context) ([]*corev1.Node, error) {
	return p.listAllocations(ctx)
}

// ResizePool resizes a worker pool to the specified size
func (p *IKSWorkerPoolProvider) ResizePool(ctx context.Context, clusterID, poolID string, newSize int) error {
	if p.client == nil && p.iksClient == nil {
		return fmt.Errorf("IBM client is not initialized")
	}

	iksClient, err := p.getIKSClient()
	if err != nil {
		return fmt.Errorf("getting IKS client: %w", err)
	}

	return iksClient.ResizeWorkerPool(ctx, clusterID, poolID, newSize)
}

// GetPool retrieves information about a worker pool
func (p *IKSWorkerPoolProvider) GetPool(ctx context.Context, clusterID, poolID string) (*commonTypes.WorkerPool, error) {
	if p.client == nil && p.iksClient == nil {
		return nil, fmt.Errorf("IBM client is not initialized")
	}

	iksClient, err := p.getIKSClient()
	if err != nil {
		return nil, fmt.Errorf("getting IKS client: %w", err)
	}

	pool, err := iksClient.GetWorkerPool(ctx, clusterID, poolID)
	if err != nil {
		return nil, err
	}

	// Convert from IKS client type to common type
	return &commonTypes.WorkerPool{
		ID:          pool.ID,
		Name:        pool.Name,
		Flavor:      pool.Flavor,
		Zone:        pool.Zone,
		SizePerZone: pool.SizePerZone,
		ActualSize:  pool.ActualSize,
		State:       pool.State,
		Labels:      pool.Labels,
	}, nil
}

// ListPools returns all worker pools for a cluster
func (p *IKSWorkerPoolProvider) ListPools(ctx context.Context, clusterID string) ([]*commonTypes.WorkerPool, error) {
	if p.client == nil && p.iksClient == nil {
		return nil, fmt.Errorf("IBM client is not initialized")
	}

	iksClient, err := p.getIKSClient()
	if err != nil {
		return nil, fmt.Errorf("getting IKS client: %w", err)
	}

	pools, err := iksClient.ListWorkerPools(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	// Convert from IKS client types to common types
	var commonPools []*commonTypes.WorkerPool
	for _, pool := range pools {
		commonPools = append(commonPools, &commonTypes.WorkerPool{
			ID:          pool.ID,
			Name:        pool.Name,
			Flavor:      pool.Flavor,
			Zone:        pool.Zone,
			SizePerZone: pool.SizePerZone,
			ActualSize:  pool.ActualSize,
			State:       pool.State,
			Labels:      pool.Labels,
		})
	}

	return commonPools, nil
}

// CreatePool creates a new worker pool with the specified configuration
func (p *IKSWorkerPoolProvider) CreatePool(ctx context.Context, clusterID string, request *commonTypes.CreatePoolRequest) (*commonTypes.WorkerPool, error) {
	logger := log.FromContext(ctx)

	if p.client == nil && p.iksClient == nil {
		return nil, fmt.Errorf("IBM client is not initialized")
	}

	iksClient, err := p.getIKSClient()
	if err != nil {
		return nil, fmt.Errorf("getting IKS client: %w", err)
	}

	// Build zone configuration
	zones := []ibm.WorkerPoolZone{
		{
			ID:       request.Zone,
			SubnetID: request.SubnetID,
		},
	}

	// Build the create request
	createRequest := &ibm.WorkerPoolCreateRequest{
		Name:           request.Name,
		Flavor:         request.Flavor,
		SizePerZone:    request.SizePerZone,
		Zones:          zones,
		Labels:         request.Labels,
		DiskEncryption: request.DiskEncryption,
		VpcID:          request.VpcID,
	}

	logger.Info("Initiated dynamic worker pool creation",
		"name", request.Name,
		"flavor", request.Flavor,
		"zone", request.Zone,
		"size", request.SizePerZone)

	pool, err := iksClient.CreateWorkerPool(ctx, clusterID, createRequest)
	if err != nil {
		return nil, fmt.Errorf("creating worker pool: %w", err)
	}

	logger.Info("Dynamic worker pool created", "pool_id", pool.ID, "pool_name", pool.Name)

	return &commonTypes.WorkerPool{
		ID:          pool.ID,
		Name:        pool.Name,
		Flavor:      pool.Flavor,
		Zone:        pool.Zone,
		SizePerZone: pool.SizePerZone,
		ActualSize:  pool.ActualSize,
		State:       pool.State,
		Labels:      pool.Labels,
	}, nil
}

// DeletePool deletes a worker pool from the cluster
func (p *IKSWorkerPoolProvider) DeletePool(ctx context.Context, clusterID, poolID string) error {
	logger := log.FromContext(ctx)

	if p.client == nil && p.iksClient == nil {
		return fmt.Errorf("IBM client is not initialized")
	}

	iksClient, err := p.getIKSClient()
	if err != nil {
		return fmt.Errorf("getting IKS client: %w", err)
	}

	logger.Info("Initiated worker pool deletion", "cluster_id", clusterID, "pool_id", poolID)

	if err := iksClient.DeleteWorkerPool(ctx, clusterID, poolID); err != nil {
		return fmt.Errorf("deleting worker pool: %w", err)
	}

	logger.Info("Worker pool deleted", "pool_id", poolID)
	return nil
}

// isInstanceTypeAllowed checks if the instance type is in the allowed list
func isInstanceTypeAllowed(instanceType string, allowedTypes []string) bool {
	if len(allowedTypes) == 0 {
		return true
	}
	for _, allowed := range allowedTypes {
		if allowed == instanceType {
			return true
		}
	}
	return false
}

// isDynamicPoolsEnabled checks if dynamic pool creation is enabled in the nodeClass
func (p *IKSWorkerPoolProvider) isDynamicPoolsEnabled(nodeClass *v1alpha1.IBMNodeClass) bool {
	return nodeClass.Spec.IKSDynamicPools != nil && nodeClass.Spec.IKSDynamicPools.Enabled
}
