package instancetype

import (
	"context"
	"fmt"
	"sort"

	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	v1alpha1 "github.com/karpenter-ibm/pkg/apis/v1alpha1"
)

// Provider defines the interface for managing IBM Cloud instance types
type Provider interface {
	// Get retrieves a specific instance type by name
	Get(ctx context.Context, name string) (*cloudprovider.InstanceType, error)

	// List returns all available instance types
	List(ctx context.Context) ([]*cloudprovider.InstanceType, error)

	// Create creates a new instance type (no-op for IBM Cloud as types are predefined)
	Create(ctx context.Context, instanceType *cloudprovider.InstanceType) error

	// Delete deletes an instance type (no-op for IBM Cloud as types are predefined)
	Delete(ctx context.Context, instanceType *cloudprovider.InstanceType) error

	// FilterInstanceTypes returns instance types that meet the specified requirements
	FilterInstanceTypes(ctx context.Context, requirements *v1alpha1.InstanceTypeRequirements) ([]*cloudprovider.InstanceType, error)

	// RankInstanceTypes sorts instance types by cost efficiency and other criteria
	RankInstanceTypes(instanceTypes []*cloudprovider.InstanceType) []*cloudprovider.InstanceType
}

// provider implements the Provider interface
type provider struct {
	// Add IBM Cloud client/dependencies here
}

// NewProvider creates a new instance type provider
func NewProvider() (Provider, error) {
	return &provider{}, nil
}

// instanceTypeRanking holds data for ranking instance types
type instanceTypeRanking struct {
	instanceType *cloudprovider.InstanceType
	score       float64
}

// calculateInstanceTypeScore computes a ranking score for an instance type
// Lower scores are better (more cost-efficient)
func calculateInstanceTypeScore(instanceType *cloudprovider.InstanceType) float64 {
	cpuCount := float64(instanceType.Capacity.CPU().Value())
	memoryGB := float64(instanceType.Capacity.Memory().Value()) / (1024 * 1024 * 1024) // Convert bytes to GB
	hourlyPrice := instanceType.Price

	// Calculate cost efficiency score (price per CPU and GB of memory)
	cpuEfficiency := hourlyPrice / cpuCount
	memoryEfficiency := hourlyPrice / memoryGB

	// Combine scores with weights
	// We weight CPU and memory equally in this implementation
	return (cpuEfficiency + memoryEfficiency) / 2
}

// RankInstanceTypes sorts instance types by cost efficiency
func (p *provider) RankInstanceTypes(instanceTypes []*cloudprovider.InstanceType) []*cloudprovider.InstanceType {
	// Create ranking slice
	rankings := make([]instanceTypeRanking, len(instanceTypes))
	for i, it := range instanceTypes {
		rankings[i] = instanceTypeRanking{
			instanceType: it,
			score:       calculateInstanceTypeScore(it),
		}
	}

	// Sort by score (lower is better)
	sort.Slice(rankings, func(i, j int) bool {
		return rankings[i].score < rankings[j].score
	})

	// Extract sorted instance types
	result := make([]*cloudprovider.InstanceType, len(rankings))
	for i, r := range rankings {
		result[i] = r.instanceType
	}

	return result
}

// FilterInstanceTypes returns instance types that meet requirements
func (p *provider) FilterInstanceTypes(ctx context.Context, requirements *v1alpha1.InstanceTypeRequirements) ([]*cloudprovider.InstanceType, error) {
	// Get all instance types
	allTypes, err := p.List(ctx)
	if err != nil {
		return nil, err
	}

	var filtered []*cloudprovider.InstanceType

	for _, it := range allTypes {
		// Check architecture requirement
		if requirements.Architecture != "" && it.Architecture != requirements.Architecture {
			continue
		}

		// Check CPU requirement
		if requirements.MinimumCPU > 0 && it.Capacity.CPU().Value() < int64(requirements.MinimumCPU) {
			continue
		}

		// Check memory requirement
		if requirements.MinimumMemory > 0 {
			memoryGB := float64(it.Capacity.Memory().Value()) / (1024 * 1024 * 1024)
			if memoryGB < float64(requirements.MinimumMemory) {
				continue
			}
		}

		// Check price requirement
		if requirements.MaximumHourlyPrice > 0 && it.Price > requirements.MaximumHourlyPrice {
			continue
		}

		filtered = append(filtered, it)
	}

	// Rank the filtered instances by cost efficiency
	return p.RankInstanceTypes(filtered), nil
}

// Get retrieves a specific instance type by name
func (p *provider) Get(ctx context.Context, name string) (*cloudprovider.InstanceType, error) {
	// TODO: Implement IBM Cloud API call to get instance type details
	return nil, fmt.Errorf("not implemented")
}

// List returns all available instance types
func (p *provider) List(ctx context.Context) ([]*cloudprovider.InstanceType, error) {
	// TODO: Implement IBM Cloud API call to list instance types
	return nil, fmt.Errorf("not implemented")
}

// Create creates a new instance type (no-op for IBM Cloud)
func (p *provider) Create(ctx context.Context, instanceType *cloudprovider.InstanceType) error {
	return nil
}

// Delete deletes an instance type (no-op for IBM Cloud)
func (p *provider) Delete(ctx context.Context, instanceType *cloudprovider.InstanceType) error {
	return nil
}
