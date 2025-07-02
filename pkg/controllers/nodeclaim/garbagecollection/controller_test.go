package garbagecollection

import (
	"context"
	"testing"

	"github.com/awslabs/operatorpkg/status"
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
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

// mockCloudProvider is a test implementation of cloudprovider.CloudProvider
type mockCloudProvider struct{}

func (m *mockCloudProvider) Create(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (*karpenterv1.NodeClaim, error) {
	return nodeClaim, nil
}

func (m *mockCloudProvider) Delete(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) error {
	return nil
}

func (m *mockCloudProvider) Get(ctx context.Context, providerID string) (*karpenterv1.NodeClaim, error) {
	return nil, nil
}

func (m *mockCloudProvider) GetInstanceTypes(ctx context.Context, nodePool *karpenterv1.NodePool) ([]*cloudprovider.InstanceType, error) {
	return nil, nil
}

func (m *mockCloudProvider) IsDrifted(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (cloudprovider.DriftReason, error) {
	return "", nil
}

func (m *mockCloudProvider) Name() string {
	return "mock"
}

func (m *mockCloudProvider) GetSupportedNodeClasses() []status.Object {
	return nil
}

func (m *mockCloudProvider) RepairPolicies() []cloudprovider.RepairPolicy {
	return nil
}

func (m *mockCloudProvider) List(ctx context.Context) ([]*karpenterv1.NodeClaim, error) {
	return nil, nil
}

func TestController_Name(t *testing.T) {
	controller := &Controller{}
	assert.Equal(t, "nodeclaim.garbagecollection", controller.Name())
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
	controller := NewController(fakeClient, &mockCloudProvider{})
	
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
			name: "NodeClaim without deletion timestamp",
			nodeClaims: []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-nodeclaim-1",
						Finalizers: []string{"karpenter.ibm.sh/nodeclaim"},
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node-1",
					},
				},
			},
			nodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node-1",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "NodeClaim with deletion timestamp and no node",
			nodeClaims: []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-nodeclaim-2",
						DeletionTimestamp: &metav1.Time{},
						Finalizers:        []string{"karpenter.ibm.sh/nodeclaim"},
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "",
					},
				},
			},
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
			controller := NewController(fakeClient, &mockCloudProvider{})

			// Run reconciliation
			_, err := controller.Reconcile(context.Background())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestController_removeFinalizer(t *testing.T) {
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
		nodeClaim  *karpenterv1.NodeClaim
		wantErr    bool
		expectCall bool
	}{
		{
			name: "NodeClaim without finalizer",
			nodeClaim: &karpenterv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-nodeclaim",
					Finalizers: []string{"other-finalizer"},
				},
			},
			wantErr:    false,
			expectCall: false,
		},
		{
			name: "NodeClaim with finalizer",
			nodeClaim: &karpenterv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-nodeclaim",
					Finalizers: []string{"karpenter.ibm.sh/nodeclaim", "other-finalizer"},
				},
			},
			wantErr:    false,
			expectCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with the NodeClaim
			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tt.nodeClaim).
				Build()

			// Create controller
			controller := &Controller{kubeClient: fakeClient}

			// Call removeFinalizer
			err := controller.removeFinalizer(context.Background(), tt.nodeClaim)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// If we expected the finalizer to be removed, verify it
			if tt.expectCall {
				assert.NotContains(t, tt.nodeClaim.Finalizers, "karpenter.ibm.sh/nodeclaim")
			}
		})
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("containsString", func(t *testing.T) {
		tests := []struct {
			slice    []string
			str      string
			expected bool
		}{
			{[]string{"a", "b", "c"}, "b", true},
			{[]string{"a", "b", "c"}, "d", false},
			{[]string{}, "a", false},
			{nil, "a", false},
		}

		for _, tt := range tests {
			result := containsString(tt.slice, tt.str)
			assert.Equal(t, tt.expected, result)
		}
	})

	t.Run("removeString", func(t *testing.T) {
		tests := []struct {
			slice    []string
			str      string
			expected []string
		}{
			{[]string{"a", "b", "c"}, "b", []string{"a", "c"}},
			{[]string{"a", "b", "c"}, "d", []string{"a", "b", "c"}},
			{[]string{"a"}, "a", []string{}},
			{[]string{}, "a", []string{}},
		}

		for _, tt := range tests {
			result := removeString(tt.slice, tt.str)
			assert.Equal(t, tt.expected, result)
		}
	})
}