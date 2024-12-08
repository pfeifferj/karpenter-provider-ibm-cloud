package instance

import (
	"context"

	v1 "k8s.io/api/core/v1"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// Provider defines the interface for managing IBM Cloud instances
type Provider interface {
	// Create launches a new instance in IBM Cloud
	Create(ctx context.Context, nodeClaim *karpv1.NodeClaim) (*v1.Node, error)

	// Delete terminates the specified instance
	Delete(ctx context.Context, node *v1.Node) error

	// GetInstance retrieves instance information for the specified node
	GetInstance(ctx context.Context, node *v1.Node) (*Instance, error)

	// TagInstance adds or updates tags on an IBM Cloud instance
	TagInstance(ctx context.Context, instanceID string, tags map[string]string) error
}
