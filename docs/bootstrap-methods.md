# Bootstrap Methods

## Overview

The Karpenter IBM Cloud Provider supports three bootstrap methods to join nodes to your Kubernetes cluster:

1. **Cloud-Init Bootstrap** - Full Kubernetes setup via cloud-init scripts
2. **IKS API Bootstrap** - Native IBM Kubernetes Service integration
3. **Auto Bootstrap** - Intelligent method selection

Each method has specific use cases, advantages, and requirements.

## Cloud-Init Bootstrap

### When to Use
- Self-managed Kubernetes clusters using VPC/non-IKS environments

### Configuration
```yaml
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: nodeclass-cloudinit
spec:
  bootstrapMode: cloud-init
  region: us-south
  vpc: vpc-12345
  image: ubuntu-20-04
  userData: |
    #!/bin/bash
    echo "Custom user configuration"
```

### Features

#### Automatic Cluster Discovery
- Detects cluster endpoint and CA certificate
- Discovers DNS cluster IP automatically
- Configures cluster CIDR from existing nodes

#### Dynamic CNI Detection
- Auto-detects Calico, Cilium, or Flannel
- Configures network plugins automatically
- Falls back to generic CNI configuration

#### Container Runtime Setup
- Installs and configures containerd (default)
- Supports CRI-O 
- Auto-detects runtime from cluster

#### Complete Kubernetes Setup
- Installs kubelet, kubeadm, kubectl
- Configures systemd services
- Sets up proper node labels and taints

### Customization Options

#### **Custom User Data**
```yaml
spec:
  userData: |
    #!/bin/bash
    # Your custom pre-bootstrap configuration
    echo "Installing custom packages..."
    apt-get update && apt-get install -y htop vim
    
    # Custom environment variables
    echo "CUSTOM_VAR=value" >> /etc/environment
    
    # Custom service configuration
    systemctl enable my-custom-service
```

#### **Environment Variables**
```bash
# Override container runtime
export CONTAINER_RUNTIME=crio

# Custom CNI configuration
export CNI_PLUGIN=cilium

# Debug mode
export DEBUG=true
```

## IKS API Bootstrap

### When to Use
- IBM Kubernetes Service (IKS) clusters

### Configuration
```yaml
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: nodeclass-iks
spec:
  bootstrapMode: iks-api
  region: us-south
  vpc: vpc-12345
  image: ubuntu-20-04
  iksClusterID: bq4f2r4w0g0r5v8dufsg
  iksWorkerPoolID: bq4f2r4w0g0r5v8dufsg-pool  # Optional
  userData: |
    #!/bin/bash
    echo "Custom post-registration setup"
```

### Features

#### Native IKS Integration
- Uses IBM Kubernetes Service APIs
- Automatic worker registration
- Consistent with IKS worker pools

#### Simplified Configuration
- No Kubernetes installation required
- Automatic cluster configuration
- IBM-managed security and compliance

#### Health Monitoring
- Built-in health checks
- Integration with IKS monitoring
- Automatic failure detection

### Requirements

#### **IKS Cluster ID**
- Must be a valid IKS cluster ID
- Cluster must be in same region as nodes
- API key must have access to cluster

## Auto Bootstrap

### When to Use
- Simplified configuration

### Configuration
```yaml
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: nodeclass-auto
spec:
  bootstrapMode: auto  # Default value
  region: us-south
  vpc: vpc-12345
  image: ubuntu-20-04
  # Optional: Provide IKS cluster ID for IKS API preference
  iksClusterID: bq4f2r4w0g0r5v8dufsg
```

### Selection Logic

#### **Method Selection Priority**:
1. **IKS API Mode** (if conditions met):
   - Valid `iksClusterID` provided
   - IKS cluster accessible
   - API credentials valid

2. **Cloud-Init Mode** (fallback):
   - No IKS cluster ID provided
   - IKS API unavailable
   - Self-managed cluster detected

### Environment Variable Support

All bootstrap modes support environment variable configuration:

```bash
# Bootstrap mode override
export BOOTSTRAP_MODE=cloud-init  # or iks-api, auto

# IKS configuration
export IKS_CLUSTER_ID=your-cluster-id
export IKS_WORKER_POOL_ID=your-pool-id

# Container runtime
export CONTAINER_RUNTIME=containerd  # or crio

# CNI plugin
export CNI_PLUGIN=calico  # or cilium, flannel

# Debug settings
export DEBUG=true
```

## Advanced Configuration

### Multi-Mode Deployment

**Scenario**: Different NodeClasses for different workloads

```yaml
# Production workloads - IKS API for consistency
---
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: production-nodes
spec:
  bootstrapMode: iks-api
  iksClusterID: prod-cluster-id
  instanceProfile: mx2-4x32
  
# Development workloads - Cloud-init for flexibility  
---
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: dev-nodes
spec:
  bootstrapMode: cloud-init
  instanceProfile: bx2-2x8
  userData: |
    #!/bin/bash
    # Development-specific configuration
    echo "dev=true" >> /etc/environment
```

### Bootstrap Customization

#### **Custom CNI Configuration**
```yaml
spec:
  bootstrapMode: cloud-init
  userData: |
    #!/bin/bash
    # Install custom CNI plugin
    curl -L -o /opt/cni/bin/custom-cni https://releases.example.com/cni
    
    # Custom CNI configuration
    cat > /etc/cni/net.d/10-custom.conf <<EOF
    {
      "cniVersion": "0.4.0",
      "name": "custom-network",
      "type": "custom-cni"
    }
    EOF
```

#### **Security Hardening**
```yaml
spec:
  userData: |
    #!/bin/bash
    # Security configurations
    echo "net.ipv4.ip_forward=0" >> /etc/sysctl.conf
    
    # Install security tools
    apt-get install -y fail2ban aide
    
    # Configure firewall
    ufw enable
    ufw default deny incoming
```

## Troubleshooting Bootstrap Issues

### Common Problems

#### **Bootstrap Script Failures**
```bash
# Check bootstrap logs
sudo journalctl -u cloud-final
sudo tail -f /var/log/cloud-init-output.log

# Debug bootstrap script
sudo cat /var/lib/cloud/instance/scripts/part-001
```

#### **Cluster Join Failures**
```bash
# Check kubelet status
sudo systemctl status kubelet
sudo journalctl -u kubelet --no-pager

# Verify cluster connectivity
kubectl config view
curl -k https://CLUSTER_ENDPOINT/healthz
```

## Related Documentation

- [Supported CNI/CRI](./supported-cni-cri.md) - Compatible network and runtime plugins
- [Limitations](./limitations.md) - Current constraints and workarounds
