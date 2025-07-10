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
	"fmt"
	"strings"
	"text/template"
	
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/types"
)

// cloudInitTemplate defines the cloud-init script template for IBM Cloud VPC using direct kubelet configuration
const cloudInitTemplate = `#!/bin/bash
set -euo pipefail

# Enhanced logging
LOG_FILE="/var/log/karpenter-bootstrap.log"
exec > >(tee -a $LOG_FILE) 2>&1

echo "$(date): ===== Karpenter IBM Cloud Bootstrap (Direct Kubelet) ====="

# Configuration
CLUSTER_ENDPOINT="{{ .ClusterEndpoint }}"
BOOTSTRAP_TOKEN="{{ .BootstrapToken }}"
CLUSTER_DNS="{{ .DNSClusterIP }}"
REGION="{{ .Region }}"
ZONE="{{ .Zone }}"

# Instance metadata
INSTANCE_ID=$(dmidecode -s system-uuid 2>/dev/null || echo "unknown")
PRIVATE_IP=$(hostname -I | awk '{print $1}')
HOSTNAME="ip-$(echo $PRIVATE_IP | tr '.' '-').{{ .Region }}.compute.ibmcloud.local"

# Set hostname
hostnamectl set-hostname "$HOSTNAME"
echo "127.0.0.1 $HOSTNAME" >> /etc/hosts
echo "$(date): ✅ Hostname set to: $HOSTNAME"

# System configuration
echo 'net.ipv4.ip_forward = 1' >> /etc/sysctl.conf && sysctl -p
swapoff -a && sed -i '/swap/d' /etc/fstab
echo "$(date): ✅ System configured"

# Install prerequisites
echo "$(date): Installing prerequisites..."
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y curl apt-transport-https ca-certificates gnupg lsb-release dmidecode
echo "$(date): ✅ Prerequisites installed"

# Install container runtime based on configuration
CONTAINER_RUNTIME="{{ .ContainerRuntime }}"
echo "$(date): Installing container runtime: $CONTAINER_RUNTIME"

install_containerd() {
    echo "$(date): Installing containerd..."
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | apt-key add -
    add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable"
    apt-get update
    apt-get install -y containerd.io
    echo "$(date): ✅ Containerd installed"

    # Configure containerd
    echo "$(date): Configuring containerd..."
    mkdir -p /etc/containerd
    containerd config default > /etc/containerd/config.toml
    
    # Enable systemd cgroups for both containerd and kubelet compatibility
    sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
    
    # Ensure proper cgroup configuration for systemd
    cat >> /etc/containerd/config.toml << 'EOF'

# Additional systemd cgroup configuration
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc.options]
  SystemdCgroup = true
EOF
    
    systemctl restart containerd
    systemctl enable containerd
    echo "$(date): ✅ Containerd configured and started"
}

install_crio() {
    echo "$(date): Installing CRI-O..."
    
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
    echo "$(date): ✅ CRI-O configured and started"
}

# Install the configured container runtime
case "$CONTAINER_RUNTIME" in
    "containerd"|"")
        install_containerd
        ;;
    "cri-o")
        install_crio
        ;;
    *)
        echo "$(date): ❌ Unsupported container runtime: $CONTAINER_RUNTIME"
        echo "$(date): Supported runtimes: containerd, cri-o"
        exit 1
        ;;
esac

# Install Kubernetes components
echo "$(date): Installing Kubernetes components..."
curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.31/deb/Release.key | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.31/deb/ /' > /etc/apt/sources.list.d/kubernetes.list
apt-get update
apt-get install -y kubelet kubectl
apt-mark hold kubelet kubectl
echo "$(date): ✅ Kubernetes components installed"

# Create directories
mkdir -p /etc/kubernetes/pki /var/lib/kubelet /etc/systemd/system/kubelet.service.d

# Write CA certificate
cat > /etc/kubernetes/pki/ca.crt << 'EOF'
{{ .CABundle }}
EOF
echo "$(date): ✅ CA certificate created"

# Create bootstrap kubeconfig
cat > /var/lib/kubelet/bootstrap-kubeconfig << EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority: /etc/kubernetes/pki/ca.crt
    server: ${CLUSTER_ENDPOINT}
  name: default
contexts:
- context:
    cluster: default
    user: kubelet-bootstrap
  name: default
current-context: default
users:
- name: kubelet-bootstrap
  user:
    token: ${BOOTSTRAP_TOKEN}
EOF
echo "$(date): ✅ Bootstrap kubeconfig created"

# Create kubelet configuration
cat > /var/lib/kubelet/config.yaml << EOF
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
authentication:
  anonymous:
    enabled: false
  webhook:
    enabled: true
  x509:
    clientCAFile: /etc/kubernetes/pki/ca.crt
authorization:
  mode: Webhook
clusterDomain: cluster.local
clusterDNS:
  - ${CLUSTER_DNS}
rotateCertificates: true
serverTLSBootstrap: true
cloudProvider: external
registerNode: true
cgroupDriver: systemd
registerWithTaints:
{{ range .Taints }}
- key: {{ .Key }}
  value: {{ .Value }}
  effect: {{ .Effect }}
{{ end }}
nodeLabels:
{{ range $key, $value := .Labels }}
  {{ $key }}: "{{ $value }}"
{{ end }}
EOF
echo "$(date): ✅ Kubelet configuration created"

# Configure kubelet service
cat > /etc/systemd/system/kubelet.service.d/10-karpenter.conf << EOF
[Service]
Environment="KUBELET_EXTRA_ARGS=--cloud-provider=external --hostname-override=${HOSTNAME} --node-ip=${PRIVATE_IP}{{ if .KubeletExtraArgs }} {{ .KubeletExtraArgs }}{{ end }}"
EOF

# Create kubelet service override
cat > /etc/systemd/system/kubelet.service << 'EOF'
[Unit]
Description=kubelet: The Kubernetes Node Agent
Documentation=https://kubernetes.io/docs/
Wants=network-online.target
After=network-online.target

[Service]
ExecStart=/usr/bin/kubelet \
  --bootstrap-kubeconfig=/var/lib/kubelet/bootstrap-kubeconfig \
  --kubeconfig=/var/lib/kubelet/kubeconfig \
  --config=/var/lib/kubelet/config.yaml \
  $KUBELET_EXTRA_ARGS
Restart=always
StartLimitInterval=0
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

echo "$(date): ✅ Kubelet service configured"

# Start kubelet
systemctl daemon-reload
systemctl enable kubelet
systemctl start kubelet

echo "$(date): Waiting for node to be ready..."
for i in {1..60}; do
  if systemctl is-active kubelet >/dev/null 2>&1; then
    echo "$(date): ✅ Kubelet is running"
    break
  fi
  sleep 5
done

echo "$(date): Checking kubelet status..."
systemctl status kubelet --no-pager || true
journalctl -u kubelet --no-pager -n 20 || true

# Run custom user data if provided
{{ if .CustomUserData }}
echo "$(date): Running custom user data..."
{{ .CustomUserData }}
{{ end }}

echo "$(date): ===== Bootstrap completed ====="
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
	}{
		Options:          options,
		KubeletExtraArgs: p.buildKubeletExtraArgs(options.KubeletConfig),
	}

	// Execute template
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing cloud-init template: %w", err)
	}

	// Return the script as plain text 
	script := buf.String()
	return script, nil
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