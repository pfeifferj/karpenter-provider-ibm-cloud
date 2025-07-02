package status

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

func getTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	return scheme
}

func getValidNodeClass() *v1alpha1.IBMNodeClass {
	return &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "valid-nodeclass",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region: "us-south",
			VPC:    "test-vpc-id",
			Image:  "test-image-id",
		},
	}
}

func getInvalidNodeClass() *v1alpha1.IBMNodeClass {
	return &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "invalid-nodeclass",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			// Missing required fields
		},
	}
}

func TestNewController(t *testing.T) {
	tests := []struct {
		name        string
		kubeClient  client.Client
		expectError bool
	}{
		{
			name:        "valid client",
			kubeClient:  fake.NewClientBuilder().Build(),
			expectError: false,
		},
		{
			name:        "nil client",
			kubeClient:  nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller, err := NewController(tt.kubeClient)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, controller)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, controller)
			}
		})
	}
}

func TestController_Reconcile(t *testing.T) {
	tests := []struct {
		name              string
		nodeClass         *v1alpha1.IBMNodeClass
		expectError       bool
		expectedReadyStatus metav1.ConditionStatus
		expectedReason      string
	}{
		{
			name:                "valid nodeclass",
			nodeClass:           getValidNodeClass(),
			expectError:         false,
			expectedReadyStatus: metav1.ConditionTrue,
			expectedReason:      "Ready",
		},
		{
			name:                "invalid nodeclass - missing region",
			nodeClass:           getInvalidNodeClass(),
			expectError:         false,
			expectedReadyStatus: metav1.ConditionFalse,
			expectedReason:      "ValidationFailed",
		},
		{
			name:      "nodeclass not found",
			nodeClass: nil,
			expectError: false, // Should not error, just ignore
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := getTestScheme()

			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.nodeClass != nil {
				builder = builder.WithRuntimeObjects(tt.nodeClass).WithStatusSubresource(&v1alpha1.IBMNodeClass{})
			}
			fakeClient := builder.Build()

			controller, err := NewController(fakeClient)
			require.NoError(t, err)

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "test-nodeclass",
				},
			}
			if tt.nodeClass != nil {
				req.Name = tt.nodeClass.Name
			}

			result, err := controller.Reconcile(ctx, req)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, reconcile.Result{}, result)

				// If nodeclass was provided, check the status was updated
				if tt.nodeClass != nil {
					var updatedNC v1alpha1.IBMNodeClass
					err := fakeClient.Get(ctx, types.NamespacedName{Name: tt.nodeClass.Name}, &updatedNC)
					require.NoError(t, err)

					// Check that status was updated
					assert.NotNil(t, updatedNC.Status.LastValidationTime)
					require.Len(t, updatedNC.Status.Conditions, 1)
					
					condition := updatedNC.Status.Conditions[0]
					assert.Equal(t, "Ready", condition.Type)
					assert.Equal(t, tt.expectedReadyStatus, condition.Status)
					assert.Equal(t, tt.expectedReason, condition.Reason)

					if tt.expectedReadyStatus == metav1.ConditionFalse {
						assert.NotEmpty(t, updatedNC.Status.ValidationError)
					} else {
						assert.Empty(t, updatedNC.Status.ValidationError)
					}
				}
			}
		})
	}
}

func TestController_validateNodeClass(t *testing.T) {
	tests := []struct {
		name        string
		nodeClass   *v1alpha1.IBMNodeClass
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid nodeclass",
			nodeClass:   getValidNodeClass(),
			expectError: false,
		},
		{
			name: "missing region",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					VPC:   "test-vpc",
					Image: "test-image",
				},
			},
			expectError: true,
			errorMsg:    "region is required",
		},
		{
			name: "missing image",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					VPC:    "test-vpc",
				},
			},
			expectError: true,
			errorMsg:    "image is required",
		},
		{
			name: "missing vpc",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					Image:  "test-image",
				},
			},
			expectError: true,
			errorMsg:    "vpc is required",
		},
		{
			name: "optional fields missing - should pass",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					VPC:    "test-vpc",
					Image:  "test-image",
					// instanceProfile and subnet are optional
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			controller := &Controller{}

			err := controller.validateNodeClass(ctx, tt.nodeClass)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestController_Register(t *testing.T) {
	scheme := getTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	
	controller, err := NewController(fakeClient)
	require.NoError(t, err)

	// Since we can't easily test the manager registration without a real manager,
	// we'll just verify the controller was created successfully
	assert.NotNil(t, controller)
}

func TestController_ReconcileWithRealValidation(t *testing.T) {
	ctx := context.Background()
	scheme := getTestScheme()

	// Test multiple validation scenarios
	testCases := []struct {
		name      string
		nodeClass *v1alpha1.IBMNodeClass
		isValid   bool
	}{
		{
			name: "completely valid",
			nodeClass: &v1alpha1.IBMNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "valid"},
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					VPC:    "vpc-12345",
					Image:  "r006-image-12345",
					Zone:   "us-south-1", // optional
				},
			},
			isValid: true,
		},
		{
			name: "empty region",
			nodeClass: &v1alpha1.IBMNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "no-region"},
				Spec: v1alpha1.IBMNodeClassSpec{
					VPC:   "vpc-12345",
					Image: "r006-image-12345",
				},
			},
			isValid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tc.nodeClass).
				WithStatusSubresource(&v1alpha1.IBMNodeClass{}).
				Build()

			controller, err := NewController(fakeClient)
			require.NoError(t, err)

			_, err = controller.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: tc.nodeClass.Name},
			})
			assert.NoError(t, err)

			// Verify status was updated correctly
			var updated v1alpha1.IBMNodeClass
			err = fakeClient.Get(ctx, types.NamespacedName{Name: tc.nodeClass.Name}, &updated)
			require.NoError(t, err)

			require.Len(t, updated.Status.Conditions, 1)
			condition := updated.Status.Conditions[0]

			if tc.isValid {
				assert.Equal(t, metav1.ConditionTrue, condition.Status)
				assert.Equal(t, "Ready", condition.Reason)
				assert.Empty(t, updated.Status.ValidationError)
			} else {
				assert.Equal(t, metav1.ConditionFalse, condition.Status)
				assert.Equal(t, "ValidationFailed", condition.Reason)
				assert.NotEmpty(t, updated.Status.ValidationError)
			}
		})
	}
}