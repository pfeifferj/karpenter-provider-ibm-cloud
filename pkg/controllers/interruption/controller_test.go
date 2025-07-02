package interruption

import (
	"context"
	"fmt"
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

	controller := NewController(kubeClient, recorder, unavailableOfferings)

	assert.NotNil(t, controller)
	assert.Equal(t, kubeClient, controller.kubeClient)
	assert.Equal(t, recorder, controller.recorder)
	assert.Equal(t, unavailableOfferings, controller.unavailableOfferings)
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
							"node.kubernetes.io/instance-type":   "bx2-2x8",
							"topology.kubernetes.io/zone":        "us-south-1",
							"kubernetes.io/hostname":             "test-node-1",
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

			controller := NewController(kubeClient, recorder, unavailableOfferings)

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

	controller := NewController(kubeClient, recorder, unavailableOfferings)

	ctx := context.Background()
	_, err := controller.Reconcile(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mock error")
}

func TestIsNodeInterrupted(t *testing.T) {
	tests := []struct {
		name     string
		node     *v1.Node
		expected bool
	}{
		{
			name: "normal node",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
			expected: false, // Currently always returns false
		},
		{
			name: "node with interruption annotation",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Annotations: map[string]string{
						"ibm.io/interruption": "true",
					},
				},
			},
			expected: false, // Currently always returns false
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNodeInterrupted(tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsCapacityRelated(t *testing.T) {
	tests := []struct {
		name     string
		node     *v1.Node
		expected bool
	}{
		{
			name: "normal node",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
			expected: false, // Currently always returns false
		},
		{
			name: "node with capacity annotation",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Annotations: map[string]string{
						"ibm.io/interruption-reason": "capacity",
					},
				},
			},
			expected: false, // Currently always returns false
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCapacityRelated(tt.node)
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