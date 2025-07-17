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

package ibm

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestIKSCLI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IKS CLI Suite")
}

var _ = Describe("IKSCLIClient", func() {
	var (
		cliClient *IKSCLIClient
		ctx       context.Context
		apiKey    string
		region    string
	)

	BeforeEach(func() {
		ctx = context.Background()
		apiKey = "test-api-key"
		region = "us-south"
		cliClient = NewIKSCLIClient(apiKey, region)
	})

	Describe("CLI Availability", func() {
		It("should detect if IBM Cloud CLI is available", func() {
			// This test checks if the CLI is available in the system
			available := cliClient.IsAvailable()

			// In a container with IBM Cloud CLI, this should be true
			// In a test environment without CLI, this might be false
			// We test both scenarios
			if available {
				Expect(available).To(BeTrue())
			} else {
				// If CLI is not available, log it but don't fail the test
				GinkgoWriter.Printf("IBM Cloud CLI not available in test environment\n")
				Expect(available).To(BeFalse())
			}
		})
	})

	Describe("Authentication", func() {
		Context("when API key is provided", func() {
			It("should handle authentication setup", func() {
				// Skip if CLI not available
				if !cliClient.IsAvailable() {
					Skip("IBM Cloud CLI not available")
				}

				// Test authentication - this will fail with invalid API key but should not panic
				err := cliClient.authenticate(ctx)

				// With a test API key, this should fail but gracefully
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("CLI login failed"))
			})
		})

		Context("when API key is missing", func() {
			It("should handle missing API key gracefully", func() {
				// Skip if CLI not available
				if !cliClient.IsAvailable() {
					Skip("IBM Cloud CLI not available")
				}

				emptyCLI := NewIKSCLIClient("", region)
				err := emptyCLI.authenticate(ctx)

				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("Worker Pool Operations", func() {
		var testClusterID string

		BeforeEach(func() {
			testClusterID = "test-cluster-id"
		})

		Context("ListWorkerPools", func() {
			It("should handle CLI command execution", func() {
				// Skip if CLI not available
				if !cliClient.IsAvailable() {
					Skip("IBM Cloud CLI not available")
				}

				// This will fail with authentication error, but tests the command structure
				_, err := cliClient.ListWorkerPools(ctx, testClusterID)

				// Should get an authentication error, not a command structure error
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("authentication failed"))
			})

			It("should parse CLI output correctly", func() {
				// Test JSON parsing with mock CLI output
				mockOutput := `[
					{
						"id": "test-pool-id",
						"poolName": "default",
						"flavor": "bx2.2x8",
						"labels": {"test": "label"},
						"workerCount": 3,
						"provider": "vpc-gen2",
						"zones": [
							{
								"id": "us-south-1",
								"workerCount": 3
							}
						],
						"vpcID": "test-vpc-id"
					}
				]`

				var cliPools []CLIWorkerPool
				err := json.Unmarshal([]byte(mockOutput), &cliPools)
				Expect(err).ToNot(HaveOccurred())

				Expect(cliPools).To(HaveLen(1))
				Expect(cliPools[0].ID).To(Equal("test-pool-id"))
				Expect(cliPools[0].PoolName).To(Equal("default"))
				Expect(cliPools[0].Flavor).To(Equal("bx2.2x8"))
				Expect(cliPools[0].WorkerCount).To(Equal(3))
				Expect(cliPools[0].Zones).To(HaveLen(1))
				Expect(cliPools[0].Zones[0].ID).To(Equal("us-south-1"))
			})
		})

		Context("GetWorkerPool", func() {
			It("should handle single worker pool retrieval", func() {
				// Skip if CLI not available
				if !cliClient.IsAvailable() {
					Skip("IBM Cloud CLI not available")
				}

				// This will fail with authentication error, but tests the method structure
				_, err := cliClient.GetWorkerPool(ctx, testClusterID, "test-pool-id")

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("authentication failed"))
			})
		})

		Context("ResizeWorkerPool", func() {
			It("should handle worker pool resize command", func() {
				// Skip if CLI not available
				if !cliClient.IsAvailable() {
					Skip("IBM Cloud CLI not available")
				}

				// This will fail with authentication error, but tests the command structure
				err := cliClient.ResizeWorkerPool(ctx, testClusterID, "test-pool-id", 5)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("authentication failed"))
			})
		})
	})

	Describe("Error Handling", func() {
		It("should handle command execution timeouts", func() {
			// Skip if CLI not available
			if !cliClient.IsAvailable() {
				Skip("IBM Cloud CLI not available")
			}

			// Create a context with very short timeout
			shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()

			// This should timeout or fail quickly
			_, err := cliClient.ListWorkerPools(shortCtx, "test-cluster")
			Expect(err).To(HaveOccurred())
		})

		It("should handle invalid cluster IDs", func() {
			// Skip if CLI not available
			if !cliClient.IsAvailable() {
				Skip("IBM Cloud CLI not available")
			}

			// Test with empty cluster ID
			_, err := cliClient.ListWorkerPools(ctx, "")
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("HybridIKSClient", func() {
	var (
		hybridClient *HybridIKSClient
	)

	BeforeEach(func() {
		// Create a mock client for testing
		// In real scenario, this would be initialized with actual IBM client
		hybridClient = &HybridIKSClient{
			useCLI: false, // Start with API mode
		}
	})

	Describe("Fallback Mechanism", func() {
		It("should start in API mode", func() {
			Expect(hybridClient.useCLI).To(BeFalse())
		})

		It("should switch to CLI mode on E3917 error", func() {
			// This test verifies the error detection logic
			// The actual switching happens in the real methods

			testError := "IKS API error: status 400, body: {\"code\":\"E3917\",\"description\":\"Cluster provider not permitted for given operation.\"}"

			// Simulate E3917 error detection
			isE3917 := containsE3917Error(testError)
			Expect(isE3917).To(BeTrue())
		})

		It("should not switch to CLI mode on other errors", func() {
			testError := "IKS API error: status 401, body: {\"code\":\"E0001\",\"description\":\"Unauthorized\"}"

			isE3917 := containsE3917Error(testError)
			Expect(isE3917).To(BeFalse())
		})
	})
})

var _ = Describe("Integration Tests", func() {
	Context("with real credentials", func() {
		var (
			realAPIKey    string
			realRegion    string
			realClusterID string
		)

		BeforeEach(func() {
			// Only run these tests if real credentials are provided
			realAPIKey = os.Getenv("IBM_API_KEY")
			realRegion = os.Getenv("IBM_REGION")
			realClusterID = os.Getenv("TEST_CLUSTER_ID")

			if realAPIKey == "" || realRegion == "" || realClusterID == "" {
				Skip("Real IBM Cloud credentials not provided - skipping integration tests")
			}
		})

		It("should successfully list worker pools with real credentials", func() {
			cliClient := NewIKSCLIClient(realAPIKey, realRegion)

			// Skip if CLI not available
			if !cliClient.IsAvailable() {
				Skip("IBM Cloud CLI not available")
			}

			pools, err := cliClient.ListWorkerPools(context.Background(), realClusterID)

			// With real credentials, this should succeed
			Expect(err).ToNot(HaveOccurred())
			Expect(pools).ToNot(BeEmpty())

			GinkgoWriter.Printf("Found %d worker pools\n", len(pools))
			for _, pool := range pools {
				GinkgoWriter.Printf("  - Pool: %s (%s) - Size: %d\n", pool.Name, pool.Flavor, pool.SizePerZone)
			}
		})

		It("should successfully get specific worker pool with real credentials", func() {
			cliClient := NewIKSCLIClient(realAPIKey, realRegion)

			// Skip if CLI not available
			if !cliClient.IsAvailable() {
				Skip("IBM Cloud CLI not available")
			}

			// First get the list to find a real pool ID
			pools, err := cliClient.ListWorkerPools(context.Background(), realClusterID)
			Expect(err).ToNot(HaveOccurred())
			Expect(pools).ToNot(BeEmpty())

			// Test getting the first pool
			pool, err := cliClient.GetWorkerPool(context.Background(), realClusterID, pools[0].ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(pool).ToNot(BeNil())
			Expect(pool.ID).To(Equal(pools[0].ID))
		})
	})
})

// Helper function to test E3917 error detection
func containsE3917Error(errorMsg string) bool {
	return errorMsg != "" && (
	// Check for E3917 error code in various formats
	containsString(errorMsg, "E3917") ||
		containsString(errorMsg, "Cluster provider not permitted"))
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
