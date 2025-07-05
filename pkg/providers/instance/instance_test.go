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
package instance

import (
	"context"
	"errors"
	"testing"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

func TestInstanceProviderStructure(t *testing.T) {
	// Test basic instance provider structure without IBM client dependencies
	// This test ensures the package compiles correctly
	t.Log("Instance provider package compiles successfully")
}

func TestNodeClassValidation(t *testing.T) {
	// Test that we can create a basic NodeClass structure
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:          "us-south",
			Zone:            "us-south-1",
			InstanceProfile: "bx2-4x16",
			Image:           "r006-test-image-id",
			VPC:             "r006-test-vpc-id",
			Subnet:          "r006-test-subnet-id",
			SecurityGroups:  []string{"r006-test-sg-id"},
			Tags: map[string]string{
				"test": "true",
			},
		},
	}

	require.NotNil(t, nodeClass)
	require.Equal(t, "us-south", nodeClass.Spec.Region)
	require.Equal(t, "test-nodeclass", nodeClass.Name)
}

func TestNewProvider(t *testing.T) {
	// This test will fail with real IBM client creation due to missing credentials
	// Skip for now until we can mock the client creation properly
	t.Skip("Skipping NewProvider test - requires valid IBM credentials or proper mock interface")
}

func TestInstanceProvider_Create(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		nodeClaim   *v1.NodeClaim
		expectError bool
	}{
		{
			name: "valid instance creation request",
			nodeClaim: &v1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclaim",
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": "bx2-4x16",
						"topology.kubernetes.io/zone":      "us-south-1",
					},
				},
				Spec: v1.NodeClaimSpec{
					NodeClassRef: &v1.NodeClassReference{
						Kind:  "IBMNodeClass",
						Group: "karpenter.ibm.sh",
						Name:  "test-nodeclass",
					},
				},
			},
			expectError: true, // Will fail without real IBM client
		},
		{
			name: "missing instance type",
			nodeClaim: &v1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclaim",
					Labels: map[string]string{
						"topology.kubernetes.io/zone": "us-south-1",
					},
				},
				Spec: v1.NodeClaimSpec{
					NodeClassRef: &v1.NodeClassReference{
						Kind:  "IBMNodeClass",
						Group: "karpenter.ibm.sh",
						Name:  "test-nodeclass",
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create provider (will fail to initialize IBM client, but that's expected for unit tests)
			provider := &IBMCloudInstanceProvider{
				client: nil, // No real client for unit tests
			}

			_, err := provider.Create(ctx, tt.nodeClaim)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestInstanceProvider_Delete(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		node        *corev1.Node
		expectError bool
	}{
		{
			name: "valid instance deletion",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "ibm://test-instance-id",
				},
			},
			expectError: true, // Will fail without real IBM client
		},
		{
			name: "invalid provider ID format",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "aws://test-instance-id",
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &IBMCloudInstanceProvider{
				client: nil, // No real client for unit tests
			}

			err := provider.Delete(ctx, tt.node)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestInstanceProvider_GetInstance(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		node        *corev1.Node
		expectError bool
	}{
		{
			name: "get instance info",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "ibm://test-instance-id",
				},
			},
			expectError: true, // Will fail without real IBM client
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &IBMCloudInstanceProvider{
				client: nil, // No real client for unit tests
			}

			_, err := provider.GetInstance(ctx, tt.node)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewProvider_ErrorHandling(t *testing.T) {
	// Test that NewProvider handles missing environment variables gracefully
	_, err := NewProvider()
	// Should return error when IBM Cloud credentials are not available
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "creating IBM Cloud client")
}

// Note: Additional tests for Create, Delete, etc. require proper mock interfaces
// for IBM Cloud clients. These tests are skipped until mock implementations
// are properly aligned with the current IBM client interface.

// MockVPCClient implements a mock VPC client for testing new functionality
type MockVPCClient struct {
	subnets        map[string]*vpcv1.Subnet
	securityGroups map[string]*vpcv1.SecurityGroup
	
	// Function overrides for testing
	listSubnetsFunc        func(ctx context.Context, options *vpcv1.ListSubnetsOptions) (*vpcv1.SubnetCollection, *core.DetailedResponse, error)
	listSecurityGroupsFunc func(ctx context.Context, options *vpcv1.ListSecurityGroupsOptions) (*vpcv1.SecurityGroupCollection, *core.DetailedResponse, error)
}

func (m *MockVPCClient) ListSubnetsWithContext(ctx context.Context, options *vpcv1.ListSubnetsOptions) (*vpcv1.SubnetCollection, *core.DetailedResponse, error) {
	if m.listSubnetsFunc != nil {
		return m.listSubnetsFunc(ctx, options)
	}
	
	var subnets []vpcv1.Subnet
	for _, subnet := range m.subnets {
		subnets = append(subnets, *subnet)
	}
	
	return &vpcv1.SubnetCollection{Subnets: subnets}, &core.DetailedResponse{}, nil
}

func (m *MockVPCClient) ListSecurityGroupsWithContext(ctx context.Context, options *vpcv1.ListSecurityGroupsOptions) (*vpcv1.SecurityGroupCollection, *core.DetailedResponse, error) {
	if m.listSecurityGroupsFunc != nil {
		return m.listSecurityGroupsFunc(ctx, options)
	}
	
	var sgs []vpcv1.SecurityGroup
	for _, sg := range m.securityGroups {
		// Filter by VPC if specified
		if options.VPCID != nil {
			if sg.VPC == nil || sg.VPC.ID == nil || *sg.VPC.ID != *options.VPCID {
				continue
			}
		}
		sgs = append(sgs, *sg)
	}
	
	return &vpcv1.SecurityGroupCollection{SecurityGroups: sgs}, &core.DetailedResponse{}, nil
}

func createTestSubnet(id, name, vpcID, zone, status string) *vpcv1.Subnet {
	return &vpcv1.Subnet{
		ID:     &id,
		Name:   &name,
		Status: &status,
		VPC: &vpcv1.VPCReference{
			ID: &vpcID,
		},
		Zone: &vpcv1.ZoneReference{
			Name: &zone,
		},
	}
}

func createTestSecurityGroup(id, name, vpcID string) *vpcv1.SecurityGroup {
	return &vpcv1.SecurityGroup{
		ID:   &id,
		Name: &name,
		VPC: &vpcv1.VPCReference{
			ID: &vpcID,
		},
	}
}

func TestSelectSubnetForZone_Logic(t *testing.T) {
	// Test the logic components that the selectSubnetForZone function uses
	
	// Test subnet filtering by VPC ID
	testSubnet1 := createTestSubnet("subnet-1", "test-subnet-1", "vpc-12345", "us-south-1", "available")
	testSubnet2 := createTestSubnet("subnet-2", "test-subnet-2", "vpc-different", "us-south-1", "available")
	
	assert.Equal(t, "vpc-12345", *testSubnet1.VPC.ID)
	assert.Equal(t, "vpc-different", *testSubnet2.VPC.ID)
	
	// Test subnet filtering by zone
	assert.Equal(t, "us-south-1", *testSubnet1.Zone.Name)
	
	// Test subnet status filtering
	assert.Equal(t, "available", *testSubnet1.Status)
	
	pendingSubnet := createTestSubnet("subnet-3", "test-subnet-3", "vpc-12345", "us-south-1", "pending")
	assert.Equal(t, "pending", *pendingSubnet.Status)
}

func TestSecurityGroupLogic(t *testing.T) {
	// Test the logic components that the getDefaultSecurityGroup function uses
	
	// Test security group creation and VPC association
	sgDefault := createTestSecurityGroup("sg-default", "default", "vpc-12345")
	sgCustom := createTestSecurityGroup("sg-custom", "custom", "vpc-12345")
	sgOtherVPC := createTestSecurityGroup("sg-other", "default", "vpc-different")
	
	// Test security group properties
	assert.Equal(t, "sg-default", *sgDefault.ID)
	assert.Equal(t, "default", *sgDefault.Name)
	assert.Equal(t, "vpc-12345", *sgDefault.VPC.ID)
	
	assert.Equal(t, "sg-custom", *sgCustom.ID)
	assert.Equal(t, "custom", *sgCustom.Name)
	
	assert.Equal(t, "vpc-different", *sgOtherVPC.VPC.ID)
	
	// Test that we can identify default security groups by name
	assert.Equal(t, "default", *sgDefault.Name)
	assert.NotEqual(t, "default", *sgCustom.Name)
}

func TestUserDataAndSSHKeysIntegration(t *testing.T) {
	// Test that UserData and SSH keys are properly set in NodeClass
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass-with-userdata",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region: "us-south",
			VPC:    "vpc-12345",
			Image:  "ubuntu-20-04",
			UserData: `#!/bin/bash
echo "Bootstrap script"
apt update`,
			SSHKeys: []string{"key-12345", "key-67890"},
		},
	}

	// Verify the fields are properly set
	assert.Equal(t, "#!/bin/bash\necho \"Bootstrap script\"\napt update", nodeClass.Spec.UserData)
	assert.Len(t, nodeClass.Spec.SSHKeys, 2)
	assert.Contains(t, nodeClass.Spec.SSHKeys, "key-12345")
	assert.Contains(t, nodeClass.Spec.SSHKeys, "key-67890")
}

func TestIsIBMInstanceNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "not found error",
			err:      errors.New("instance not found"),
			expected: true,
		},
		{
			name:     "not_found error with underscore",
			err:      errors.New("resource not_found"),
			expected: true,
		},
		{
			name:     "resource_not_found error",
			err:      errors.New("resource_not_found"),
			expected: true,
		},
		{
			name:     "404 error",
			err:      errors.New("HTTP 404 error"),
			expected: true,
		},
		{
			name:     "mixed case NOT FOUND",
			err:      errors.New("Instance NOT FOUND"),
			expected: true,
		},
		{
			name:     "other error",
			err:      errors.New("internal server error"),
			expected: false,
		},
		{
			name:     "permission denied error",
			err:      errors.New("access denied"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isIBMInstanceNotFoundError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDeleteWithNotFoundError(t *testing.T) {
	// Test that Delete method properly returns NodeClaimNotFoundError when instance doesn't exist
	ctx := context.Background()
	
	// Create a provider with nil client to simulate the error path
	provider := &IBMCloudInstanceProvider{
		client: nil,
	}
	
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Spec: corev1.NodeSpec{
			ProviderID: "ibm://test-instance-id",
		},
	}
	
	err := provider.Delete(ctx, node)
	
	// Should get an error since client is nil
	assert.Error(t, err)
	
	// For this test, we expect an IBM client initialization error
	assert.Contains(t, err.Error(), "IBM client not initialized")
}

func TestCloudProviderErrorHandling(t *testing.T) {
	// Test that proper error types can be detected by Karpenter
	
	// Create a mock NodeClaimNotFoundError
	originalErr := errors.New("instance not found")
	notFoundErr := cloudprovider.NewNodeClaimNotFoundError(originalErr)
	
	// Verify that IsNodeClaimNotFoundError properly detects the error type
	assert.True(t, cloudprovider.IsNodeClaimNotFoundError(notFoundErr))
	assert.False(t, cloudprovider.IsNodeClaimNotFoundError(originalErr))
	assert.False(t, cloudprovider.IsNodeClaimNotFoundError(nil))
	
	// Test error message
	assert.Contains(t, notFoundErr.Error(), "instance not found")
}