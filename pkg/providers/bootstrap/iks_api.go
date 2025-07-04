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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
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

// generateIKSAPIScript generates a script that uses IKS API to add the node
func (p *IBMBootstrapProvider) generateIKSAPIScript(ctx context.Context, options Options, clusterInfo *ClusterInfo) (string, error) {
	logger := log.FromContext(ctx)
	
	clusterID := clusterInfo.IKSClusterID
	if clusterID == "" {
		clusterID = os.Getenv("IKS_CLUSTER_ID")
	}
	
	if clusterID == "" {
		return "", fmt.Errorf("IKS cluster ID not found")
	}

	logger.Info("Generating IKS API bootstrap script", "cluster_id", clusterID)

	// Create the IKS API script
	script := fmt.Sprintf(`#!/bin/bash
set -euo pipefail

echo "Starting IKS API node registration..."

# Configuration
CLUSTER_ID="%s"
ZONE="%s"
REGION="%s"
API_KEY="${IBM_API_KEY}"

# Get instance metadata
INSTANCE_ID=$(curl -s http://169.254.169.254/metadata/v1/instance/id)
if [ -z "$INSTANCE_ID" ]; then
    echo "Error: Could not retrieve instance ID from metadata service"
    exit 1
fi

echo "Instance ID: $INSTANCE_ID"
echo "Cluster ID: $CLUSTER_ID"
echo "Zone: $ZONE"

# Function to get IAM token
get_iam_token() {
    local response=$(curl -s -X POST \
        "https://iam.cloud.ibm.com/identity/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "grant_type=urn:urn:ibm:params:oauth:grant-type:apikey&apikey=$API_KEY")
    
    echo "$response" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4
}

# Function to add worker to IKS cluster
add_worker_to_cluster() {
    local token=$(get_iam_token)
    
    if [ -z "$token" ]; then
        echo "Error: Could not obtain IAM token"
        return 1
    fi
    
    # Prepare worker request payload
    local payload=$(cat <<EOF
{
    "cluster_id": "$CLUSTER_ID",
    "vpc_instance_id": "$INSTANCE_ID",
    "zone": "$ZONE",
    "labels": {
        "karpenter.sh/managed": "true",
        "karpenter.ibm.sh/zone": "$ZONE",
        "karpenter.ibm.sh/region": "$REGION"
    }
}
EOF
)

    echo "Adding worker to IKS cluster..."
    local response=$(curl -s -X POST \
        "https://containers.cloud.ibm.com/global/v1/clusters/$CLUSTER_ID/workers" \
        -H "Authorization: Bearer $token" \
        -H "Content-Type: application/json" \
        -d "$payload")
    
    echo "IKS API Response: $response"
    
    # Check if request was successful
    local worker_id=$(echo "$response" | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
    if [ -n "$worker_id" ]; then
        echo "Successfully added worker to cluster: $worker_id"
        return 0
    else
        echo "Error: Failed to add worker to cluster"
        echo "Response: $response"
        return 1
    fi
}

# Function to wait for worker to be ready
wait_for_worker_ready() {
    local token=$(get_iam_token)
    
    echo "Waiting for worker to be ready..."
    local max_attempts=60
    local attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        local workers_response=$(curl -s \
            "https://containers.cloud.ibm.com/global/v1/clusters/$CLUSTER_ID/workers" \
            -H "Authorization: Bearer $token")
        
        # Find our worker by instance ID
        local worker_state=$(echo "$workers_response" | jq -r ".[] | select(.id | contains(\"$INSTANCE_ID\")) | .health.state")
        
        case "$worker_state" in
            "normal")
                echo "Worker is ready!"
                return 0
                ;;
            "warning"|"critical")
                echo "Worker is in $worker_state state, continuing to wait..."
                ;;
            "")
                echo "Worker not found yet, continuing to wait..."
                ;;
            *)
                echo "Worker is in $worker_state state, continuing to wait..."
                ;;
        esac
        
        attempt=$((attempt + 1))
        echo "Attempt $attempt/$max_attempts: Worker state is $worker_state"
        sleep 30
    done
    
    echo "Warning: Worker did not become ready within expected time"
    return 1
}

# Function to install required packages
install_prerequisites() {
    echo "Installing prerequisites..."
    apt-get update
    apt-get install -y curl jq
}

# Function to setup logging
setup_logging() {
    # Create log directory
    mkdir -p /var/log/karpenter
    
    # Setup log rotation
    cat <<EOF > /etc/logrotate.d/karpenter
/var/log/karpenter/*.log {
    daily
    rotate 7
    compress
    delaycompress
    missingok
    notifempty
    create 644 root root
}
EOF
}

# Function to register with health monitoring
register_health_monitoring() {
    echo "Setting up health monitoring..."
    
    # Create health check script
    cat <<'EOF' > /usr/local/bin/worker-health-check.sh
#!/bin/bash
# Health check script for IKS worker node

CLUSTER_ID="%s"
API_KEY="${IBM_API_KEY}"

check_worker_health() {
    local token=$(curl -s -X POST \
        "https://iam.cloud.ibm.com/identity/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "grant_type=urn:urn:ibm:params:oauth:grant-type:apikey&apikey=$API_KEY" | \
        grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
    
    local instance_id=$(curl -s http://169.254.169.254/metadata/v1/instance/id)
    
    if [ -n "$token" ] && [ -n "$instance_id" ]; then
        curl -s \
            "https://containers.cloud.ibm.com/global/v1/clusters/$CLUSTER_ID/workers" \
            -H "Authorization: Bearer $token" | \
            jq -r ".[] | select(.id | contains(\"$instance_id\")) | .health.state"
    else
        echo "unknown"
    fi
}

health_state=$(check_worker_health)
echo "Worker health state: $health_state"

case "$health_state" in
    "normal")
        exit 0
        ;;
    "warning")
        exit 1
        ;;
    "critical")
        exit 2
        ;;
    *)
        exit 3
        ;;
esac
EOF

    chmod +x /usr/local/bin/worker-health-check.sh
    
    # Create systemd service for health monitoring
    cat <<EOF > /etc/systemd/system/worker-health-monitor.service
[Unit]
Description=IKS Worker Health Monitor
After=network.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/worker-health-check.sh
User=root

[Install]
WantedBy=multi-user.target
EOF

    # Create timer for regular health checks
    cat <<EOF > /etc/systemd/system/worker-health-monitor.timer
[Unit]
Description=Run IKS Worker Health Monitor every 5 minutes
Requires=worker-health-monitor.service

[Timer]
OnCalendar=*:0/5
Persistent=true

[Install]
WantedBy=timers.target
EOF

    systemctl daemon-reload
    systemctl enable worker-health-monitor.timer
    systemctl start worker-health-monitor.timer
}

# Main execution
main() {
    echo "Starting IKS API worker registration..."
    
    # Install prerequisites
    install_prerequisites
    
    # Setup logging
    setup_logging
    
    # Add worker to cluster via IKS API
    if add_worker_to_cluster; then
        echo "Worker successfully added to IKS cluster"
        
        # Wait for worker to be ready
        wait_for_worker_ready
        
        # Setup health monitoring
        register_health_monitoring
        
        echo "IKS API node registration completed successfully!"
        echo "Worker node should appear in cluster shortly"
    else
        echo "Failed to add worker to IKS cluster"
        exit 1
    fi
}

# Run custom user data first if provided
%s

# Execute main function
main 2>&1 | tee /var/log/karpenter/iks-bootstrap.log

echo "IKS bootstrap completed!"
`, clusterID, options.Zone, options.Region, clusterID, p.formatCustomUserData(options.CustomUserData))

	// Base64 encode the script
	return base64.StdEncoding.EncodeToString([]byte(script)), nil
}

// formatCustomUserData formats custom user data for inclusion in script
func (p *IBMBootstrapProvider) formatCustomUserData(customUserData string) string {
	if customUserData == "" {
		return "# No custom user data provided"
	}
	
	return fmt.Sprintf(`echo "Running custom user data..."
%s
echo "Custom user data completed"`, customUserData)
}

// AddWorkerToIKSCluster adds a VPC instance to an IKS cluster as a worker node
func (p *IBMBootstrapProvider) AddWorkerToIKSCluster(ctx context.Context, options IKSWorkerPoolOptions) (*IKSWorkerResponse, error) {
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