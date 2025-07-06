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
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// CLIWorkerPool represents worker pool data from CLI output
type CLIWorkerPool struct {
	ID          string            `json:"id"`
	PoolName    string            `json:"poolName"`
	Flavor      string            `json:"flavor"`
	Labels      map[string]string `json:"labels"`
	WorkerCount int               `json:"workerCount"`
	Provider    string            `json:"provider"`
	Zones       []CLIZone         `json:"zones"`
	VPCID       string            `json:"vpcID"`
}

// CLIZone represents zone data from CLI output
type CLIZone struct {
	ID          string `json:"id"`
	WorkerCount int    `json:"workerCount"`
}

// IKSCLIClient handles IBM Cloud CLI operations for IKS
type IKSCLIClient struct {
	apiKey string
	region string
}

// NewIKSCLIClient creates a new CLI-based IKS client
func NewIKSCLIClient(apiKey, region string) *IKSCLIClient {
	return &IKSCLIClient{
		apiKey: apiKey,
		region: region,
	}
}

// IsAvailable checks if IBM Cloud CLI is available
func (c *IKSCLIClient) IsAvailable() bool {
	_, err := exec.LookPath("ibmcloud")
	return err == nil
}

// authenticate sets up CLI authentication
func (c *IKSCLIClient) authenticate(ctx context.Context) error {
	// Set API key environment variable for CLI
	env := append(os.Environ(), fmt.Sprintf("IBMCLOUD_API_KEY=%s", c.apiKey))
	
	// Login to IBM Cloud
	cmd := exec.CommandContext(ctx, "ibmcloud", "login", "--apikey", c.apiKey, "-r", c.region, "-q")
	cmd.Env = env
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("CLI login failed: %w, output: %s", err, string(output))
	}
	
	// Target container service plugin
	cmd = exec.CommandContext(ctx, "ibmcloud", "plugin", "list")
	cmd.Env = env
	
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("checking plugins failed: %w, output: %s", err, string(output))
	}
	
	// Check if kubernetes-service plugin is installed
	if !strings.Contains(string(output), "kubernetes-service") {
		return fmt.Errorf("kubernetes-service plugin not installed, please run: ibmcloud plugin install kubernetes-service")
	}
	
	return nil
}

// ListWorkerPools lists worker pools using CLI
func (c *IKSCLIClient) ListWorkerPools(ctx context.Context, clusterID string) ([]*WorkerPool, error) {
	if err := c.authenticate(ctx); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}
	
	env := append(os.Environ(), fmt.Sprintf("IBMCLOUD_API_KEY=%s", c.apiKey))
	
	// Execute CLI command
	cmd := exec.CommandContext(ctx, "ibmcloud", "ks", "worker-pools", "-c", clusterID, "--output", "json")
	cmd.Env = env
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("CLI command failed: %w, output: %s", err, string(output))
	}
	
	// Parse CLI output
	var cliPools []CLIWorkerPool
	if err := json.Unmarshal(output, &cliPools); err != nil {
		return nil, fmt.Errorf("parsing CLI output: %w", err)
	}
	
	// Convert to standard WorkerPool format
	var workerPools []*WorkerPool
	for _, cliPool := range cliPools {
		// Calculate total size across zones
		totalSize := 0
		zone := ""
		for _, z := range cliPool.Zones {
			totalSize += z.WorkerCount
			if zone == "" {
				zone = z.ID
			}
		}
		
		pool := &WorkerPool{
			ID:          cliPool.ID,
			Name:        cliPool.PoolName,
			Flavor:      cliPool.Flavor,
			Zone:        zone,
			SizePerZone: totalSize / len(cliPool.Zones), // Average per zone
			ActualSize:  totalSize,
			State:       "active", // CLI doesn't provide state, assume active
			Labels:      cliPool.Labels,
			CreatedAt:   time.Now(), // CLI doesn't provide creation time
			UpdatedAt:   time.Now(),
		}
		workerPools = append(workerPools, pool)
	}
	
	return workerPools, nil
}

// GetWorkerPool gets a specific worker pool using CLI
func (c *IKSCLIClient) GetWorkerPool(ctx context.Context, clusterID, poolID string) (*WorkerPool, error) {
	pools, err := c.ListWorkerPools(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	
	for _, pool := range pools {
		if pool.ID == poolID {
			return pool, nil
		}
	}
	
	return nil, fmt.Errorf("worker pool %s not found", poolID)
}

// ResizeWorkerPool resizes a worker pool using CLI
func (c *IKSCLIClient) ResizeWorkerPool(ctx context.Context, clusterID, poolID string, newSize int) error {
	if err := c.authenticate(ctx); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	
	env := append(os.Environ(), fmt.Sprintf("IBMCLOUD_API_KEY=%s", c.apiKey))
	
	// Execute CLI resize command
	cmd := exec.CommandContext(ctx, "ibmcloud", "ks", "worker-pool", "resize", 
		"-c", clusterID, 
		"--worker-pool", poolID, 
		"--size-per-zone", strconv.Itoa(newSize))
	cmd.Env = env
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("CLI resize failed: %w, output: %s", err, string(output))
	}
	
	return nil
}

// HybridIKSClient combines API and CLI approaches with fallback
type HybridIKSClient struct {
	apiClient *IKSClient
	cliClient *IKSCLIClient
	useCLI    bool
}

// NewHybridIKSClient creates a hybrid client that tries API first, falls back to CLI
func NewHybridIKSClient(client *Client) *HybridIKSClient {
	var ibmAPIKey string
	if client.credStore != nil {
		ctx := context.Background()
		ibmAPIKey, _ = client.credStore.GetIBMAPIKey(ctx)
	}
	
	return &HybridIKSClient{
		apiClient: NewIKSClient(client),
		cliClient: NewIKSCLIClient(ibmAPIKey, client.region),
		useCLI:    false, // Start with API, fallback to CLI on E3917 errors
	}
}

// ListWorkerPools tries API first, falls back to CLI on E3917 errors
func (h *HybridIKSClient) ListWorkerPools(ctx context.Context, clusterID string) ([]*WorkerPool, error) {
	if h.useCLI {
		return h.cliClient.ListWorkerPools(ctx, clusterID)
	}
	
	// Try API first
	pools, err := h.apiClient.ListWorkerPools(ctx, clusterID)
	if err != nil {
		// Check for E3917 error
		if strings.Contains(err.Error(), "E3917") {
			// Switch to CLI mode permanently for this client
			h.useCLI = true
			
			// Check if CLI is available
			if !h.cliClient.IsAvailable() {
				return nil, fmt.Errorf("API failed with E3917 and CLI not available: %w", err)
			}
			
			// Retry with CLI
			return h.cliClient.ListWorkerPools(ctx, clusterID)
		}
		return nil, err
	}
	
	return pools, nil
}

// GetWorkerPool tries API first, falls back to CLI on E3917 errors
func (h *HybridIKSClient) GetWorkerPool(ctx context.Context, clusterID, poolID string) (*WorkerPool, error) {
	if h.useCLI {
		return h.cliClient.GetWorkerPool(ctx, clusterID, poolID)
	}
	
	// Try API first
	pool, err := h.apiClient.GetWorkerPool(ctx, clusterID, poolID)
	if err != nil {
		// Check for E3917 error
		if strings.Contains(err.Error(), "E3917") {
			// Switch to CLI mode permanently for this client
			h.useCLI = true
			
			// Check if CLI is available
			if !h.cliClient.IsAvailable() {
				return nil, fmt.Errorf("API failed with E3917 and CLI not available: %w", err)
			}
			
			// Retry with CLI
			return h.cliClient.GetWorkerPool(ctx, clusterID, poolID)
		}
		return nil, err
	}
	
	return pool, nil
}

// ResizeWorkerPool tries API first, falls back to CLI on E3917 errors
func (h *HybridIKSClient) ResizeWorkerPool(ctx context.Context, clusterID, poolID string, newSize int) error {
	if h.useCLI {
		return h.cliClient.ResizeWorkerPool(ctx, clusterID, poolID, newSize)
	}
	
	// Try API first
	err := h.apiClient.ResizeWorkerPool(ctx, clusterID, poolID, newSize)
	if err != nil {
		// Check for E3917 error
		if strings.Contains(err.Error(), "E3917") {
			// Switch to CLI mode permanently for this client
			h.useCLI = true
			
			// Check if CLI is available
			if !h.cliClient.IsAvailable() {
				return fmt.Errorf("API failed with E3917 and CLI not available: %w", err)
			}
			
			// Retry with CLI
			return h.cliClient.ResizeWorkerPool(ctx, clusterID, poolID, newSize)
		}
		return err
	}
	
	return nil
}

// GetClusterConfig delegates to API client (CLI doesn't provide kubeconfig)
func (h *HybridIKSClient) GetClusterConfig(ctx context.Context, clusterID string) (string, error) {
	return h.apiClient.GetClusterConfig(ctx, clusterID)
}

// GetWorkerDetails delegates to API client (CLI doesn't provide worker details)
func (h *HybridIKSClient) GetWorkerDetails(ctx context.Context, clusterID, workerID string) (*IKSWorkerDetails, error) {
	return h.apiClient.GetWorkerDetails(ctx, clusterID, workerID)
}

// GetVPCInstanceIDFromWorker delegates to API client (CLI doesn't provide VPC instance ID)
func (h *HybridIKSClient) GetVPCInstanceIDFromWorker(ctx context.Context, clusterID, workerID string) (string, error) {
	return h.apiClient.GetVPCInstanceIDFromWorker(ctx, clusterID, workerID)
}