package instancetype

import (
	"context"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
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
}
