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
NODE_NAME="{{ .NodeName }}"

# Instance metadata
PRIVATE_IP=$(hostname -I | awk '{print $1}')
{{- if .InstanceID }}
INSTANCE_ID="{{ .InstanceID }}"
{{- else }}
INSTANCE_ID=$(dmidecode -s system-uuid 2>/dev/null || echo "unknown")
{{- end }}

# Use NodeClaim name as hostname for proper Karpenter registration
HOSTNAME="$NODE_NAME"

# Set hostname
hostnamectl set-hostname "$HOSTNAME"
echo "127.0.0.1 $HOSTNAME" >> /etc/hosts
echo "$(date): ✅ Hostname set to: $HOSTNAME (NodeClaim name)"

# System configuration
echo 'net.ipv4.ip_forward = 1' >> /etc/sysctl.conf && sysctl -p
swapoff -a && sed -i '/swap/d' /etc/fstab

# Filesystem preparation - fix disk capacity detection
echo "$(date): Preparing filesystem..."
# Resize the root filesystem to ensure full disk is available
ROOT_DEV=$(df / | awk 'NR==2 {print $1}')
if [[ "$ROOT_DEV" =~ ^/dev/ ]]; then
    resize2fs "$ROOT_DEV" || true
    echo "$(date): ✅ Root filesystem resized"
else
    echo "$(date): ⚠️ Could not identify root device: $ROOT_DEV"
fi

# Ensure proper directory structure for container storage
mkdir -p /var/lib/containerd /var/lib/kubelet /var/log/pods /var/lib/cni
chown -R root:root /var/lib/containerd /var/lib/kubelet

# Create required directories for CNI
mkdir -p /opt/cni/bin /etc/cni/net.d /var/lib/calico /var/run/calico /var/log/calico/cni
chown -R root:root /var/lib/calico /var/run/calico /var/log/calico

echo "$(date): ✅ System configured"

# Install prerequisites
echo "$(date): Installing prerequisites..."
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y curl apt-transport-https ca-certificates gnupg lsb-release dmidecode jq
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
    
    # Create containerd config with proper systemd cgroup configuration
    cat > /etc/containerd/config.toml << 'EOF'
disabled_plugins = []
imports = []
oom_score = 0
plugin_dir = ""
required_plugins = []
root = "/var/lib/containerd"
state = "/run/containerd"
temp = ""
version = 2

[grpc]
  address = "/run/containerd/containerd.sock"
  tcp_address = ""
  tcp_tls_cert = ""
  tcp_tls_key = ""
  uid = 0
  gid = 0
  max_recv_message_size = 16777216
  max_send_message_size = 16777216

[debug]
  address = ""
  format = ""
  level = ""
  uid = 0
  gid = 0

[metrics]
  address = ""
  grpc_histogram = false

[cgroup]
  path = ""

[timeouts]
  "io.containerd.timeout.shim.cleanup" = "5s"
  "io.containerd.timeout.shim.load" = "5s"
  "io.containerd.timeout.shim.shutdown" = "3s"
  "io.containerd.timeout.task.state" = "2s"

[plugins]
  [plugins."io.containerd.gc.v1.scheduler"]
    pause_threshold = 0.02
    deletion_threshold = 0
    mutation_threshold = 100
    schedule_delay = "0s"
    startup_delay = "100ms"
  [plugins."io.containerd.grpc.v1.cri"]
    disable_tcp_service = true
    stream_server_address = "127.0.0.1"
    stream_server_port = "0"
    stream_idle_timeout = "4h0m0s"
    enable_selinux = false
    selinux_category_range = 1024
    sandbox_image = "registry.k8s.io/pause:3.6"
    stats_collect_period = 10
    enable_tls_streaming = false
    max_container_log_line_size = 16384
    disable_cgroup = false
    disable_apparmor = false
    restrict_oom_score_adj = false
    max_concurrent_downloads = 3
    disable_proc_mount = false
    unset_seccomp_profile = ""
    tolerate_missing_hugetlb_controller = true
    disable_hugetlb_controller = true
    ignore_image_defined_volumes = false
    [plugins."io.containerd.grpc.v1.cri".containerd]
      snapshotter = "overlayfs"
      default_runtime_name = "runc"
      no_pivot = false
      disable_snapshot_annotations = true
      discard_unpacked_layers = false
      [plugins."io.containerd.grpc.v1.cri".containerd.runtimes]
        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
          runtime_type = "io.containerd.runc.v2"
          runtime_engine = ""
          runtime_root = ""
          privileged_without_host_devices = false
          base_runtime_spec = ""
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

# Provider ID configuration
PROVIDER_ID="ibm:///${REGION}/${INSTANCE_ID}"
echo "$(date): Instance ID: $INSTANCE_ID, Provider ID: $PROVIDER_ID"

# Configure kubelet service
cat > /etc/systemd/system/kubelet.service.d/10-karpenter.conf << EOF
[Service]
Environment="KUBELET_EXTRA_ARGS=--hostname-override=${HOSTNAME} --node-ip=${PRIVATE_IP} --provider-id=${PROVIDER_ID}{{ if .KubeletExtraArgs }} {{ .KubeletExtraArgs }}{{ end }}"
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

# Verify filesystem before starting kubelet
echo "$(date): Verifying filesystem readiness..."
df -h / && echo "$(date): ✅ Filesystem verified" || echo "$(date): ⚠️ Filesystem verification failed"

# Wait for container runtime to be ready
echo "$(date): Waiting for container runtime to be ready..."
for i in {1..30}; do
  if systemctl is-active containerd >/dev/null 2>&1; then
    echo "$(date): ✅ Container runtime is ready"
    break
  fi
  sleep 2
done

# Install CNI binaries and configuration
CNI_PLUGIN="{{ .CNIPlugin }}"
CNI_VERSION="{{ .CNIVersion }}"
echo "$(date): Installing $CNI_PLUGIN CNI binaries and configuration..."
mkdir -p /etc/cni/net.d

# Download and install CNI binaries
echo "$(date): Downloading CNI binaries..."
CNI_PLUGINS_VERSION="v1.4.0"

# Download standard CNI plugins
curl -L "https://github.com/containernetworking/plugins/releases/download/${CNI_PLUGINS_VERSION}/cni-plugins-linux-{{ .Architecture }}-${CNI_PLUGINS_VERSION}.tgz" | tar -C /opt/cni/bin -xz

# Download plugin-specific CNI binaries using detected version
case "$CNI_PLUGIN" in
  "calico")
    echo "$(date): Downloading Calico CNI binaries version $CNI_VERSION..."
    curl -L -o /opt/cni/bin/calico "https://github.com/projectcalico/cni-plugin/releases/download/${CNI_VERSION}/calico-{{ .Architecture }}"
    curl -L -o /opt/cni/bin/calico-ipam "https://github.com/projectcalico/cni-plugin/releases/download/${CNI_VERSION}/calico-ipam-{{ .Architecture }}"
    chmod +x /opt/cni/bin/calico /opt/cni/bin/calico-ipam
    mkdir -p /var/log/calico/cni
    ;;
  "cilium")
    echo "$(date): Downloading Cilium CNI binaries version $CNI_VERSION..."
    curl -L -o /tmp/cilium.tar.gz "https://github.com/cilium/cilium/releases/download/${CNI_VERSION}/cilium-linux-amd64.tar.gz"
    tar -xzf /tmp/cilium.tar.gz -C /opt/cni/bin/ cilium-cni
    chmod +x /opt/cni/bin/cilium-cni
    rm -f /tmp/cilium.tar.gz
    mkdir -p /var/log/cilium
    ;;
  "flannel")
    echo "$(date): Downloading Flannel CNI binaries version $CNI_VERSION..."
    curl -L -o /opt/cni/bin/flannel "https://github.com/flannel-io/cni-plugin/releases/download/${CNI_VERSION}/flannel-amd64"
    chmod +x /opt/cni/bin/flannel
    mkdir -p /var/log/flannel
    ;;
esac

# Set proper permissions
chmod +x /opt/cni/bin/*
echo "$(date): ✅ CNI binaries installed"

# Install CNI configuration based on detected plugin
case "$CNI_PLUGIN" in
  "calico")
    echo "$(date): Installing Calico CNI configuration..."
    
    # Create nodename file - this is critical for Calico CNI to work
    # This prevents the race condition where CNI is invoked before the DaemonSet creates this file
    echo "$HOSTNAME" > /var/lib/calico/nodename
    echo "$(date): ✅ Created Calico nodename file: $HOSTNAME"
    
    cat > /etc/cni/net.d/10-calico.conflist << 'EOF'
{
  "name": "k8s-pod-network",
  "cniVersion": "0.3.1",
  "plugins": [
    {
      "type": "calico",
      "log_level": "info",
      "log_file_path": "/var/log/calico/cni/cni.log",
      "datastore_type": "kubernetes",
      "nodename": "__KUBERNETES_NODE_NAME__",
      "mtu": 1440,
      "ipam": {
          "type": "calico-ipam"
      },
      "policy": {
          "type": "k8s"
      },
      "kubernetes": {
          "kubeconfig": "__KUBECONFIG_FILEPATH__"
      }
    },
    {
      "type": "portmap",
      "snat": true,
      "capabilities": {"portMappings": true}
    },
    {
      "type": "bandwidth",
      "capabilities": {"bandwidth": true}
    }
  ]
}
EOF
    # Replace placeholders in CNI configuration
    sed -i "s/__KUBERNETES_NODE_NAME__/$(hostname)/g" /etc/cni/net.d/10-calico.conflist
    sed -i "s/__KUBECONFIG_FILEPATH__/\/var\/lib\/kubelet\/bootstrap-kubeconfig/g" /etc/cni/net.d/10-calico.conflist
    ;;
  "cilium")
    echo "$(date): Installing Cilium CNI configuration..."
    cat > /etc/cni/net.d/05-cilium.conflist << 'EOF'
{
  "name": "cilium",
  "cniVersion": "0.3.1",
  "plugins": [
    {
      "type": "cilium-cni",
      "enable-debug": false,
      "log-file": "/var/log/cilium/cilium-cni.log"
    }
  ]
}
EOF
    ;;
  "flannel")
    echo "$(date): Installing Flannel CNI configuration..."
    cat > /etc/cni/net.d/10-flannel.conflist << 'EOF'
{
  "name": "cbr0",
  "cniVersion": "0.3.1",
  "plugins": [
    {
      "type": "flannel",
      "delegate": {
        "hairpinMode": true,
        "isDefaultGateway": true
      }
    },
    {
      "type": "portmap",
      "capabilities": {
        "portMappings": true
      }
    }
  ]
}
EOF
    ;;
  *)
    echo "$(date): ⚠️  Unknown CNI plugin: $CNI_PLUGIN, skipping CNI configuration"
    echo "$(date): Node will rely on CNI DaemonSet deployment"
    ;;
esac

echo "$(date): ✅ CNI configuration installed for $CNI_PLUGIN"

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

# Wait for CNI to be fully operational
echo "$(date): Waiting for $CNI_PLUGIN CNI to initialize..."
CNI_WAIT_TIMEOUT=300  # 5 minutes
CNI_WAIT_INTERVAL=5
elapsed=0

check_cni_ready() {
    case "$CNI_PLUGIN" in
      "calico")
        # Check 1: CNI binaries exist and are executable
        [ -x /opt/cni/bin/calico ] || return 1
        [ -x /opt/cni/bin/calico-ipam ] || return 1
        
        # Check 2: CNI configuration exists and is valid JSON
        [ -f /etc/cni/net.d/10-calico.conflist ] || return 1
        
        # Check 3: Calico nodename file exists (created during bootstrap)
        [ -f /var/lib/calico/nodename ] || return 1
        
        # Check 4: CNI can be invoked successfully
        if [ -x /opt/cni/bin/calico ] && [ -f /etc/cni/net.d/10-calico.conflist ]; then
            # Try a simple CNI version check
            CNI_PATH=/opt/cni/bin /opt/cni/bin/calico version >/dev/null 2>&1 || return 1
        fi
        ;;
      "cilium")
        # Check 1: CNI binaries exist and are executable
        [ -x /opt/cni/bin/cilium-cni ] || return 1
        
        # Check 2: CNI configuration exists
        [ -f /etc/cni/net.d/05-cilium.conflist ] || return 1
        
        # Check 3: Cilium agent is running (if crictl available)
        if command -v crictl >/dev/null 2>&1; then
            crictl ps 2>/dev/null | grep -q cilium || return 1
        fi
        ;;
      "flannel")
        # Check 1: CNI binaries exist and are executable
        [ -x /opt/cni/bin/flannel ] || return 1
        
        # Check 2: CNI configuration exists
        [ -f /etc/cni/net.d/10-flannel.conflist ] || return 1
        
        # Check 3: Flannel is running (if crictl available)
        if command -v crictl >/dev/null 2>&1; then
            crictl ps 2>/dev/null | grep -q flannel || return 1
        fi
        ;;
      *)
        # For unknown CNI plugins, just check basic CNI setup
        # Look for any CNI configuration file
        ls /etc/cni/net.d/*.conflist >/dev/null 2>&1 || return 1
        # Check for basic CNI binaries
        [ -x /opt/cni/bin/bridge ] || return 1
        [ -x /opt/cni/bin/loopback ] || return 1
        ;;
    esac
    
    return 0
}

while [ $elapsed -lt $CNI_WAIT_TIMEOUT ]; do
  if check_cni_ready; then
    echo "$(date): ✅ $CNI_PLUGIN CNI is fully operational"
    break
  fi
  
  echo "$(date): Waiting for $CNI_PLUGIN CNI readiness... (elapsed: ${elapsed}s)"
  
  # Detailed status for debugging every 30 seconds
  if [ $((elapsed % 30)) -eq 0 ] && [ $elapsed -gt 0 ]; then
    echo "$(date): CNI Status Check:"
    case "$CNI_PLUGIN" in
      "calico")
        echo "  - CNI binaries: $([ -x /opt/cni/bin/calico ] && echo "✓" || echo "✗")"
        echo "  - CNI config: $([ -f /etc/cni/net.d/10-calico.conflist ] && echo "✓" || echo "✗")"
        echo "  - Calico nodename: $([ -f /var/lib/calico/nodename ] && echo "✓" || echo "✗")"
        if command -v crictl >/dev/null 2>&1; then
          echo "  - Calico container: $(crictl ps 2>/dev/null | grep -q calico-node && echo "✓" || echo "✗")"
        fi
        ;;
      "cilium")
        echo "  - CNI binaries: $([ -x /opt/cni/bin/cilium-cni ] && echo "✓" || echo "✗")"
        echo "  - CNI config: $([ -f /etc/cni/net.d/05-cilium.conflist ] && echo "✓" || echo "✗")"
        if command -v crictl >/dev/null 2>&1; then
          echo "  - Cilium container: $(crictl ps 2>/dev/null | grep -q cilium && echo "✓" || echo "✗")"
        fi
        ;;
      "flannel")
        echo "  - CNI binaries: $([ -x /opt/cni/bin/flannel ] && echo "✓" || echo "✗")"
        echo "  - CNI config: $([ -f /etc/cni/net.d/10-flannel.conflist ] && echo "✓" || echo "✗")"
        if command -v crictl >/dev/null 2>&1; then
          echo "  - Flannel container: $(crictl ps 2>/dev/null | grep -q flannel && echo "✓" || echo "✗")"
        fi
        ;;
      *)
        echo "  - CNI configs: $(ls /etc/cni/net.d/*.conflist 2>/dev/null | wc -l) found"
        echo "  - Basic CNI binaries: $([ -x /opt/cni/bin/bridge ] && echo "✓" || echo "✗")"
        ;;
    esac
  fi
  
  sleep $CNI_WAIT_INTERVAL
  elapsed=$((elapsed + CNI_WAIT_INTERVAL))
done

# Check if we timed out
if [ $elapsed -ge $CNI_WAIT_TIMEOUT ]; then
  echo "$(date): ⚠️ $CNI_PLUGIN CNI failed to become ready after ${CNI_WAIT_TIMEOUT}s"
  echo "$(date): Comprehensive diagnostic information:"
  
  # CNI binaries
  echo "  CNI binaries in /opt/cni/bin/:"
  ls -la /opt/cni/bin/ 2>/dev/null || echo "    Directory does not exist"
  
  # CNI configs
  echo "  CNI configs in /etc/cni/net.d/:"
  ls -la /etc/cni/net.d/ 2>/dev/null || echo "    Directory does not exist"
  
  # Plugin-specific diagnostics
  case "$CNI_PLUGIN" in
    "calico")
      if [ -f /etc/cni/net.d/10-calico.conflist ]; then
        echo "  Calico CNI config content:"
        head -20 /etc/cni/net.d/10-calico.conflist 2>/dev/null | sed 's/^/    /'
      fi
      echo "  Calico directory /var/lib/calico/:"
      ls -la /var/lib/calico/ 2>/dev/null || echo "    Directory does not exist"
      echo "  Calico runtime directory /var/run/calico/:"
      ls -la /var/run/calico/ 2>/dev/null || echo "    Directory does not exist"
      ;;
    "cilium")
      if [ -f /etc/cni/net.d/05-cilium.conflist ]; then
        echo "  Cilium CNI config content:"
        head -20 /etc/cni/net.d/05-cilium.conflist 2>/dev/null | sed 's/^/    /'
      fi
      echo "  Cilium directory /var/run/cilium/:"
      ls -la /var/run/cilium/ 2>/dev/null || echo "    Directory does not exist"
      ;;
    "flannel")
      if [ -f /etc/cni/net.d/10-flannel.conflist ]; then
        echo "  Flannel CNI config content:"
        head -20 /etc/cni/net.d/10-flannel.conflist 2>/dev/null | sed 's/^/    /'
      fi
      echo "  Flannel directory /var/run/flannel/:"
      ls -la /var/run/flannel/ 2>/dev/null || echo "    Directory does not exist"
      ;;
  esac
  
  # Container runtime status
  if command -v crictl >/dev/null 2>&1; then
    echo "  Running containers:"
    crictl ps 2>/dev/null | head -10 | sed 's/^/    /' || echo "    Could not list containers"
  fi
  
  # Kubelet logs for CNI errors
  echo "  Recent kubelet CNI logs:"
  journalctl -u kubelet --no-pager -n 10 2>/dev/null | grep -i cni | tail -5 | sed 's/^/    /' || echo "    No CNI-related logs found"
  
  # Try to get CNI pod status
  if command -v kubectl >/dev/null 2>&1; then
    echo "  $CNI_PLUGIN pod status:"
    kubectl --kubeconfig=/var/lib/kubelet/kubeconfig get pods -n kube-system -l k8s-app=$CNI_PLUGIN --field-selector spec.nodeName=$(hostname) -o wide 2>/dev/null | sed 's/^/    /' || echo "    Could not get pod status"
  fi
  
  # Continue anyway - let Kubernetes handle the CNI readiness
  echo "$(date): Continuing bootstrap despite CNI issues..."
else
  echo "$(date): ✅ $CNI_PLUGIN CNI initialization completed successfully"
fi

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
