package events

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestNodeClaimFailedToResolveNodeClass(t *testing.T) {
	tests := []struct {
		name      string
		nodeClaim *karpv1.NodeClaim
		expected  string
	}{
		{
			name: "valid NodeClaim",
			nodeClaim: &karpv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclaim",
				},
			},
			expected: "Failed to resolve NodeClass for NodeClaim test-nodeclaim",
		},
		{
			name: "NodeClaim with empty name",
			nodeClaim: &karpv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "",
				},
			},
			expected: "Failed to resolve NodeClass for NodeClaim ",
		},
		{
			name: "NodeClaim with namespace",
			nodeClaim: &karpv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nodeclaim",
					Namespace: "karpenter",
				},
			},
			expected: "Failed to resolve NodeClass for NodeClaim test-nodeclaim",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NodeClaimFailedToResolveNodeClass(tt.nodeClaim)
			
			assert.Equal(t, corev1.EventTypeWarning, event.Type)
			assert.Equal(t, "FailedToResolveNodeClass", event.Reason)
			assert.Equal(t, tt.expected, event.Message)
		})
	}
}

func TestNodePoolFailedToResolveNodeClass(t *testing.T) {
	tests := []struct {
		name     string
		nodePool *karpv1.NodePool
		expected string
	}{
		{
			name: "valid NodePool",
			nodePool: &karpv1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodepool",
				},
			},
			expected: "Failed to resolve NodeClass for NodePool test-nodepool",
		},
		{
			name: "NodePool with empty name",
			nodePool: &karpv1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "",
				},
			},
			expected: "Failed to resolve NodeClass for NodePool ",
		},
		{
			name: "NodePool with namespace",
			nodePool: &karpv1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nodepool",
					Namespace: "karpenter",
				},
			},
			expected: "Failed to resolve NodeClass for NodePool test-nodepool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NodePoolFailedToResolveNodeClass(tt.nodePool)
			
			assert.Equal(t, corev1.EventTypeWarning, event.Type)
			assert.Equal(t, "FailedToResolveNodeClass", event.Reason)
			assert.Equal(t, tt.expected, event.Message)
		})
	}
}

func TestEventStructure(t *testing.T) {
	nodeClaim := &karpv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclaim",
		},
	}
	
	event := NodeClaimFailedToResolveNodeClass(nodeClaim)
	
	// Ensure all required fields are set
	assert.NotEmpty(t, event.Type)
	assert.NotEmpty(t, event.Reason)
	assert.NotEmpty(t, event.Message)
	
	// Ensure event type is valid
	assert.Contains(t, []string{corev1.EventTypeNormal, corev1.EventTypeWarning}, event.Type)
}

func TestNilObjectHandling(t *testing.T) {
	// These functions should handle nil objects gracefully without panicking
	assert.NotPanics(t, func() {
		event := NodeClaimFailedToResolveNodeClass(nil)
		assert.Equal(t, corev1.EventTypeWarning, event.Type)
		assert.Equal(t, "FailedToResolveNodeClass", event.Reason)
		assert.Contains(t, event.Message, "Failed to resolve NodeClass for NodeClaim <unknown>")
	})
	
	assert.NotPanics(t, func() {
		event := NodePoolFailedToResolveNodeClass(nil)
		assert.Equal(t, corev1.EventTypeWarning, event.Type)
		assert.Equal(t, "FailedToResolveNodeClass", event.Reason)
		assert.Contains(t, event.Message, "Failed to resolve NodeClass for NodePool <unknown>")
	})
}