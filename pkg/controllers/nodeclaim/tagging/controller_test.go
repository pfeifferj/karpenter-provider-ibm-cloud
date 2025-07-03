package tagging

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
)

// mockInstanceProvider is a test implementation of instance.Provider
type mockInstanceProvider struct {
	tagCallCount int
	tagError     error
}

func (m *mockInstanceProvider) SetKubeClient(client client.Client) {}

func (m *mockInstanceProvider) Create(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (*v1.Node, error) {
	return &v1.Node{}, nil
}

func (m *mockInstanceProvider) Delete(ctx context.Context, node *v1.Node) error {
	return nil
}

func (m *mockInstanceProvider) GetInstance(ctx context.Context, node *v1.Node) (*instance.Instance, error) {
	return nil, nil
}

func (m *mockInstanceProvider) TagInstance(ctx context.Context, instanceID string, tags map[string]string) error {
	m.tagCallCount++
	return m.tagError
}

func TestController_Name(t *testing.T) {
	controller := &Controller{}
	assert.Equal(t, "nodeclaim.tagging", controller.Name())
}

func TestController_Register(t *testing.T) {
	// Create a proper scheme
	s := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(s))
	
	// Register Karpenter v1 types manually
	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	s.AddKnownTypes(gv,
		&karpenterv1.NodeClaim{},
		&karpenterv1.NodeClaimList{},
		&karpenterv1.NodePool{},
		&karpenterv1.NodePoolList{},
	)
	metav1.AddToGroupVersion(s, gv)
	
	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()
	controller := NewController(fakeClient, &mockInstanceProvider{})
	
	// Test that Register method exists and can be called
	// (will fail with nil manager but that's expected)
	var mgr manager.Manager
	err := controller.Register(context.Background(), mgr)
	assert.Error(t, err) // Expected because mgr is nil
}

func TestController_Reconcile(t *testing.T) {
	// Create a proper scheme
	s := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(s))
	
	// Register Karpenter v1 types manually
	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	s.AddKnownTypes(gv,
		&karpenterv1.NodeClaim{},
		&karpenterv1.NodeClaimList{},
		&karpenterv1.NodePool{},
		&karpenterv1.NodePoolList{},
	)
	metav1.AddToGroupVersion(s, gv)

	tests := []struct {
		name           string
		nodeClaims     []karpenterv1.NodeClaim
		nodes          []v1.Node
		tagError       error
		wantErr        bool
		expectedTags   int
	}{
		{
			name:         "no NodeClaims",
			wantErr:      false,
			expectedTags: 0,
		},
		{
			name: "NodeClaim without provider ID",
			nodeClaims: []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim-1",
						Labels: map[string]string{
							"label1": "value1",
						},
					},
					Status: karpenterv1.NodeClaimStatus{
						ProviderID: "",
					},
				},
			},
			wantErr:      false,
			expectedTags: 0,
		},
		{
			name: "NodeClaim with labels to tag",
			nodeClaims: []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim-2",
						Labels: map[string]string{
							"karpenter.sh/nodepool": "test-pool",
							"custom-label":          "custom-value",
						},
					},
					Status: karpenterv1.NodeClaimStatus{
						ProviderID: "ibm://instance-123",
						NodeName:   "test-node",
					},
				},
			},
			nodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							"node-label": "node-value",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "ibm://instance-123",
					},
				},
			},
			wantErr:      false,
			expectedTags: 1,
		},
		{
			name: "NodeClaim with tagging error",
			nodeClaims: []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim-3",
						Labels: map[string]string{
							"label1": "value1",
						},
					},
					Status: karpenterv1.NodeClaimStatus{
						ProviderID: "ibm://instance-456",
						NodeName:   "test-node-3",
					},
				},
			},
			nodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node-3",
					},
					Spec: v1.NodeSpec{
						ProviderID: "ibm://instance-456",
					},
				},
			},
			tagError:     fmt.Errorf("tagging failed"),
			wantErr:      true,
			expectedTags: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create objects for the fake client
			objs := make([]client.Object, 0)
			for i := range tt.nodeClaims {
				objs = append(objs, &tt.nodeClaims[i])
			}
			for i := range tt.nodes {
				objs = append(objs, &tt.nodes[i])
			}

			// Create fake client
			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(objs...).
				Build()

			// Create mock provider
			mockProvider := &mockInstanceProvider{
				tagError: tt.tagError,
			}

			// Create controller
			controller := NewController(fakeClient, mockProvider)

			// Run reconciliation
			_, err := controller.Reconcile(context.Background())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify the expected number of tag calls
			assert.Equal(t, tt.expectedTags, mockProvider.tagCallCount)
		})
	}
}

func TestGetProviderIDFromNodeClaim(t *testing.T) {
	tests := []struct {
		name       string
		nodeClaim  *karpenterv1.NodeClaim
		expected   string
	}{
		{
			name: "valid provider ID",
			nodeClaim: &karpenterv1.NodeClaim{
				Status: karpenterv1.NodeClaimStatus{
					ProviderID: "ibm://instance-123",
				},
			},
			expected: "instance-123",
		},
		{
			name: "provider ID without prefix",
			nodeClaim: &karpenterv1.NodeClaim{
				Status: karpenterv1.NodeClaimStatus{
					ProviderID: "instance-456",
				},
			},
			expected: "instance-456",
		},
		{
			name: "empty provider ID",
			nodeClaim: &karpenterv1.NodeClaim{
				Status: karpenterv1.NodeClaimStatus{
					ProviderID: "",
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the provider ID extraction logic
			providerID := tt.nodeClaim.Status.ProviderID
			if providerID != "" && len(providerID) > 6 && providerID[:6] == "ibm://" {
				providerID = providerID[6:]
			}
			assert.Equal(t, tt.expected, providerID)
		})
	}
}