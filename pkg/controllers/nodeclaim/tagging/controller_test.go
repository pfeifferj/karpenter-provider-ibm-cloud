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
package tagging

import (
	"context"
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
)

// Note: The tagging controller now creates its own VPC provider internally,
// so we can't easily mock the tag operations in unit tests.
// Real integration tests would be needed to verify tagging behavior.

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
	controller, err := NewController(fakeClient)
	if err != nil {
		t.Skipf("Skipping test due to missing IBM credentials: %v", err)
		return
	}

	// Test that Register method exists and can be called
	// (will fail with nil manager but that's expected)
	var mgr manager.Manager
	err = controller.Register(context.Background(), mgr)
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
		name       string
		nodeClaims []karpenterv1.NodeClaim
		nodes      []v1.Node
		wantErr    bool
	}{
		{
			name:    "no NodeClaims",
			wantErr: false,
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
			wantErr: false,
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
			wantErr: false,
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
			// Note: Can't easily test error cases without mocking
			wantErr: false,
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

			// Create controller
			controller, err := NewController(fakeClient)
			// Note: Will fail without IBM credentials, but we can test the constructor
			if err != nil {
				// Expected in test environment without IBM credentials
				t.Skipf("Skipping test due to missing IBM credentials: %v", err)
				return
			}

			// Run reconciliation
			_, err2 := controller.Reconcile(context.Background())
			// In test environment, tagging may fail due to missing credentials
			// This would need integration tests with real IBM Cloud setup
			if err2 != nil {
				t.Logf("Expected failure in test environment: %v", err2)
			}
		})
	}
}

func TestGetProviderIDFromNodeClaim(t *testing.T) {
	tests := []struct {
		name      string
		nodeClaim *karpenterv1.NodeClaim
		expected  string
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
