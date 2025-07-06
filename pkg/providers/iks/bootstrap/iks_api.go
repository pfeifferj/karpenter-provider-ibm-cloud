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

package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
	
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/types"
)

// IKSWorkerRequest represents a request to add a worker to an IKS cluster
type IKSWorkerRequest struct {
	ClusterID      string `json:"cluster_id"`
	WorkerPoolID   string `json:"worker_pool_id,omitempty"`
	VPCInstanceID  string `json:"vpc_instance_id"`
	Zone           string `json:"zone"`
	Labels         map[string]string `json:"labels,omitempty"`
	Taints         []IKSTaint `json:"taints,omitempty"`
}

// IKSTaint represents a Kubernetes taint for IKS workers
type IKSTaint struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

// IKSWorkerResponse represents the response from IKS worker creation
type IKSWorkerResponse struct {
	ID       string            `json:"id"`
	State    string            `json:"state"`
	Message  string            `json:"message"`
	Labels   map[string]string `json:"labels"`
	Location string            `json:"location"`
}


// AddWorkerToIKSCluster adds a VPC instance to an IKS cluster as a worker node
func (p *IKSBootstrapProvider) AddWorkerToIKSCluster(ctx context.Context, options types.IKSWorkerPoolOptions) (*IKSWorkerResponse, error) {
	logger := log.FromContext(ctx)
	
	// Get IAM token
	token, err := p.client.GetIAMClient().GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting IAM token: %w", err)
	}

	// Prepare request payload
	request := IKSWorkerRequest{
		ClusterID:     options.ClusterID,
		WorkerPoolID:  options.WorkerPoolID,
		VPCInstanceID: options.VPCInstanceID,
		Zone:          options.Zone,
		Labels: map[string]string{
			"karpenter.sh/managed":         "true",
			"karpenter.ibm.sh/zone":        options.Zone,
			"karpenter.ibm.sh/provisioner": "karpenter-ibm",
		},
	}

	payloadBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	// Make API request
	url := fmt.Sprintf("https://containers.cloud.ibm.com/global/v1/clusters/%s/workers", options.ClusterID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: failed to close response body: %v\n", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("IKS API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var response IKSWorkerResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	logger.Info("Successfully added worker to IKS cluster", "worker_id", response.ID, "state", response.State)
	return &response, nil
}