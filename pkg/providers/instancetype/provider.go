package instancetype

import (
	"context"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/karpenter/pkg/apis/v1beta1"
)

// Provider defines the interface for managing IBM Cloud instance types
type Provider interface {
	// List returns all available instance types that match the given requirements
	List(ctx context.Context, requirements *v1beta1.Requirements) ([]*InstanceType, error)

	// Get returns the instance type with the specified name
	Get(ctx context.Context, name string) (*InstanceType, error)
}

// InstanceType represents an IBM Cloud instance type
type InstanceType struct {
	// Name is the instance type name (e.g., bx2-2x8)
	Name string

	// Architecture is the CPU architecture (e.g., amd64)
	Architecture string

	// CPU is the number of vCPUs
	CPU resource.Quantity

	// Memory is the amount of memory in bytes
	Memory resource.Quantity

	// Price is the hourly price for the instance type
	Price float64

	// Region is the region where this instance type is available
	Region string

	// Zones are the availability zones where this instance type is available
	Zones []string

	// Capabilities represents the instance type's capabilities
	Capabilities []string

	// Requirements represents the instance type's requirements
	Requirements v1beta1.Requirements
}
