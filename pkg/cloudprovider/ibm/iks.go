/*
Copyright 2024 The Kubernetes Authors.

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
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// IKSClient handles IBM Kubernetes Service API operations
type IKSClient struct {
	client     *Client
	baseURL    string
	httpClient *http.Client
}

// setIKSHeaders sets the required headers for IKS API requests
func (c *IKSClient) setIKSHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "IBM-Cloud-CLI/1.0.0")

	// Add regional targeting for global endpoint
	region := c.client.GetRegion()
	req.Header.Set("X-Region", region)

	// Add resource group context if available
	if resourceGroupID := os.Getenv("IBM_RESOURCE_GROUP_ID"); resourceGroupID != "" {
		req.Header.Set("X-Auth-Resource-Group", resourceGroupID)
	}

	// Add headers to mimic IBM Cloud CLI behavior
	req.Header.Set("X-CLI-Request", "true")
	req.Header.Set("X-Service-Name", "containers-kubernetes")
	req.Header.Set("X-Auth-User-Token", token)
}

// IKSWorkerDetails represents the response from IKS worker API
type IKSWorkerDetails struct {
	ID                string                `json:"id"`
	Provider          string                `json:"provider"`
	Flavor            string                `json:"flavor"`
	Location          string                `json:"location"`
	PoolID            string                `json:"poolID"`
	PoolName          string                `json:"poolName"`
	NetworkInterfaces []IKSNetworkInterface `json:"networkInterfaces"`
	Health            IKSHealthStatus       `json:"health"`
	Lifecycle         IKSLifecycleStatus    `json:"lifecycle"`
}

// IKSNetworkInterface represents a worker's network interface
type IKSNetworkInterface struct {
	SubnetID  string `json:"subnetID"`
	IPAddress string `json:"ipAddress"`
	CIDR      string `json:"cidr"`
	Primary   bool   `json:"primary"`
}

// IKSHealthStatus represents worker health information
type IKSHealthStatus struct {
	State   string `json:"state"`
	Message string `json:"message"`
}

// IKSLifecycleStatus represents worker lifecycle information
type IKSLifecycleStatus struct {
	DesiredState string `json:"desiredState"`
	ActualState  string `json:"actualState"`
	Message      string `json:"message"`
}

// NewIKSClient creates a new IKS API client
func NewIKSClient(client *Client) *IKSClient {
	// Use correct containers API endpoint matching IBM Cloud CLI
	// CLI uses v1 without 'global' prefix
	baseURL := "https://containers.cloud.ibm.com/v1"

	return &IKSClient{
		client:  client,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetWorkerDetails retrieves detailed information about an IKS worker
func (c *IKSClient) GetWorkerDetails(ctx context.Context, clusterID, workerID string) (*IKSWorkerDetails, error) {
	// Get IAM token for authentication
	token, err := c.client.iamClient.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting IAM token: %w", err)
	}

	// Construct API URL
	url := fmt.Sprintf("%s/clusters/%s/workers/%s", c.baseURL, clusterID, workerID)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Set headers
	c.setIKSHeaders(req, token)

	// Make request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// Check for API errors
	if resp.StatusCode != http.StatusOK {
		// Parse error response to get more details
		var errorResp struct {
			Code        string `json:"code"`
			Description string `json:"description"`
			Type        string `json:"type"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			// Handle specific error codes gracefully
			switch errorResp.Code {
			case "E3917": // Cluster provider not permitted for given operation
				return nil, fmt.Errorf("cluster %s is not configured for Karpenter management (IKS managed cluster): %s", clusterID, errorResp.Description)
			default:
				return nil, fmt.Errorf("IKS API error (code: %s): %s", errorResp.Code, errorResp.Description)
			}
		}
		return nil, fmt.Errorf("IKS API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var workerDetails IKSWorkerDetails
	if err := json.Unmarshal(body, &workerDetails); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &workerDetails, nil
}

// GetVPCInstanceIDFromWorker extracts the VPC instance ID from worker details
// For VPC Gen2 workers, we need to look up the VPC instance by subnet and IP
func (c *IKSClient) GetVPCInstanceIDFromWorker(ctx context.Context, clusterID, workerID string) (string, error) {
	// Get worker details
	workerDetails, err := c.GetWorkerDetails(ctx, clusterID, workerID)
	if err != nil {
		return "", fmt.Errorf("getting worker details: %w", err)
	}

	// Ensure this is a VPC Gen2 worker
	if workerDetails.Provider != "vpc-gen2" {
		return "", fmt.Errorf("worker %s is not a VPC Gen2 worker (provider: %s)", workerID, workerDetails.Provider)
	}

	// Find primary network interface
	var primaryInterface *IKSNetworkInterface
	for _, ni := range workerDetails.NetworkInterfaces {
		if ni.Primary {
			primaryInterface = &ni
			break
		}
	}

	if primaryInterface == nil {
		return "", fmt.Errorf("no primary network interface found for worker %s", workerID)
	}

	// Use VPC client to find instance by subnet and IP
	vpcClient, err := c.client.GetVPCClient()
	if err != nil {
		return "", fmt.Errorf("getting VPC client: %w", err)
	}

	// List instances and find the one with matching IP in the subnet
	instances, err := vpcClient.ListInstances(ctx)
	if err != nil {
		return "", fmt.Errorf("listing VPC instances: %w", err)
	}

	for _, instance := range instances {
		// Check if instance has matching IP address in primary interface
		for _, ni := range instance.NetworkInterfaces {
			if ni.Subnet != nil && ni.Subnet.ID != nil && *ni.Subnet.ID == primaryInterface.SubnetID {
				if ni.PrimaryIP != nil && ni.PrimaryIP.Address != nil && *ni.PrimaryIP.Address == primaryInterface.IPAddress {
					return *instance.ID, nil
				}
			}
		}
	}

	return "", fmt.Errorf("VPC instance not found for worker %s (IP: %s, subnet: %s)",
		workerID, primaryInterface.IPAddress, primaryInterface.SubnetID)
}

// GetClusterConfig retrieves the kubeconfig for the specified cluster
func (c *IKSClient) GetClusterConfig(ctx context.Context, clusterID string) (string, error) {
	if c.client == nil || c.client.iamClient == nil {
		return "", fmt.Errorf("client not properly initialized")
	}

	// Get IAM token for authentication
	token, err := c.client.iamClient.GetToken(ctx)
	if err != nil {
		return "", fmt.Errorf("getting IAM token: %w", err)
	}

	// Construct API URL for cluster config
	url := fmt.Sprintf("%s/clusters/%s/config", c.baseURL, clusterID)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	// Set headers
	c.setIKSHeaders(req, token)

	// Make request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("making request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	// Check for API errors
	if resp.StatusCode != http.StatusOK {
		// Parse error response to get more details
		var errorResp struct {
			Code        string `json:"code"`
			Description string `json:"description"`
			Type        string `json:"type"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return "", fmt.Errorf("IKS API error (code: %s): %s", errorResp.Code, errorResp.Description)
		}
		return "", fmt.Errorf("IKS API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse response - the API returns the kubeconfig as a string
	var configResp struct {
		Config string `json:"config"`
	}
	if err := json.Unmarshal(body, &configResp); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	return configResp.Config, nil
}

// WorkerPool represents an IKS worker pool
type WorkerPool struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Flavor      string            `json:"flavor"`
	Zone        string            `json:"zone"`
	SizePerZone int               `json:"sizePerZone"`
	ActualSize  int               `json:"actualSize"`
	State       string            `json:"state"`
	Labels      map[string]string `json:"labels"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

// WorkerPoolResizeRequest represents a request to resize a worker pool
type WorkerPoolResizeRequest struct {
	State       string `json:"state"`
	SizePerZone int    `json:"sizePerZone"`
}

// ListWorkerPools retrieves all worker pools for a cluster
func (c *IKSClient) ListWorkerPools(ctx context.Context, clusterID string) ([]*WorkerPool, error) {
	// Get IAM token for authentication
	token, err := c.client.iamClient.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting IAM token: %w", err)
	}

	// Construct API URL
	url := fmt.Sprintf("%s/clusters/%s/workerpools", c.baseURL, clusterID)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Set headers
	c.setIKSHeaders(req, token)

	// Make request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// Check for API errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("IKS API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var workerPools []*WorkerPool
	if err := json.Unmarshal(body, &workerPools); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return workerPools, nil
}

// ResizeWorkerPool resizes a worker pool by adding one node
func (c *IKSClient) ResizeWorkerPool(ctx context.Context, clusterID, poolID string, newSize int) error {
	// Get IAM token for authentication
	token, err := c.client.iamClient.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("getting IAM token: %w", err)
	}

	// Prepare request payload
	request := WorkerPoolResizeRequest{
		State:       "resizing",
		SizePerZone: newSize,
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	// Construct API URL
	url := fmt.Sprintf("%s/clusters/%s/workerpools/%s", c.baseURL, clusterID, poolID)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "PATCH", url, strings.NewReader(string(requestBody)))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	// Set headers
	c.setIKSHeaders(req, token)

	// Make request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("making request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	// Check for API errors
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("IKS worker pool resize failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetWorkerPool retrieves a specific worker pool
func (c *IKSClient) GetWorkerPool(ctx context.Context, clusterID, poolID string) (*WorkerPool, error) {
	// Get IAM token for authentication
	token, err := c.client.iamClient.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting IAM token: %w", err)
	}

	// Construct API URL
	url := fmt.Sprintf("%s/clusters/%s/workerpools/%s", c.baseURL, clusterID, poolID)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Set headers
	c.setIKSHeaders(req, token)

	// Make request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// Check for API errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("IKS API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var workerPool WorkerPool
	if err := json.Unmarshal(body, &workerPool); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &workerPool, nil
}
