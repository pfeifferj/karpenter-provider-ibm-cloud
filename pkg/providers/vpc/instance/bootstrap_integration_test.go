/*
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

package instance

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
)

func TestBootstrapIntegration(t *testing.T) {
	tests := []struct {
		name         string
		nodeClass    *v1alpha1.IBMNodeClass
		expectManual bool
		expectDynamic bool
	}{
		{
			name: "Manual userData provided - should use as-is",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					UserData: "#!/bin/bash\necho 'Manual bootstrap script'",
				},
			},
			expectManual: true,
			expectDynamic: false,
		},
		{
			name: "No userData provided - should generate dynamic bootstrap",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					// UserData is empty - should trigger automatic bootstrap
				},
			},
			expectManual: false,
			expectDynamic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock IBM client
			mockClient := &ibm.Client{} // In real test, this would be properly mocked

			// Create fake kube client
			kubeClient := fake.NewClientBuilder().Build()

			// Create VPC instance provider
			provider, err := NewVPCInstanceProvider(mockClient, kubeClient)
			assert.NoError(t, err)

			vpcProvider := provider.(*VPCInstanceProvider)

			// Test bootstrap user data generation
			userData, err := vpcProvider.generateBootstrapUserData(
				context.Background(),
				tt.nodeClass,
				types.NamespacedName{Name: "test-node", Namespace: "default"},
			)

			if tt.expectManual {
				// Should return the manual userData as-is
				assert.NoError(t, err)
				assert.Equal(t, tt.nodeClass.Spec.UserData, userData)
				assert.Contains(t, userData, "Manual bootstrap script")
			}

			if tt.expectDynamic {
				// Should attempt dynamic generation (will fallback to basic in test environment)
				assert.NoError(t, err)
				assert.Contains(t, userData, "Basic bootstrap for region")
				assert.Contains(t, userData, tt.nodeClass.Spec.Region)
				// In real environment with kubernetes client, would contain dynamic script
			}
		})
	}
}

func TestBootstrapBehavior(t *testing.T) {
	// Create mock IBM client
	mockClient := &ibm.Client{}
	kubeClient := fake.NewClientBuilder().Build()

	provider, err := NewVPCInstanceProvider(mockClient, kubeClient)
	assert.NoError(t, err)

	vpcProvider := provider.(*VPCInstanceProvider)

	t.Run("Basic bootstrap script contains required information", func(t *testing.T) {
		nodeClass := &v1alpha1.IBMNodeClass{
			Spec: v1alpha1.IBMNodeClassSpec{
				Region: "eu-de",
			},
		}

		basicScript := vpcProvider.getBasicBootstrapScript(nodeClass)

		// Verify basic script contains essential information
		assert.Contains(t, basicScript, "#!/bin/bash")
		assert.Contains(t, basicScript, "eu-de")
		assert.Contains(t, basicScript, "Bootstrap token")
		assert.Contains(t, basicScript, "Internal API endpoint")
		assert.Contains(t, basicScript, "hostname configuration")
	})

	t.Run("Manual userData takes precedence", func(t *testing.T) {
		nodeClass := &v1alpha1.IBMNodeClass{
			Spec: v1alpha1.IBMNodeClassSpec{
				Region: "us-south",
				UserData: "#!/bin/bash\necho 'Custom bootstrap'",
			},
		}

		userData, err := vpcProvider.generateBootstrapUserData(
			context.Background(),
			nodeClass,
			types.NamespacedName{Name: "test", Namespace: "default"},
		)

		assert.NoError(t, err)
		assert.Equal(t, nodeClass.Spec.UserData, userData)
		assert.Contains(t, userData, "Custom bootstrap")
	})
}

func TestBootstrapProviderLazyInitialization(t *testing.T) {
	mockClient := &ibm.Client{}
	kubeClient := fake.NewClientBuilder().Build()

	provider, err := NewVPCInstanceProvider(mockClient, kubeClient)
	assert.NoError(t, err)

	vpcProvider := provider.(*VPCInstanceProvider)

	// Initially, bootstrap provider should be nil
	assert.Nil(t, vpcProvider.bootstrapProvider)
	assert.Nil(t, vpcProvider.k8sClient)

	// After calling generateBootstrapUserData with empty userData, 
	// it should attempt to initialize (but fail in test environment)
	nodeClass := &v1alpha1.IBMNodeClass{
		Spec: v1alpha1.IBMNodeClassSpec{
			Region: "us-south",
			// Empty userData triggers bootstrap provider initialization
		},
	}

	userData, err := vpcProvider.generateBootstrapUserData(
		context.Background(),
		nodeClass,
		types.NamespacedName{Name: "test", Namespace: "default"},
	)

	// Should not error (falls back to basic bootstrap)
	assert.NoError(t, err)
	assert.NotEmpty(t, userData)

	// In test environment, it falls back to basic bootstrap
	assert.Contains(t, userData, "Basic bootstrap")
}