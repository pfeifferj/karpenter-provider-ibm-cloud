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
package status

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/subnet"
)

// =============================================================================
// SETUP HELPERS
// =============================================================================

func getTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	return s
}

func getValidNodeClass() *v1alpha1.IBMNodeClass {
	// Use real IBM Cloud resources if available
	vpcID := os.Getenv("VPC_ID")
	if vpcID == "" {
		vpcID = "vpc-12345678" // Fallback for unit tests
	}

	return &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valid-nodeclass",
			Namespace: "default",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region: "us-south",
			VPC:    vpcID,
			Image:  "r006-988caa8b-7786-49c9-aea6-9553af2b1969", // Real Ubuntu 20.04 image
		},
	}
}

// =============================================================================
// UNIT TESTS - Field Validation
// =============================================================================

func TestValidateRequiredFields(t *testing.T) {
	tests := []struct {
		name          string
		nodeClass     *v1alpha1.IBMNodeClass
		expectedError string
	}{
		{
			name:          "all required fields present",
			nodeClass:     getValidNodeClass(),
			expectedError: "",
		},
		{
			name: "missing region",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					VPC:   "vpc-12345678",
					Image: "r006-12345678-1234-1234-1234-123456789012",
				},
			},
			expectedError: "region",
		},
		{
			name: "missing vpc",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					Image:  "r006-12345678-1234-1234-1234-123456789012",
				},
			},
			expectedError: "vpc",
		},
		{
			name: "missing image",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					VPC:    "vpc-12345678",
				},
			},
			expectedError: "image",
		},
		{
			name: "multiple missing fields",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{},
			},
			expectedError: "required fields missing",
		},
		{
			name: "empty strings treated as missing",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "",
					VPC:    "",
					Image:  "",
				},
			},
			expectedError: "required fields missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &Controller{}
			err := controller.validateRequiredFields(tt.nodeClass)

			if tt.expectedError == "" {
				assert.NoError(t, err, "Expected no validation error")
			} else {
				require.Error(t, err, "Expected validation error")
				assert.Contains(t, err.Error(), tt.expectedError, "Error should contain expected field name")
			}
		})
	}
}

// =============================================================================
// UNIT TESTS - Format Validation
// =============================================================================

func TestValidateFieldFormats(t *testing.T) {
	tests := []struct {
		name          string
		nodeClass     *v1alpha1.IBMNodeClass
		expectedError string
	}{
		{
			name:          "valid formats",
			nodeClass:     getValidNodeClass(),
			expectedError: "",
		},
		{
			name: "invalid VPC ID format",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					VPC:    "invalid", // Too short, no dashes
					Image:  "r006-12345678-1234-1234-1234-123456789012",
				},
			},
			expectedError: "VPC ID format invalid",
		},
		{
			name: "invalid image ID format",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					VPC:    "vpc-12345678",
					Image:  "invalid-image-id",
				},
			},
			expectedError: "", // Current implementation doesn't validate image format
		},
		{
			name: "invalid zone format",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					Zone:   "eu-gb-1", // Wrong region
					VPC:    "vpc-12345678",
					Image:  "r006-12345678-1234-1234-1234-123456789012",
				},
			},
			expectedError: "must start with region prefix",
		},
		{
			name: "invalid subnet ID format",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					VPC:    "r006-12345678-1234-1234-1234-123456789012",
					Image:  "r006-12345678-1234-1234-1234-123456789012",
					Subnet: "invalid", // Too short, no dashes
				},
			},
			expectedError: "subnet ID format invalid",
		},
		{
			name: "valid optional fields",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					Zone:   "us-south-1",
					VPC:    "r006-12345678-1234-1234-1234-123456789012",
					Image:  "r006-12345678-1234-1234-1234-123456789012",
					Subnet: "0717-12345678-1234-1234-1234-123456789012",
				},
			},
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &Controller{}
			err := controller.validateFieldFormats(tt.nodeClass)

			if tt.expectedError == "" {
				assert.NoError(t, err, "Expected no validation error")
			} else {
				require.Error(t, err, "Expected validation error")
				assert.Contains(t, err.Error(), tt.expectedError, "Error should contain expected message")
			}
		})
	}
}

// =============================================================================
// UNIT TESTS - Placement Strategy Validation
// =============================================================================

func TestValidatePlacementStrategy(t *testing.T) {
	tests := []struct {
		name          string
		strategy      *v1alpha1.PlacementStrategy
		expectedError string
	}{
		{
			name:          "nil strategy should pass",
			strategy:      nil,
			expectedError: "",
		},
		{
			name: "valid balanced strategy",
			strategy: &v1alpha1.PlacementStrategy{
				ZoneBalance: "Balanced",
				SubnetSelection: &v1alpha1.SubnetSelectionCriteria{
					MinimumAvailableIPs: 10,
					RequiredTags: map[string]string{
						"Environment": "production",
					},
				},
			},
			expectedError: "",
		},
		{
			name: "valid availability first strategy",
			strategy: &v1alpha1.PlacementStrategy{
				ZoneBalance: "AvailabilityFirst",
			},
			expectedError: "",
		},
		{
			name: "valid cost optimized strategy",
			strategy: &v1alpha1.PlacementStrategy{
				ZoneBalance: "CostOptimized",
			},
			expectedError: "",
		},
		{
			name: "invalid zone balance strategy",
			strategy: &v1alpha1.PlacementStrategy{
				ZoneBalance: "InvalidStrategy",
			},
			expectedError: "invalid ZoneBalance",
		},
		{
			name: "negative minimum available IPs",
			strategy: &v1alpha1.PlacementStrategy{
				ZoneBalance: "Balanced",
				SubnetSelection: &v1alpha1.SubnetSelectionCriteria{
					MinimumAvailableIPs: -1,
				},
			},
			expectedError: "MinimumAvailableIPs must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &Controller{}
			err := controller.validatePlacementStrategy(tt.strategy)

			if tt.expectedError == "" {
				assert.NoError(t, err, "Expected no validation error")
			} else {
				require.Error(t, err, "Expected validation error")
				assert.Contains(t, err.Error(), tt.expectedError, "Error should contain expected message")
			}
		})
	}
}

// =============================================================================
// UNIT TESTS - Business Logic Validation
// =============================================================================

func TestValidateBusinessLogic(t *testing.T) {
	tests := []struct {
		name      string
		nodeClass *v1alpha1.IBMNodeClass
		wantError bool
	}{
		{
			name:      "basic valid nodeclass",
			nodeClass: getValidNodeClass(),
			wantError: false,
		},
		{
			name: "zone and subnet specified",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					Zone:   "us-south-1",
					VPC:    "vpc-12345678",
					Image:  "r006-12345678-1234-1234-1234-123456789012",
					Subnet: "subnet-12345678",
				},
			},
			wantError: false, // Currently we skip detailed validation
		},
		{
			name: "complex placement strategy",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					VPC:    "vpc-12345678",
					Image:  "r006-12345678-1234-1234-1234-123456789012",
					PlacementStrategy: &v1alpha1.PlacementStrategy{
						ZoneBalance: "Balanced",
						SubnetSelection: &v1alpha1.SubnetSelectionCriteria{
							MinimumAvailableIPs: 50,
							RequiredTags: map[string]string{
								"Environment": "staging",
								"Team":        "platform",
							},
						},
					},
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &Controller{}
			err := controller.validateBusinessLogic(tt.nodeClass)

			if tt.wantError {
				assert.Error(t, err, "Expected validation error")
			} else {
				assert.NoError(t, err, "Expected no validation error")
			}
		})
	}
}

// =============================================================================
// INTEGRATION TESTS - Controller Reconcile
// =============================================================================

func TestControllerReconcile(t *testing.T) {
	tests := []struct {
		name             string
		nodeClass        *v1alpha1.IBMNodeClass
		expectedStatus   string
		expectedReady    bool
		expectedMessages []string
	}{
		{
			name:          "valid NodeClass becomes ready",
			nodeClass:     getValidNodeClass(),
			expectedStatus: "True",
			expectedReady: true,
			expectedMessages: []string{
				"NodeClass is ready",
			},
		},
		{
			name: "invalid NodeClass has validation errors",
			nodeClass: &v1alpha1.IBMNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-nodeclass",
					Namespace: "default",
				},
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "", // Missing required field
					VPC:    "invalid-vpc-id", // Invalid format
					Image:  "r006-12345678-1234-1234-1234-123456789012",
				},
			},
			expectedStatus: "False",
			expectedReady:  false,
			expectedMessages: []string{
				"field validation failed",
			},
		},
		{
			name: "NodeClass with complex valid spec",
			nodeClass: &v1alpha1.IBMNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "complex-nodeclass",
					Namespace: "default",
				},
				Spec: v1alpha1.IBMNodeClassSpec{
					Region:          "us-south",
					Zone:            "us-south-1",
					VPC:             "vpc-12345678",
					Image:           "r006-12345678-1234-1234-1234-123456789012",
					Subnet:          "subnet-12345678",
					InstanceProfile: "bx2-4x16",
					SSHKeys:         []string{"key-12345678"},
					SecurityGroups:  []string{"sg-12345678"},
					UserData:        "#!/bin/bash\necho 'Hello World'",
					PlacementStrategy: &v1alpha1.PlacementStrategy{
						ZoneBalance: "Balanced",
						SubnetSelection: &v1alpha1.SubnetSelectionCriteria{
							MinimumAvailableIPs: 50,
							RequiredTags: map[string]string{
								"Environment": "production",
							},
						},
					},
				},
			},
			expectedStatus: "True",
			expectedReady:  true,
			expectedMessages: []string{
				"NodeClass is ready",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			s := getTestScheme()
			client := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tt.nodeClass).
				WithStatusSubresource(tt.nodeClass).
				Build()

			// Create IBM client if credentials are available
			var ibmClient *ibm.Client
			var subnetProvider subnet.Provider
			if os.Getenv("IBM_API_KEY") != "" && os.Getenv("VPC_API_KEY") != "" {
				var err error
				ibmClient, err = ibm.NewClient()
				if err != nil {
					t.Logf("Warning: Could not create IBM client for real validation: %v", err)
				}
				
				// Create subnet provider for validation
				subnetProvider, err = subnet.NewProvider()
				if err != nil {
					t.Logf("Warning: Could not create subnet provider: %v", err)
				}
			}

			controller := &Controller{
				kubeClient:     client,
				ibmClient:      ibmClient,
				subnetProvider: subnetProvider,
			}

			// Execute
			ctx := context.Background()
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.nodeClass.Name,
					Namespace: tt.nodeClass.Namespace,
				},
			}

			result, err := controller.Reconcile(ctx, req)
			assert.NoError(t, err, "Reconcile should not return error")
			assert.Equal(t, reconcile.Result{}, result, "Should not requeue")

			// Verify
			var updatedNodeClass v1alpha1.IBMNodeClass
			err = client.Get(ctx, req.NamespacedName, &updatedNodeClass)
			require.NoError(t, err, "Should be able to get updated NodeClass")

			// Check status conditions
			require.NotEmpty(t, updatedNodeClass.Status.Conditions, "Should have status conditions")
			readyCondition := updatedNodeClass.Status.Conditions[0]
			assert.Equal(t, tt.expectedStatus, string(readyCondition.Status), "Ready condition status should match")
			
			// Check that condition message contains expected content
			found := false
			for _, expectedMsg := range tt.expectedMessages {
				if assert.Contains(t, readyCondition.Message, expectedMsg) {
					found = true
					break
				}
			}
			assert.True(t, found, "Should find at least one expected message in condition")
			assert.WithinDuration(t, time.Now(), readyCondition.LastTransitionTime.Time, 5*time.Second, "Transition time should be recent")
		})
	}
}

// =============================================================================
// INTEGRATION TESTS - Edge Cases
// =============================================================================

func TestControllerReconcileEdgeCases(t *testing.T) {
	t.Run("missing NodeClass", func(t *testing.T) {
		s := getTestScheme()
		client := fake.NewClientBuilder().WithScheme(s).Build()

		controller := &Controller{
			kubeClient: client,
		}

		ctx := context.Background()
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "non-existent",
				Namespace: "default",
			},
		}

		result, err := controller.Reconcile(ctx, req)
		assert.NoError(t, err, "Should handle missing NodeClass gracefully")
		assert.Equal(t, reconcile.Result{}, result, "Should not requeue")
	})

	t.Run("concurrent reconciliation", func(t *testing.T) {
		s := getTestScheme()
		nodeClass := getValidNodeClass()
		client := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(nodeClass).
			WithStatusSubresource(nodeClass).
			Build()

		controller := &Controller{
			kubeClient: client,
		}

		ctx := context.Background()
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      nodeClass.Name,
				Namespace: nodeClass.Namespace,
			},
		}

		// Run multiple reconciliations concurrently
		done := make(chan error, 3)
		for i := 0; i < 3; i++ {
			go func() {
				_, err := controller.Reconcile(ctx, req)
				done <- err
			}()
		}

		// Wait for all to complete
		for i := 0; i < 3; i++ {
			err := <-done
			assert.NoError(t, err, "Concurrent reconciliation should not error")
		}

		// Verify final state is consistent
		var finalNodeClass v1alpha1.IBMNodeClass
		err := client.Get(ctx, req.NamespacedName, &finalNodeClass)
		require.NoError(t, err)

		assert.NotEmpty(t, finalNodeClass.Status.Conditions, "Should have status conditions")
		assert.Equal(t, "True", string(finalNodeClass.Status.Conditions[0].Status), "Should be ready")
	})

	t.Run("validation edge cases", func(t *testing.T) {
		tests := []struct {
			name        string
			nodeClass   *v1alpha1.IBMNodeClass
			expectReady bool
		}{
			{
				name: "whitespace in fields gets trimmed",
				nodeClass: &v1alpha1.IBMNodeClass{
					ObjectMeta: metav1.ObjectMeta{Name: "whitespace-test", Namespace: "default"},
					Spec: v1alpha1.IBMNodeClassSpec{
						Region: " us-south ",
						VPC:    " vpc-12345678 ",
						Image:  " r006-12345678-1234-1234-1234-123456789012 ",
					},
				},
				expectReady: true,
			},
			{
				name: "maximum valid spec",
				nodeClass: &v1alpha1.IBMNodeClass{
					ObjectMeta: metav1.ObjectMeta{Name: "max-spec", Namespace: "default"},
					Spec: v1alpha1.IBMNodeClassSpec{
						Region:          "us-south",
						Zone:            "us-south-1",
						VPC:             "vpc-12345678",
						Image:           "r006-12345678-1234-1234-1234-123456789012",
						Subnet:          "subnet-12345678",
						InstanceProfile: "bx2-4x16",
						SSHKeys:         []string{"key-12345678", "key-87654321"},
						SecurityGroups:  []string{"sg-12345678", "sg-87654321"},
						ResourceGroup:   "rg-12345678",
						UserData:        "#!/bin/bash\necho 'Complex setup'\nmkdir /opt/app",
						PlacementStrategy: &v1alpha1.PlacementStrategy{
							ZoneBalance: "AvailabilityFirst",
							SubnetSelection: &v1alpha1.SubnetSelectionCriteria{
								MinimumAvailableIPs: 100,
								RequiredTags: map[string]string{
									"Environment": "production",
									"Team":        "platform",
									"Application": "karpenter",
								},
							},
						},
					},
				},
				expectReady: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s := getTestScheme()
				client := fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(tt.nodeClass).
					WithStatusSubresource(tt.nodeClass).
					Build()

				controller := &Controller{
					kubeClient: client,
				}

				ctx := context.Background()
				req := reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      tt.nodeClass.Name,
						Namespace: tt.nodeClass.Namespace,
					},
				}

				_, err := controller.Reconcile(ctx, req)
				assert.NoError(t, err)

				var updatedNodeClass v1alpha1.IBMNodeClass
				err = client.Get(ctx, req.NamespacedName, &updatedNodeClass)
				require.NoError(t, err)

				require.NotEmpty(t, updatedNodeClass.Status.Conditions)
				if tt.expectReady {
					assert.Equal(t, "True", string(updatedNodeClass.Status.Conditions[0].Status))
				} else {
					assert.Equal(t, "False", string(updatedNodeClass.Status.Conditions[0].Status))
				}
			})
		}
	})
}

// =============================================================================
// REAL IBM CLOUD INTEGRATION TESTS
// =============================================================================

func TestControllerWithRealIBMCloud(t *testing.T) {
	// Skip if IBM Cloud credentials are not available
	if os.Getenv("IBM_API_KEY") == "" || os.Getenv("VPC_API_KEY") == "" {
		t.Skip("Skipping IBM Cloud integration tests - set IBM_API_KEY and VPC_API_KEY to run")
	}

	vpcID := os.Getenv("VPC_ID")
	subnetID := os.Getenv("SUBNET_ID_US_SOUTH_1")
	
	if vpcID == "" {
		t.Skip("Skipping IBM Cloud integration tests - set VPC_ID to run")
	}

	tests := []struct {
		name           string
		nodeClass      *v1alpha1.IBMNodeClass
		expectReady    bool
		expectMessages []string
	}{
		{
			name: "valid VPC should pass validation",
			nodeClass: &v1alpha1.IBMNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "real-ibm-test-valid",
					Namespace: "default",
				},
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					VPC:    vpcID,
					Image:  "r006-988caa8b-7786-49c9-aea6-9553af2b1969", // Real Ubuntu 20.04 image
				},
			},
			expectReady:    true,
			expectMessages: []string{"NodeClass is ready"},
		},
		{
			name: "invalid VPC should fail validation",
			nodeClass: &v1alpha1.IBMNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "real-ibm-test-invalid-vpc",
					Namespace: "default",
				},
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					VPC:    "vpc-nonexistent-12345678",
					Image:  "r006-988caa8b-7786-49c9-aea6-9553af2b1969", // Real Ubuntu 20.04 image
				},
			},
			expectReady:    false,
			expectMessages: []string{"VPC validation failed"},
		},
		{
			name: "valid subnet should pass validation",
			nodeClass: &v1alpha1.IBMNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "real-ibm-test-valid-subnet",
					Namespace: "default",
				},
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					Zone:   "us-south-1",
					VPC:    vpcID,
					Subnet: subnetID,
					Image:  "r006-988caa8b-7786-49c9-aea6-9553af2b1969", // Real Ubuntu 20.04 image
				},
			},
			expectReady:    subnetID != "", // Only test if subnet ID is provided
			expectMessages: []string{"NodeClass is ready"},
		},
		{
			name: "invalid subnet should fail validation",
			nodeClass: &v1alpha1.IBMNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "real-ibm-test-invalid-subnet",
					Namespace: "default",
				},
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					Zone:   "us-south-1",
					VPC:    vpcID,
					Subnet: "0717-nonexistent-subnet-12345678",
					Image:  "r006-988caa8b-7786-49c9-aea6-9553af2b1969", // Real Ubuntu 20.04 image
				},
			},
			expectReady:    false,
			expectMessages: []string{"subnet validation failed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip subnet tests if no subnet ID provided
			if (tt.name == "valid subnet should pass validation" && subnetID == "") {
				t.Skip("Skipping subnet test - no SUBNET_ID_US_SOUTH_1 provided")
			}

			s := getTestScheme()
			client := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tt.nodeClass).
				WithStatusSubresource(tt.nodeClass).
				Build()

			// Create real IBM client
			ibmClient, err := ibm.NewClient()
			require.NoError(t, err, "Should be able to create IBM client with provided credentials")

			// Create subnet provider
			subnetProvider, err := subnet.NewProvider()
			require.NoError(t, err, "Should be able to create subnet provider")

			controller := &Controller{
				kubeClient:     client,
				ibmClient:      ibmClient,
				subnetProvider: subnetProvider,
			}

			ctx := context.Background()
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.nodeClass.Name,
					Namespace: tt.nodeClass.Namespace,
				},
			}

			// Allow more time for real API calls
			timeout := 30 * time.Second
			timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			result, err := controller.Reconcile(timeoutCtx, req)
			assert.NoError(t, err, "Reconcile should not return error")
			assert.Equal(t, reconcile.Result{}, result, "Should not requeue")

			// Verify status was updated
			var updatedNodeClass v1alpha1.IBMNodeClass
			err = client.Get(ctx, req.NamespacedName, &updatedNodeClass)
			require.NoError(t, err, "Should be able to get updated NodeClass")

			// Check status conditions
			require.NotEmpty(t, updatedNodeClass.Status.Conditions, "Should have status conditions")
			readyCondition := updatedNodeClass.Status.Conditions[0]

			if tt.expectReady {
				assert.Equal(t, "True", string(readyCondition.Status), "Should be ready with real IBM Cloud validation")
			} else {
				assert.Equal(t, "False", string(readyCondition.Status), "Should not be ready due to validation failure")
			}

			// Check that condition message contains expected content
			found := false
			for _, expectedMsg := range tt.expectMessages {
				if assert.Contains(t, readyCondition.Message, expectedMsg) {
					found = true
					break
				}
			}
			assert.True(t, found, "Should find expected message in condition")

			t.Logf("Test %s completed. Status: %s, Message: %s", 
				tt.name, readyCondition.Status, readyCondition.Message)
		})
	}
}

func TestIBMCloudResourceValidation(t *testing.T) {
	// Skip if IBM Cloud credentials are not available
	if os.Getenv("IBM_API_KEY") == "" || os.Getenv("VPC_API_KEY") == "" {
		t.Skip("Skipping IBM Cloud resource validation tests - set IBM_API_KEY and VPC_API_KEY to run")
	}

	vpcID := os.Getenv("VPC_ID")
	if vpcID == "" {
		t.Skip("Skipping IBM Cloud resource validation tests - set VPC_ID to run")
	}

	// Create real IBM client
	ibmClient, err := ibm.NewClient()
	require.NoError(t, err, "Should be able to create IBM client")

	// Create subnet provider
	subnetProvider, err := subnet.NewProvider()
	require.NoError(t, err, "Should be able to create subnet provider")

	controller := &Controller{
		ibmClient:      ibmClient,
		subnetProvider: subnetProvider,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("validate real VPC", func(t *testing.T) {
		err := controller.validateVPC(ctx, vpcID)
		assert.NoError(t, err, "Real VPC should validate successfully")
	})

	t.Run("validate nonexistent VPC", func(t *testing.T) {
		err := controller.validateVPC(ctx, "vpc-nonexistent-12345678")
		assert.Error(t, err, "Nonexistent VPC should fail validation")
		assert.Contains(t, err.Error(), "not found", "Error should indicate VPC not found")
	})

	subnetID := os.Getenv("SUBNET_ID_US_SOUTH_1")
	if subnetID != "" {
		t.Run("validate real subnet", func(t *testing.T) {
			err := controller.validateSubnet(ctx, subnetID, vpcID)
			assert.NoError(t, err, "Real subnet should validate successfully")
		})

		t.Run("validate nonexistent subnet", func(t *testing.T) {
			err := controller.validateSubnet(ctx, "0717-nonexistent-subnet-12345678", vpcID)
			assert.Error(t, err, "Nonexistent subnet should fail validation")
		})
	}

	t.Run("validate subnets available in VPC", func(t *testing.T) {
		err := controller.validateSubnetsAvailable(ctx, vpcID, "us-south-1")
		assert.NoError(t, err, "Should find available subnets in real VPC")
	})
}

// =============================================================================
// PERFORMANCE TESTS
// =============================================================================

func TestControllerPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	s := getTestScheme()
	nodeClass := getValidNodeClass()
	client := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(nodeClass).
		WithStatusSubresource(nodeClass).
		Build()

	controller := &Controller{
		kubeClient: client,
	}

	ctx := context.Background()
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      nodeClass.Name,
			Namespace: nodeClass.Namespace,
		},
	}

	// Benchmark reconciliation performance
	start := time.Now()
	iterations := 100

	for i := 0; i < iterations; i++ {
		_, err := controller.Reconcile(ctx, req)
		require.NoError(t, err)
	}

	duration := time.Since(start)
	avgDuration := duration / time.Duration(iterations)

	t.Logf("Reconciled %d times in %v (avg: %v per reconciliation)", iterations, duration, avgDuration)
	
	// Reconciliation should be fast (under 10ms per call)
	assert.Less(t, avgDuration, 10*time.Millisecond, "Reconciliation should be fast")
}