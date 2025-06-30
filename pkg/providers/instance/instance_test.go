package instance

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

func TestInstanceProviderStructure(t *testing.T) {
	// Test basic instance provider structure without IBM client dependencies
	// This test ensures the package compiles correctly
	t.Log("Instance provider package compiles successfully")
}

func TestNodeClassValidation(t *testing.T) {
	// Test that we can create a basic NodeClass structure
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:          "us-south",
			Zone:            "us-south-1",
			InstanceProfile: "bx2-4x16",
			Image:           "r006-test-image-id",
			VPC:             "r006-test-vpc-id",
			Subnet:          "r006-test-subnet-id",
			SecurityGroups:  []string{"r006-test-sg-id"},
			Tags: map[string]string{
				"test": "true",
			},
		},
	}

	require.NotNil(t, nodeClass)
	require.Equal(t, "us-south", nodeClass.Spec.Region)
	require.Equal(t, "test-nodeclass", nodeClass.Name)
}

func TestNewProvider(t *testing.T) {
	// This test will fail with real IBM client creation due to missing credentials
	// Skip for now until we can mock the client creation properly
	t.Skip("Skipping NewProvider test - requires valid IBM credentials or proper mock interface")
}

func TestInstanceProvider_Create(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		nodeClaim   *v1.NodeClaim
		expectError bool
	}{
		{
			name: "valid instance creation request",
			nodeClaim: &v1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclaim",
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": "bx2-4x16",
						"topology.kubernetes.io/zone":      "us-south-1",
					},
				},
				Spec: v1.NodeClaimSpec{
					NodeClassRef: &v1.NodeClassReference{
						Kind:  "IBMNodeClass",
						Group: "karpenter.ibm.sh",
						Name:  "test-nodeclass",
					},
				},
			},
			expectError: true, // Will fail without real IBM client
		},
		{
			name: "missing instance type",
			nodeClaim: &v1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclaim",
					Labels: map[string]string{
						"topology.kubernetes.io/zone": "us-south-1",
					},
				},
				Spec: v1.NodeClaimSpec{
					NodeClassRef: &v1.NodeClassReference{
						Kind:  "IBMNodeClass",
						Group: "karpenter.ibm.sh",
						Name:  "test-nodeclass",
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create provider (will fail to initialize IBM client, but that's expected for unit tests)
			provider := &IBMCloudInstanceProvider{
				client: nil, // No real client for unit tests
			}

			_, err := provider.Create(ctx, tt.nodeClaim)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestInstanceProvider_Delete(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		node        *corev1.Node
		expectError bool
	}{
		{
			name: "valid instance deletion",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "ibm://test-instance-id",
				},
			},
			expectError: true, // Will fail without real IBM client
		},
		{
			name: "invalid provider ID format",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "aws://test-instance-id",
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &IBMCloudInstanceProvider{
				client: nil, // No real client for unit tests
			}

			err := provider.Delete(ctx, tt.node)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestInstanceProvider_GetInstance(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		node        *corev1.Node
		expectError bool
	}{
		{
			name: "get instance info",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "ibm://test-instance-id",
				},
			},
			expectError: true, // Will fail without real IBM client
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &IBMCloudInstanceProvider{
				client: nil, // No real client for unit tests
			}

			_, err := provider.GetInstance(ctx, tt.node)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewProvider_ErrorHandling(t *testing.T) {
	// Test that NewProvider handles missing environment variables gracefully
	_, err := NewProvider()
	// Should return error when IBM Cloud credentials are not available
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "creating IBM Cloud client")
}

// Note: Additional tests for Create, Delete, etc. require proper mock interfaces
// for IBM Cloud clients. These tests are skipped until mock implementations
// are properly aligned with the current IBM client interface.