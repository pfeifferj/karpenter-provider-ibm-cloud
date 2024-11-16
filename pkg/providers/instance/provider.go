package instance

import (
	"context"

	"k8s.io/api/core/v1"
	"sigs.k8s.io/karpenter/pkg/apis/v1beta1"
)

// Provider defines the interface for managing IBM Cloud instances
type Provider interface {
	// Create launches a new instance in IBM Cloud
	Create(ctx context.Context, nodeClaim *v1beta1.NodeClaim) (*v1.Node, error)

	// Delete terminates the specified instance
	Delete(ctx context.Context, node *v1.Node) error

	// GetInstance retrieves instance information for the specified node
	GetInstance(ctx context.Context, node *v1.Node) (*Instance, error)
}

// Instance represents an IBM Cloud instance
type Instance struct {
	// ID is the instance ID
	ID string

	// Name is the instance name
	Name string

	// Type is the instance type
	Type string

	// Zone is the availability zone
	Zone string

	// State represents the current state of the instance
	State string

	// LaunchTime is when the instance was created
	LaunchTime string
}
