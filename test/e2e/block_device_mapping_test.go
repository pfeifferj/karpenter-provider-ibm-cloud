//go:build e2e
// +build e2e

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

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

// TestE2EBlockDeviceMapping tests block device mapping functionality end-to-end
func TestE2EBlockDeviceMapping(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	testName := fmt.Sprintf("e2e-block-device-%d", time.Now().Unix())
	t.Logf("Starting E2E Block Device Mapping test: %s", testName)

	// Test cases for different block device configurations
	testCases := []struct {
		name                string
		blockDeviceMappings []v1alpha1.BlockDeviceMapping
		expectedBootSize    int64
		expectedDataVolumes int
	}{
		{
			name: "custom-boot-volume",
			blockDeviceMappings: []v1alpha1.BlockDeviceMapping{
				{
					RootVolume: true,
					VolumeSpec: &v1alpha1.VolumeSpec{
						Capacity: &[]int64{150}[0], // 150GB boot volume
						Profile:  &[]string{"10iops-tier"}[0],
						DeleteOnTermination: &[]bool{true}[0],
					},
				},
			},
			expectedBootSize:    150,
			expectedDataVolumes: 0,
		},
		{
			name: "boot-and-data-volumes",
			blockDeviceMappings: []v1alpha1.BlockDeviceMapping{
				{
					RootVolume: true,
					VolumeSpec: &v1alpha1.VolumeSpec{
						Capacity: &[]int64{100}[0], // 100GB boot
						Profile:  &[]string{"general-purpose"}[0],
					},
				},
				{
					DeviceName: &[]string{"data-storage"}[0],
					VolumeSpec: &v1alpha1.VolumeSpec{
						Capacity: &[]int64{200}[0], // 200GB data volume
						Profile:  &[]string{"5iops-tier"}[0],
						DeleteOnTermination: &[]bool{false}[0], // Preserve data volume
					},
				},
			},
			expectedBootSize:    100,
			expectedDataVolumes: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			subTestName := fmt.Sprintf("%s-%s", testName, tc.name)
			
			// Step 1: Create NodeClass with block device mapping
			nodeClass := suite.createTestNodeClassWithBlockDeviceMapping(t, subTestName, tc.blockDeviceMappings)
			t.Logf("Created NodeClass with block device mapping: %s", nodeClass.Name)

			// Step 2: Wait for NodeClass to be ready
			suite.waitForNodeClassReady(t, nodeClass.Name)
			t.Logf("NodeClass is ready: %s", nodeClass.Name)

			// Step 3: Create NodePool
			nodePool := suite.createTestNodePool(t, subTestName, nodeClass.Name)
			t.Logf("Created NodePool: %s", nodePool.Name)

			// Step 4: Deploy test workload to trigger Karpenter provisioning
			deployment := suite.createTestWorkload(t, subTestName)
			t.Logf("Created test workload: %s", deployment.Name)

			// Step 5: Wait for node to be provisioned
			node := suite.waitForNodeProvisioned(t, nodePool.Name)
			t.Logf("Node provisioned: %s", node.Name)

			// Step 6: Verify block device configuration on the provisioned node
			suite.verifyBlockDeviceConfiguration(t, node, tc.expectedBootSize, tc.expectedDataVolumes)

			// Step 7: Verify workload is scheduled on the new node
			suite.verifyWorkloadScheduled(t, deployment.Name, node.Name)
			t.Logf("Workload successfully scheduled on node with custom block devices")

			// Step 8: Clean up resources
			suite.cleanupTestResources(t, subTestName)
			t.Logf("Cleaned up resources for test: %s", tc.name)
		})
	}
}

// createTestNodeClassWithBlockDeviceMapping creates a NodeClass with specified block device mappings
func (s *E2ETestSuite) createTestNodeClassWithBlockDeviceMapping(t *testing.T, testName string, blockDeviceMappings []v1alpha1.BlockDeviceMapping) *v1alpha1.IBMNodeClass {
	// Dynamically get an available instance type
	instanceType := s.GetAvailableInstanceType(t)
	bootstrapMode := "cloud-init"

	nodeClass := &v1alpha1.IBMNodeClass{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "karpenter.ibm.sh/v1alpha1",
			Kind:       "IBMNodeClass",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-nodeclass", testName),
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:              s.testRegion,
			Zone:                s.testZone,
			InstanceProfile:     instanceType,
			Image:               s.testImage,
			VPC:                 s.testVPC,
			Subnet:              s.testSubnet,
			SecurityGroups:      []string{s.testSecurityGroup},
			APIServerEndpoint:   s.APIServerEndpoint,
			BootstrapMode:       &bootstrapMode,
			ResourceGroup:       s.testResourceGroup,
			SSHKeys:             []string{s.testSshKeyId},
			BlockDeviceMappings: blockDeviceMappings,
			Tags: map[string]string{
				"test":       "e2e-block-device",
				"test-name":  testName,
				"created-by": "karpenter-e2e",
				"purpose":    "block-device-testing",
			},
		},
	}

	err := s.kubeClient.Create(context.Background(), nodeClass)
	require.NoError(t, err)

	return nodeClass
}

// verifyBlockDeviceConfiguration verifies that the node has the expected block device configuration
func (s *E2ETestSuite) verifyBlockDeviceConfiguration(t *testing.T, node *corev1.Node, expectedBootSize int64, expectedDataVolumes int) {
	t.Logf("Verifying block device configuration on node: %s", node.Name)

	// Get the instance ID from the node
	providerID := node.Spec.ProviderID
	require.NotEmpty(t, providerID, "Node should have provider ID")
	
	instanceID := extractInstanceIDFromProviderID(providerID)
	require.NotEmpty(t, instanceID, "Should be able to extract instance ID from provider ID")

	// Connect to IBM Cloud VPC API to verify volume configuration
	vpcClient, err := s.vpcClientManager.GetVPCClient(context.Background())
	require.NoError(t, err)

	// Get instance details
	instance, err := vpcClient.GetInstance(context.Background(), instanceID)
	require.NoError(t, err)
	require.NotNil(t, instance, "Instance should exist")

	// Verify boot volume
	require.NotNil(t, instance.BootVolumeAttachment, "Instance should have boot volume attachment")
	bootVolume := instance.BootVolumeAttachment.Volume
	require.NotNil(t, bootVolume, "Boot volume should exist")

	// Check boot volume capacity
	t.Logf("Boot volume capacity: %d GB (expected: %d GB)", *bootVolume.Capacity, expectedBootSize)
	require.Equal(t, expectedBootSize, *bootVolume.Capacity, "Boot volume should have expected capacity")

	// Verify additional data volumes
	actualDataVolumes := 0
	if instance.VolumeAttachments != nil {
		actualDataVolumes = len(instance.VolumeAttachments)
	}
	
	t.Logf("Data volumes count: %d (expected: %d)", actualDataVolumes, expectedDataVolumes)
	require.Equal(t, expectedDataVolumes, actualDataVolumes, "Should have expected number of data volumes")

	// If there are data volumes, verify their configuration
	if expectedDataVolumes > 0 && instance.VolumeAttachments != nil {
		for i, volumeAttachment := range instance.VolumeAttachments {
			volume := volumeAttachment.Volume
			require.NotNil(t, volume, "Data volume %d should exist", i)
			
			t.Logf("Data volume %d: capacity=%d GB, profile=%s", 
				i, *volume.Capacity, *volume.Profile.Name)
		}
	}

	t.Logf("✅ Block device configuration verified successfully")
}

// extractInstanceIDFromProviderID extracts the instance ID from a Kubernetes provider ID
func extractInstanceIDFromProviderID(providerID string) string {
	// Provider ID format: ibm://<region>/<instance-id>
	// Example: ibm://us-south/0717-12345678-1234-5678-9abc-def012345678
	if len(providerID) > 6 && providerID[:6] == "ibm://" {
		parts := strings.Split(providerID[6:], "/")
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	return ""
}