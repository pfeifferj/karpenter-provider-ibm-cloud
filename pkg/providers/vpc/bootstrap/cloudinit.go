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
	"fmt"
	"strings"
	"text/template"
	
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/types"
)

// cloudInitTemplate defines the cloud-init script template for IBM Cloud VPC
const cloudInitTemplate = `#!/bin/bash
set -e

# Enhanced logging for debugging
LOG_FILE="/var/log/karpenter-bootstrap.log"
exec > >(tee -a $LOG_FILE) 2>&1

echo "$(date): ===== Karpenter IBM Cloud node bootstrap started ====="

# Configuration
CLUSTER_ENDPOINT="{{ .ClusterEndpoint }}"
BOOTSTRAP_TOKEN="{{ .BootstrapToken }}"
REGION="{{ .Region }}"
ZONE="{{ .Zone }}"

PRIVATE_IP="$(hostname -I | awk '{print $1}')"

echo "$(date): Private IP: $PRIVATE_IP"
echo "$(date): Region: $REGION"
echo "$(date): Cluster Endpoint: $CLUSTER_ENDPOINT"

# Set hostname in IBM Cloud format
HOSTNAME_IP=$(echo $PRIVATE_IP | tr '.' '-')
HOSTNAME="ip-${HOSTNAME_IP}.${REGION}.compute.ibmcloud.local"

echo "$(date): Setting hostname to: $HOSTNAME"
hostnamectl set-hostname "$HOSTNAME"
echo "127.0.0.1 $HOSTNAME" >> /etc/hosts
echo "$(date): ✅ Hostname configured"

# Test connectivity to API server
echo "$(date): Testing connectivity to API server..."
if curl -k --connect-timeout 10 $CLUSTER_ENDPOINT > /dev/null 2>&1; then
    echo "$(date): ✅ API server reachable"
else
    echo "$(date): ❌ API server NOT reachable - bootstrap may fail!"
    echo "$(date): Endpoint: $CLUSTER_ENDPOINT"
fi

# Essential system configuration for kubeadm
echo "$(date): Configuring system for kubeadm..."
echo 'net.ipv4.ip_forward = 1' >> /etc/sysctl.conf && sysctl -p
swapoff -a && sed -i '/swap/d' /etc/fstab
echo "$(date): ✅ System configured for kubeadm"

# Install prerequisites
echo "$(date): Installing prerequisites..."
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y curl apt-transport-https ca-certificates gnupg lsb-release
echo "$(date): ✅ Prerequisites installed"

# Install container runtime
install_container_runtime() {
    case "$CONTAINER_RUNTIME" in
        "containerd")
            install_containerd
            ;;
        "cri-o")
            install_crio
            ;;
        *)
            echo "Unsupported container runtime: $CONTAINER_RUNTIME"
            exit 1
            ;;
    esac
}

# Install containerd
echo "$(date): Installing containerd..."
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | apt-key add -
add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable"
apt-get update
apt-get install -y containerd.io
echo "$(date): ✅ Containerd installed"

# Configure containerd
echo "$(date): Configuring containerd..."
mkdir -p /etc/containerd
containerd config default | sed 's/SystemdCgroup = false/SystemdCgroup = true/' > /etc/containerd/config.toml
systemctl restart containerd
systemctl enable containerd
echo "$(date): ✅ Containerd configured and started"

install_crio() {
    echo "Installing CRI-O..."
    
    # Add CRI-O repository
    curl -fsSL https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable/xUbuntu_20.04/Release.key | apt-key add -
    curl -fsSL https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable:/cri-o:/1.24/xUbuntu_20.04/Release.key | apt-key add -
    
    echo "deb https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable/xUbuntu_20.04/ /" > /etc/apt/sources.list.d/devel:kubic:libcontainers:stable.list
    echo "deb https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable:/cri-o:/1.24/xUbuntu_20.04/ /" > /etc/apt/sources.list.d/devel:kubic:libcontainers:stable:cri-o:1.24.list
    
    apt-get update
    apt-get install -y cri-o cri-o-runc
    
    # Start CRI-O
    systemctl enable crio
    systemctl start crio
}

# Install Kubernetes components
echo "$(date): Installing Kubernetes components..."
curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.31/deb/Release.key | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.31/deb/ /' > /etc/apt/sources.list.d/kubernetes.list
apt-get update
apt-get install -y kubelet kubeadm kubectl
apt-mark hold kubelet kubeadm kubectl
echo "$(date): ✅ Kubernetes components installed"

# Configure kubelet with cloud provider
echo "$(date): Configuring kubelet..."
mkdir -p /var/lib/kubelet
cat > /var/lib/kubelet/kubeadm-flags.env << EOF
KUBELET_KUBEADM_ARGS="--cloud-provider=external --hostname-override=$HOSTNAME"
EOF
echo "$(date): ✅ Kubelet configured"

# Join the cluster using API server endpoint
echo "$(date): Attempting to join cluster using API server: $CLUSTER_ENDPOINT"
echo "$(date): Hostname: $HOSTNAME"
echo "$(date): Bootstrap Token: ${BOOTSTRAP_TOKEN:0:10}..."
if kubeadm join $CLUSTER_ENDPOINT --token $BOOTSTRAP_TOKEN --discovery-token-unsafe-skip-ca-verification; then
    echo "$(date): ✅ Successfully joined cluster!"
else
    echo "$(date): ❌ Failed to join cluster"
    # Show detailed error info
    echo "$(date): Checking kubelet status..."
    systemctl status kubelet --no-pager
    echo "$(date): Checking kubelet logs..."
    journalctl -u kubelet --no-pager -n 20
    exit 1
fi

echo "$(date): ===== Bootstrap process completed ====="
echo "$(date): Log available at: $LOG_FILE"


# Run custom user data first if provided
{{ if .CustomUserData }}
echo "$(date): Running custom user data..."
{{ .CustomUserData }}
{{ end }}

echo "$(date): Node bootstrap completed!"

`

// generateCloudInitScript generates a cloud-init script for node bootstrapping
func (p *VPCBootstrapProvider) generateCloudInitScript(ctx context.Context, options types.Options) (string, error) {
	// Create template
	tmpl, err := template.New("cloudinit").Parse(cloudInitTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing cloud-init template: %w", err)
	}

	// Build template data
	data := struct {
		types.Options
		KubeletExtraArgs string
		CABundle         string
	}{
		Options:          options,
		KubeletExtraArgs: p.buildKubeletExtraArgs(options.KubeletConfig),
		CABundle:         base64.StdEncoding.EncodeToString([]byte(options.CABundle)),
	}

	// Execute template
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing cloud-init template: %w", err)
	}

	// Base64 encode the script
	script := buf.String()
	return base64.StdEncoding.EncodeToString([]byte(script)), nil
}

// buildKubeletExtraArgs builds kubelet extra arguments string
func (p *VPCBootstrapProvider) buildKubeletExtraArgs(config *types.KubeletConfig) string {
	if config == nil || len(config.ExtraArgs) == 0 {
		return ""
	}

	var args []string
	for key, value := range config.ExtraArgs {
		args = append(args, fmt.Sprintf("--%s=%s", key, value))
	}

	return strings.Join(args, " ")
}