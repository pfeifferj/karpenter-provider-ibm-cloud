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
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"
)

var (
	// Cache available instance types to avoid repeated API calls
	cachedInstanceTypes []string
	defaultTestInstance = "bx2-2x8" // Fallback if detection fails
)

// GetAvailableInstanceType returns a suitable instance type for testing
// It queries the VPC API to find available instance types in the current region
func (s *E2ETestSuite) GetAvailableInstanceType(t *testing.T) string {
	// Return cached value if available
	if len(cachedInstanceTypes) > 0 {
		return cachedInstanceTypes[0]
	}

	// Try to get instance types from VPC API
	instanceTypes, err := s.getInstanceTypesFromVPC()
	if err != nil {
		t.Logf("Warning: Failed to get instance types from VPC API: %v", err)
		t.Logf("Using default instance type: %s", defaultTestInstance)
		return defaultTestInstance
	}

	if len(instanceTypes) == 0 {
		t.Logf("Warning: No instance types found, using default: %s", defaultTestInstance)
		return defaultTestInstance
	}

	// Cache the results
	cachedInstanceTypes = instanceTypes

	// Prefer smaller instance types for testing (lower cost)
	for _, profile := range instanceTypes {
		// Look for small instances (2 vCPU types)
		if strings.Contains(profile, "2x8") || strings.Contains(profile, "2x10") {
			t.Logf("Selected instance type for testing: %s", profile)
			return profile
		}
	}

	// If no small instances found, use the first available
	t.Logf("Selected instance type for testing: %s", instanceTypes[0])
	return instanceTypes[0]
}

// GetMultipleInstanceTypes returns multiple instance types for testing NodePool with multiple types
func (s *E2ETestSuite) GetMultipleInstanceTypes(t *testing.T, count int) []string {
	if count <= 0 {
		count = 3
	}

	// Get all available types
	allTypes, err := s.getInstanceTypesFromVPC()
	if err != nil {
		t.Logf("Warning: Failed to get instance types: %v", err)
		// Return some known working types as fallback
		return []string{"bx2-2x8", "bx2d-2x8", "bx3d-2x10"}
	}

	// Filter for suitable test instances (2-4 vCPU range)
	var testTypes []string
	for _, profile := range allTypes {
		if strings.Contains(profile, "2x") || strings.Contains(profile, "4x") {
			testTypes = append(testTypes, profile)
			if len(testTypes) >= count {
				break
			}
		}
	}

	// If we didn't find enough, add from all types
	if len(testTypes) < count {
		for _, profile := range allTypes {
			found := false
			for _, existing := range testTypes {
				if existing == profile {
					found = true
					break
				}
			}
			if !found {
				testTypes = append(testTypes, profile)
				if len(testTypes) >= count {
					break
				}
			}
		}
	}

	if len(testTypes) == 0 {
		// Last resort fallback
		testTypes = []string{"bx2-2x8", "bx2d-2x8", "bx3d-2x10"}
	}

	t.Logf("Selected instance types for testing: %v", testTypes)
	return testTypes
}

// getInstanceTypesFromVPC queries the VPC API for available instance profiles
func (s *E2ETestSuite) getInstanceTypesFromVPC() ([]string, error) {
	// Get VPC client from IBM client
	vpcClient, err := s.createVPCClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create VPC client: %w", err)
	}

	// List instance profiles
	listOptions := &vpcv1.ListInstanceProfilesOptions{}
	profiles, _, err := vpcClient.ListInstanceProfiles(listOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to list instance profiles: %w", err)
	}

	var instanceTypes []string
	for _, profile := range profiles.Profiles {
		if profile.Name != nil {
			instanceTypes = append(instanceTypes, *profile.Name)
		}
	}

	return instanceTypes, nil
}

// createVPCClient creates a VPC client for API calls
func (s *E2ETestSuite) createVPCClient() (*vpcv1.VpcV1, error) {
	apiKey := os.Getenv("IBMCLOUD_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("IBMCLOUD_API_KEY not set")
	}

	vpcURL := os.Getenv("VPC_URL")
	if vpcURL == "" {
		// Default to the region-specific URL
		region := os.Getenv("IBMCLOUD_REGION")
		if region == "" {
			return nil, fmt.Errorf("IBMCLOUD_REGION not set")
		}
		vpcURL = fmt.Sprintf("https://%s.iaas.cloud.ibm.com/v1", region)
	}

	authenticator := &core.IamAuthenticator{
		ApiKey: apiKey,
	}

	vpcClient, err := vpcv1.NewVpcV1(&vpcv1.VpcV1Options{
		Authenticator: authenticator,
		URL:           vpcURL,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create VPC client: %w", err)
	}

	return vpcClient, nil
}

// ValidateInstanceTypeAvailability checks if a specific instance type is available
func (s *E2ETestSuite) ValidateInstanceTypeAvailability(t *testing.T, instanceType string) bool {
	types, err := s.getInstanceTypesFromVPC()
	if err != nil {
		t.Logf("Warning: Could not validate instance type availability: %v", err)
		return false
	}

	for _, available := range types {
		if available == instanceType {
			return true
		}
	}

	return false
}

// GetSmallestInstanceType returns the smallest available instance type for cost-effective testing
func (s *E2ETestSuite) GetSmallestInstanceType(t *testing.T) string {
	types, err := s.getInstanceTypesFromVPC()
	if err != nil {
		t.Logf("Warning: Failed to get instance types: %v", err)
		return defaultTestInstance
	}

	// Priority order for small test instances
	priorities := []string{
		"bx2-2x8",   // Balanced, 2 vCPU, 8 GB RAM
		"bx2d-2x8",  // Balanced with NVMe
		"bx3d-2x10", // Next gen balanced
		"cx2-2x4",   // Compute optimized, smaller
		"mx2-2x16",  // Memory optimized
	}

	// Check priority list first
	for _, priority := range priorities {
		for _, available := range types {
			if available == priority {
				t.Logf("Selected smallest instance type: %s", priority)
				return priority
			}
		}
	}

	// If none from priority list, find any 2x type
	for _, profile := range types {
		if strings.Contains(profile, "2x") {
			t.Logf("Selected smallest instance type: %s", profile)
			return profile
		}
	}

	// Fallback to first available
	if len(types) > 0 {
		t.Logf("Selected instance type: %s", types[0])
		return types[0]
	}

	t.Logf("Using default instance type: %s", defaultTestInstance)
	return defaultTestInstance
}
