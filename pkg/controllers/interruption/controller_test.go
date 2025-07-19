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
package interruption

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cache"
)

func TestNewController(t *testing.T) {
	kubeClient := fake.NewClientBuilder().Build()
	recorder := record.NewFakeRecorder(10)
	unavailableOfferings := cache.NewUnavailableOfferings()

	controller := NewController(kubeClient, recorder, unavailableOfferings, nil)

	assert.NotNil(t, controller)
	assert.Equal(t, kubeClient, controller.kubeClient)
	assert.Equal(t, recorder, controller.recorder)
	assert.Equal(t, unavailableOfferings, controller.unavailableOfferings)
	assert.Nil(t, controller.providerFactory)
	assert.NotNil(t, controller.httpClient)
	assert.Equal(t, 10*time.Second, controller.httpClient.Timeout)
}

func TestControllerName(t *testing.T) {
	controller := &Controller{}
	assert.Equal(t, "interruption", controller.Name())
}

func TestReconcile(t *testing.T) {
	tests := []struct {
		name  string
		nodes []v1.Node
	}{
		{
			name:  "no nodes",
			nodes: []v1.Node{},
		},
		{
			name: "single node not interrupted",
			nodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node-1",
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "bx2-2x8",
							"topology.kubernetes.io/zone":      "us-south-1",
							"kubernetes.io/hostname":           "test-node-1",
						},
					},
					Spec: v1.NodeSpec{
						Unschedulable: false,
					},
				},
			},
		},
		{
			name: "multiple nodes",
			nodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node-1",
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "bx2-2x8",
							"topology.kubernetes.io/zone":      "us-south-1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node-2",
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "bx2-4x16",
							"topology.kubernetes.io/zone":      "us-south-2",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			err := v1.AddToScheme(scheme)
			require.NoError(t, err)

			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(nodeSliceToObjects(tt.nodes)...).
				Build()

			recorder := record.NewFakeRecorder(10)
			unavailableOfferings := cache.NewUnavailableOfferings()

			controller := NewController(kubeClient, recorder, unavailableOfferings, nil)

			ctx := context.Background()
			result, err := controller.Reconcile(ctx)

			assert.NoError(t, err)
			assert.Equal(t, time.Minute, result.RequeueAfter)
		})
	}
}

func TestReconcileListError(t *testing.T) {
	// Test with a client that will return an error when listing nodes
	kubeClient := &mockErrorClient{}
	recorder := record.NewFakeRecorder(10)
	unavailableOfferings := cache.NewUnavailableOfferings()

	controller := NewController(kubeClient, recorder, unavailableOfferings, nil)

	ctx := context.Background()
	_, err := controller.Reconcile(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mock error")
}

func TestIsNodeInterrupted(t *testing.T) {
	tests := []struct {
		name           string
		node           *v1.Node
		expectedResult bool
		expectedReason InterruptionReason
	}{
		{
			name: "healthy node should not be interrupted",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "healthy-node",
				},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:   v1.NodeReady,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			expectedResult: false,
			expectedReason: "",
		},
		{
			name: "node with ready=false should be interrupted",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unhealthy-node",
				},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:    v1.NodeReady,
							Status:  v1.ConditionFalse,
							Reason:  "KubeletNotReady",
							Message: "Kubelet stopped posting node status",
						},
					},
				},
			},
			expectedResult: true,
			expectedReason: InstanceHealthFailed,
		},
		{
			name: "node with capacity issues should be capacity-related interruption",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "capacity-node",
				},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:    v1.NodeReady,
							Status:  v1.ConditionFalse,
							Reason:  "CapacityUnavailable",
							Message: "Not enough capacity available",
						},
					},
				},
			},
			expectedResult: true,
			expectedReason: CapacityUnavailable,
		},
		{
			name: "node with memory pressure should be capacity-related",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "memory-pressure-node",
				},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:   v1.NodeReady,
							Status: v1.ConditionTrue,
						},
						{
							Type:   v1.NodeMemoryPressure,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			expectedResult: true,
			expectedReason: CapacityUnavailable,
		},
		{
			name: "node with network unavailable should be network-related",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "network-node",
				},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:   v1.NodeReady,
							Status: v1.ConditionTrue,
						},
						{
							Type:   v1.NodeNetworkUnavailable,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			expectedResult: true,
			expectedReason: NetworkResourceLimit,
		},
		{
			name: "node with interruption annotation should not be processed again",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "already-interrupted-node",
					Annotations: map[string]string{
						InterruptionAnnotation: "true",
					},
				},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:   v1.NodeReady,
							Status: v1.ConditionFalse,
						},
					},
				},
			},
			expectedResult: false,
			expectedReason: "",
		},
		{
			name: "node with maintenance annotation should be host-maintenance",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "maintenance-node",
					Annotations: map[string]string{
						"ibm-cloud.kubernetes.io/maintenance": "true",
					},
				},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:   v1.NodeReady,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			expectedResult: true,
			expectedReason: HostMaintenance,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &Controller{
				httpClient:      &http.Client{Timeout: 5 * time.Second},
				providerFactory: nil, // No provider factory in tests
			}

			interrupted, reason := controller.isNodeInterrupted(context.Background(), tt.node)
			assert.Equal(t, tt.expectedResult, interrupted)
			assert.Equal(t, tt.expectedReason, reason)
		})
	}
}

func TestIsCapacityRelated(t *testing.T) {
	tests := []struct {
		name     string
		node     *v1.Node
		reason   InterruptionReason
		expected bool
	}{
		{
			name:     "capacity unavailable should be capacity related",
			node:     &v1.Node{},
			reason:   CapacityUnavailable,
			expected: true,
		},
		{
			name:     "network resource limit should be capacity related",
			node:     &v1.Node{},
			reason:   NetworkResourceLimit,
			expected: true,
		},
		{
			name:     "host maintenance should not be capacity related",
			node:     &v1.Node{},
			reason:   HostMaintenance,
			expected: false,
		},
		{
			name:     "instance health failed should not be capacity related",
			node:     &v1.Node{},
			reason:   InstanceHealthFailed,
			expected: false,
		},
		{
			name: "unknown reason with memory pressure should be capacity related",
			node: &v1.Node{
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:   v1.NodeMemoryPressure,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			reason:   "unknown",
			expected: true,
		},
		{
			name: "unknown reason without pressure should not be capacity related",
			node: &v1.Node{
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:   v1.NodeReady,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			reason:   "unknown",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &Controller{}
			result := controller.isCapacityRelated(tt.node, tt.reason)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMarkNodeAsInterrupted(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(node).
		Build()

	controller := &Controller{
		kubeClient: fakeClient,
	}

	ctx := context.Background()
	reason := CapacityUnavailable

	err := controller.markNodeAsInterrupted(ctx, node, reason)
	require.NoError(t, err)

	// Verify the node was updated with interruption annotations
	updatedNode := &v1.Node{}
	err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-node"}, updatedNode)
	require.NoError(t, err)

	assert.Equal(t, "true", updatedNode.Annotations[InterruptionAnnotation])
	assert.Equal(t, string(reason), updatedNode.Annotations[InterruptionReasonAnnotation])
	assert.NotEmpty(t, updatedNode.Annotations[InterruptionTimeAnnotation])

	// Verify the time annotation is a valid RFC3339 timestamp
	_, err = time.Parse(time.RFC3339, updatedNode.Annotations[InterruptionTimeAnnotation])
	assert.NoError(t, err)
}

func TestGetInstanceIDFromNode(t *testing.T) {
	tests := []struct {
		name     string
		node     *v1.Node
		expected string
	}{
		{
			name: "instance ID from provider ID",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: "ibm:///eu-de-2/02c7_12345678-1234-1234-1234-123456789abc",
				},
			},
			expected: "02c7_12345678-1234-1234-1234-123456789abc",
		},
		{
			name: "instance ID from label",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"ibm-cloud.kubernetes.io/instance-id": "02c7_87654321-4321-4321-4321-cba987654321",
					},
				},
			},
			expected: "02c7_87654321-4321-4321-4321-cba987654321",
		},
		{
			name: "instance ID from annotation",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"ibm-cloud.kubernetes.io/instance-id": "02c7_11111111-2222-3333-4444-555555555555",
					},
				},
			},
			expected: "02c7_11111111-2222-3333-4444-555555555555",
		},
		{
			name:     "no instance ID available",
			node:     &v1.Node{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &Controller{}
			result := controller.getInstanceIDFromNode(tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCheckCapacitySignals(t *testing.T) {
	tests := []struct {
		name     string
		node     *v1.Node
		expected InterruptionReason
	}{
		{
			name: "IBM capacity annotation should return capacity unavailable",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"ibm-cloud.kubernetes.io/status": "capacity unavailable",
					},
				},
			},
			expected: CapacityUnavailable,
		},
		{
			name: "IBM network annotation should return network resource limit",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"ibm-cloud.kubernetes.io/error": "network resources unavailable",
					},
				},
			},
			expected: NetworkResourceLimit,
		},
		{
			name: "maintenance annotation should return host maintenance",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"ibm-cloud.kubernetes.io/maintenance": "true",
					},
				},
			},
			expected: HostMaintenance,
		},
		{
			name: "no relevant annotations should return empty",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"some.other.annotation": "value",
					},
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &Controller{}
			result := controller.checkCapacitySignals(tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRegister(t *testing.T) {
	// Test that Register doesn't panic and returns no error
	controller := &Controller{}

	// We can't easily test the full registration without a real manager,
	// but we can test that the method exists and doesn't panic
	assert.NotPanics(t, func() {
		// This will fail because we're passing nil, but it won't panic
		err := controller.Register(context.Background(), nil)
		// We expect an error here since we're passing nil manager
		assert.Error(t, err)
	})
}

// Helper functions and mocks

func nodeSliceToObjects(nodes []v1.Node) []client.Object {
	objects := make([]client.Object, len(nodes))
	for i, node := range nodes {
		nodeCopy := node
		objects[i] = &nodeCopy
	}
	return objects
}

// mockErrorClient is a mock client that returns errors for testing
type mockErrorClient struct {
	client.Client
}

func (m *mockErrorClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return fmt.Errorf("mock error")
}

func (m *mockErrorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return assert.AnError
}

func (m *mockErrorClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return assert.AnError
}

func (m *mockErrorClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return assert.AnError
}

func (m *mockErrorClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return assert.AnError
}

func (m *mockErrorClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return assert.AnError
}

func (m *mockErrorClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return assert.AnError
}

func (m *mockErrorClient) Status() client.SubResourceWriter {
	return &mockSubResourceWriter{}
}

func (m *mockErrorClient) SubResource(subResource string) client.SubResourceClient {
	return &mockSubResourceClient{}
}

// mockSubResourceWriter implements client.SubResourceWriter
type mockSubResourceWriter struct{}

func (m *mockSubResourceWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return assert.AnError
}

func (m *mockSubResourceWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return assert.AnError
}

func (m *mockSubResourceWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return assert.AnError
}

// mockSubResourceClient implements client.SubResourceClient
type mockSubResourceClient struct{}

func (m *mockSubResourceClient) Get(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceGetOption) error {
	return assert.AnError
}

func (m *mockSubResourceClient) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return assert.AnError
}

func (m *mockSubResourceClient) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return assert.AnError
}

func (m *mockSubResourceClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return assert.AnError
}

func (m *mockErrorClient) Scheme() *runtime.Scheme {
	return runtime.NewScheme()
}
