# Supported CNI and CRI

This document outlines the supported Container Network Interface (CNI) plugins and Container Runtime Interface (CRI) implementations with the Karpenter IBM Cloud Provider.

## Container Network Interface (CNI) Support

### Tested and Supported CNI Plugins

| CNI Plugin | Status | Bootstrap Mode | Notes |
|------------|--------|----------------|-------|
| **Calico** | ✅ Fully Supported | cloud-init, iks-api | Default for most IBM Cloud deployments |
| **Cilium** | ✅ Fully Supported | cloud-init | Advanced networking features supported |
| **Flannel** | ✅ Basic Support | cloud-init | Simple overlay networking |

### CNI Auto-Detection

The Karpenter IBM Cloud Provider includes **automatic CNI detection** in cloud-init bootstrap mode:

```yaml
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
spec:
  bootstrapMode: cloud-init  # Enables auto-detection
  # CNI will be automatically detected and configured
```

**Detection Process:**
1. Scans cluster for CNI-specific DaemonSets and ConfigMaps
2. Identifies CNI plugin from `kube-system` namespace resources
3. Configures node networking accordingly
4. Falls back to generic CNI configuration if detection fails

### CNI-Specific Configuration

#### Calico Configuration
- **Detection**: Looks for `calico-node` DaemonSet
- **Configuration**: Automatically configures IPPool and network policies
- **Features**: Full support for network policies and BGP routing

#### Cilium Configuration  
- **Detection**: Looks for `cilium` DaemonSet and ConfigMap
- **Configuration**: Configures eBPF datapath and cluster mesh
- **Features**: Advanced security policies and observability

#### Flannel Configuration
- **Detection**: Looks for `kube-flannel` DaemonSet
- **Configuration**: Sets up VXLAN overlay networking
- **Features**: Basic pod-to-pod communication

## Container Runtime Interface (CRI) Support

### Supported Container Runtimes

| Runtime        | Status             | Detection Method     | Default Version     |
|----------------|--------------------|----------------------|---------------------|
| **containerd** | ✅ Fully Supported | Auto-detected        | 1.6.12+             |
| **CRI-O**      | 🔄 Basic Support   | Manual configuration | 1.24+               |

### Runtime Auto-Detection

**Automatic Detection Process:**
1. Queries existing cluster nodes for runtime information
2. Reads `node.status.nodeInfo.containerRuntimeVersion`
3. Configures new nodes with the detected runtime
4. Falls back to containerd if detection fails

**Manual Runtime Configuration:**
```bash
# Set via environment variable
export CONTAINER_RUNTIME=containerd

# Or configure in bootstrap script
echo "CONTAINER_RUNTIME=containerd" >> /etc/environment
```

### Runtime-Specific Details

#### containerd (Recommended)
- **Default**: Yes, for most deployments
- **Features**: Full CRI compliance, optimal performance
- **Configuration**: Automatic setup with reasonable defaults

#### CRI-O
- **Use Case**: OpenShift and security-focused deployments
- **Setup**: Requires manual configuration in user data
- **Features**: OCI-compliant, security hardened

## Bootstrap Mode Compatibility

### Cloud-Init Bootstrap Mode
```yaml
spec:
  bootstrapMode: cloud-init
```
- ✅ **CNI**: Full auto-detection for all supported plugins
- ✅ **CRI**: Auto-detection and configuration
- ✅ **Flexibility**: Custom configurations via userData

### IKS API Bootstrap Mode
```yaml
spec:
  bootstrapMode: iks-api
  iksClusterID: your-cluster-id
```
- ✅ **CNI**: Uses IKS cluster's existing CNI configuration
- ✅ **CRI**: Inherits from IKS worker pool settings
- ✅ **Consistency**: Guaranteed compatibility with IKS cluster

### Auto Bootstrap Mode
```yaml
spec:
  bootstrapMode: auto
```
- ✅ **Smart Selection**: Chooses optimal bootstrap method
- ✅ **Fallback**: Graceful degradation if preferred method fails
- ✅ **Detection**: Automatic CNI/CRI detection in both modes

## 🔧 Custom Configuration

### Advanced CNI Configuration
```yaml
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
spec:
  bootstrapMode: cloud-init
  userData: |
    #!/bin/bash
    # Custom CNI configuration
    curl -L -o /opt/cni/bin/my-cni https://releases.example.com/cni
    chmod +x /opt/cni/bin/my-cni
    
    # Custom CNI config
    cat > /etc/cni/net.d/10-mycni.conf <<EOF
    {
      "cniVersion": "0.4.0",
      "name": "mycni",
      "type": "my-cni"
    }
    EOF
```

### Runtime Configuration Override
```yaml
spec:
  userData: |
    #!/bin/bash
    # Force specific container runtime
    export CONTAINER_RUNTIME=cri-o
    
    # Install CRI-O
    curl -L -o /tmp/crio.tar.gz https://releases.cri-o.io/...
    tar -xzf /tmp/crio.tar.gz -C /
    systemctl enable --now crio
```

## Unsupported / Experimental

### CNI Plugins
- ⚠️ **Weave Net**: Limited testing, may work with custom configuration
- ⚠️ **Antrea**: Experimental support, requires manual setup

### Container Runtimes
- ❌ **Kata Containers**: Not currently supported
- ❌ **gVisor**: Not currently supported

## Getting Help

### Request Support for Additional CNI/CRI

**Missing a CNI plugin or container runtime?** We'd love to help!

#### How to Request Support

1. **Open a GitHub Issue**: [Create a Feature Request](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/issues/new?template=feature_request.md)

2. **Provide Details**:
   ```
   **CNI/CRI Request**: [Plugin/Runtime Name]
   **Use Case**: [Why you need this]
   **IBM Cloud Context**: [IKS, self-managed, etc.]
   ```

3. **Include Configuration**:
   - Current CNI/CRI setup (if any)
   - Desired configuration
   - Any specific requirements

#### Community Contributions

We welcome community contributions for CNI/CRI support:

1. **Fork the Repository**
2. **Add Support** in `/pkg/providers/bootstrap/`
3. **Write Tests** for the new functionality  
4. **Submit a Pull Request** with documentation

#### Get Help

- **Slack**: Join `#karpenter-ibm-cloud` channel
- **Issues**: [Bug Reports](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/issues)

## Troubleshooting CNI/CRI Issues

### Common Issues

#### CNI Detection Failures
```bash
# Check CNI detection logs
kubectl logs -n karpenter deployment/karpenter-controller | grep "CNI"

# Verify CNI installation on existing nodes
kubectl get nodes -o jsonpath='{.items[*].status.nodeInfo.containerRuntimeVersion}'
```

#### Runtime Compatibility Issues
```bash
# Check runtime logs
journalctl -u containerd
journalctl -u crio

# Verify CRI socket
ls -la /var/run/containerd/containerd.sock
ls -la /var/run/crio/crio.sock
```

### Debug Bootstrap Process
```yaml
# Enable verbose bootstrap logging
spec:
  userData: |
    #!/bin/bash
    set -x  # Enable debug output
    export DEBUG=true
    
    # Your custom configuration
```

### Validation Commands
```bash
# Test CNI functionality
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: cni-test
spec:
  containers:
  - name: test
    image: busybox
    command: ["sleep", "300"]
EOF

# Check pod networking
kubectl exec cni-test -- ip addr show
kubectl exec cni-test -- ping -c 3 kubernetes.default.svc.cluster.local
```

## Additional Resources

- [Kubernetes CNI Documentation](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/network-plugins/)
- [CRI Interface Documentation](https://kubernetes.io/docs/concepts/architecture/cri/)
- [IBM Cloud Kubernetes Service Networking](https://cloud.ibm.com/docs/containers?topic=containers-cs_network_planning)
