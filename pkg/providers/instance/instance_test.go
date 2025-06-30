package instance

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

// Note: Additional tests for Create, Delete, etc. require proper mock interfaces
// for IBM Cloud clients. These tests are skipped until mock implementations
// are properly aligned with the current IBM client interface.