package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestNodePoolReconciler_Reconcile(t *testing.T) {
	ctx := context.Background()
	
	// Create scheme and add required types
	scheme := runtime.NewScheme()
	// Add core Karpenter types to scheme
	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	scheme.AddKnownTypes(gv, &v1.NodePool{}, &v1.NodePoolList{}, &v1.NodeClaim{}, &v1.NodeClaimList{})
	metav1.AddToGroupVersion(scheme, gv)

	tests := []struct {
		name           string
		nodePool       *v1.NodePool
		existingClaims []*v1.NodeClaim
		expectError    bool
		expectFinalizer bool
	}{
		{
			name: "new NodePool gets finalizer",
			nodePool: &v1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodepool",
				},
				Spec: v1.NodePoolSpec{
					Template: v1.NodeClaimTemplate{
						Spec: v1.NodeClaimTemplateSpec{
							NodeClassRef: &v1.NodeClassReference{
								Kind: "IBMNodeClass",
								Name: "test-nodeclass",
							},
						},
					},
				},
			},
			expectFinalizer: true,
		},
		{
			name: "NodePool with finalizer and no deletion timestamp",
			nodePool: &v1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-nodepool",
					Finalizers: []string{"karpenter.sh/finalizer"},
				},
				Spec: v1.NodePoolSpec{
					Template: v1.NodeClaimTemplate{
						Spec: v1.NodeClaimTemplateSpec{
							NodeClassRef: &v1.NodeClassReference{
								Kind: "IBMNodeClass",
								Name: "test-nodeclass",
							},
						},
					},
				},
			},
			expectFinalizer: true,
		},
		{
			name: "NodePool with deletion timestamp but no NodeClaims",
			nodePool: &v1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-nodepool",
					Finalizers:        []string{"karpenter.sh/finalizer"},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: v1.NodePoolSpec{
					Template: v1.NodeClaimTemplate{
						Spec: v1.NodeClaimTemplateSpec{
							NodeClassRef: &v1.NodeClassReference{
								Kind: "IBMNodeClass",
								Name: "test-nodeclass",
							},
						},
					},
				},
			},
			expectFinalizer: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create objects for the fake client
			clientObjs := []client.Object{tt.nodePool}
			for _, claim := range tt.existingClaims {
				clientObjs = append(clientObjs, claim)
			}

			// Create fake client
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(clientObjs...).
				WithStatusSubresource(&v1.NodePool{}).
				Build()

			// Create reconciler
			reconciler := &NodePoolReconciler{
				kubeClient: fakeClient,
			}

			// Run reconcile
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: tt.nodePool.Name,
				},
			}

			result, err := reconciler.Reconcile(ctx, req)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, reconcile.Result{}, result)
			}

			// Check finalizer state
			updatedNodePool := &v1.NodePool{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: tt.nodePool.Name}, updatedNodePool)
			if tt.expectFinalizer {
				assert.NoError(t, err)
				assert.Contains(t, updatedNodePool.Finalizers, "karpenter.sh/finalizer")
			} else if !tt.nodePool.DeletionTimestamp.IsZero() {
				// If deletion timestamp is set and we don't expect finalizer, object should be deleted
				assert.True(t, err != nil || len(updatedNodePool.Finalizers) == 0)
			}
		})
	}
}

func TestNodePoolReconciler_cleanupNodePoolResources(t *testing.T) {
	ctx := context.Background()
	
	// Create scheme and add required types
	scheme := runtime.NewScheme()
	// Add core Karpenter types to scheme
	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	scheme.AddKnownTypes(gv, &v1.NodePool{}, &v1.NodePoolList{}, &v1.NodeClaim{}, &v1.NodeClaimList{})
	metav1.AddToGroupVersion(scheme, gv)

	tests := []struct {
		name           string
		nodePool       *v1.NodePool
		existingClaims []*v1.NodeClaim
		expectError    bool
	}{
		{
			name: "cleanup with no NodeClaims",
			nodePool: &v1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodepool",
				},
			},
			existingClaims: nil,
			expectError:    false,
		},
		{
			name: "cleanup with existing NodeClaims",
			nodePool: &v1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodepool",
				},
			},
			existingClaims: []*v1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-claim-1",
					},
					Spec: v1.NodeClaimSpec{
						NodeClassRef: &v1.NodeClassReference{
							Kind:  "IBMNodeClass",
							Group: "karpenter.ibm.sh",
							Name:  "test-nodeclass",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-claim-2",
					},
					Spec: v1.NodeClaimSpec{
						NodeClassRef: &v1.NodeClassReference{
							Kind:  "IBMNodeClass",
							Group: "karpenter.ibm.sh",
							Name:  "test-nodeclass",
						},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create objects for the fake client
			clientObjs := []client.Object{tt.nodePool}
			for _, claim := range tt.existingClaims {
				clientObjs = append(clientObjs, claim)
			}

			// Create fake client with indexer for NodeClaims by NodePool
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(clientObjs...).
				Build()

			// Create reconciler
			reconciler := &NodePoolReconciler{
				kubeClient: fakeClient,
			}

			// Run cleanup
			err := reconciler.cleanupNodePoolResources(ctx, tt.nodePool)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNodeClaimReconciler_Reconcile(t *testing.T) {
	ctx := context.Background()
	
	// Create scheme and add required types
	scheme := runtime.NewScheme()
	// Add core Karpenter types to scheme
	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	scheme.AddKnownTypes(gv, &v1.NodePool{}, &v1.NodePoolList{}, &v1.NodeClaim{}, &v1.NodeClaimList{})
	metav1.AddToGroupVersion(scheme, gv)

	tests := []struct {
		name        string
		nodeClaim   *v1.NodeClaim
		expectError bool
	}{
		{
			name: "new NodeClaim gets finalizer",
			nodeClaim: &v1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclaim",
				},
				Spec: v1.NodeClaimSpec{
					NodeClassRef: &v1.NodeClassReference{
						Kind:  "IBMNodeClass",
						Group: "karpenter.ibm.sh",
						Name:  "test-nodeclass",
					},
				},
			},
			expectError: false,
		},
		{
			name: "NodeClaim with deletion timestamp",
			nodeClaim: &v1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-nodeclaim",
					Finalizers:        []string{"karpenter.sh/finalizer"},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: v1.NodeClaimSpec{
					NodeClassRef: &v1.NodeClassReference{
						Kind:  "IBMNodeClass",
						Group: "karpenter.ibm.sh",
						Name:  "test-nodeclass",
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.nodeClaim).
				WithStatusSubresource(&v1.NodeClaim{}).
				Build()

			// Create reconciler
			reconciler := &NodeClaimReconciler{
				kubeClient: fakeClient,
			}

			// Run reconcile
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: tt.nodeClaim.Name,
				},
			}

			result, err := reconciler.Reconcile(ctx, req)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, reconcile.Result{}, result)
			}
		})
	}
}

// Test helper to ensure the module compiles
func TestControllersCompile(t *testing.T) {
	// This test simply ensures that the controller types compile correctly
	// with the current Karpenter v1 API
	assert.NotNil(t, &NodePoolReconciler{})
	assert.NotNil(t, &NodeClaimReconciler{})
}