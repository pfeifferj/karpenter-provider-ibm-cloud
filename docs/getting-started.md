# Quick Start Guide

Get up and running with Karpenter IBM Cloud Provider!

## Prerequisites

- IBM Cloud account with VPC access
- Kubernetes cluster (IKS or self-managed)
- `kubectl` configured for your cluster
- IBM Cloud CLI installed

## Required IBM Cloud Resources

Before installing Karpenter, ensure you have:

### VPC Infrastructure
```bash
# List your VPCs
ibmcloud is vpcs

# List subnets in your VPC
ibmcloud is subnets --vpc your-vpc-id

# List security groups
ibmcloud is security-groups --vpc your-vpc-id
```

### API Keys
```bash
# Create IBM Cloud API key
ibmcloud iam api-key-create karpenter-key --description "Karpenter IBM Cloud Provider"

# Create VPC-specific API key  
ibmcloud iam api-key-create karpenter-vpc-key --description "Karpenter VPC Access"
```

### Resource IDs
Collect the following information:
- **VPC ID**: `r006-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`
- **Subnet ID(s)**: `0717-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`
- **Security Group ID(s)**: `r006-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`
- **Image ID**: Use `ubuntu-20-04` or specific image ID
- **Region**: e.g., `us-south`

## Quick Installation

### Step 1: Install Karpenter
```bash
# Add Helm repository
helm repo add karpenter-ibm https://pfeifferj.github.io/karpenter-provider-ibm-cloud
helm repo update

# Install Karpenter with IBM Cloud Provider
helm install karpenter karpenter-ibm/karpenter \
  --namespace karpenter \
  --create-namespace \
  --set controller.env.IBM_API_KEY="your-ibm-api-key" \
  --set controller.env.VPC_API_KEY="your-vpc-api-key" \
  --set controller.env.IBM_REGION="us-south"
```

### Step 2: Create NodeClass
```bash
cat <<EOF | kubectl apply -f -
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: default-nodeclass
spec:
  region: us-south
  vpc: r006-your-vpc-id
  image: ubuntu-20-04
  securityGroups:
    - r006-your-security-group-id
  subnet: 0717-your-subnet-id
  bootstrapMode: auto
EOF
```

### Step 3: Create NodePool
```bash
cat <<EOF | kubectl apply -f -
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: default-nodepool
spec:
  template:
    metadata:
      labels:
        node-type: karpenter-provisioned
    spec:
      nodeClassRef:
        apiVersion: karpenter.ibm.sh/v1alpha1
        kind: IBMNodeClass
        name: default-nodeclass
      requirements:
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["bx2-2x8", "bx2-4x16", "mx2-2x16"]
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
  limits:
    cpu: 1000
  disruption:
    consolidationPolicy: WhenEmpty
    consolidateAfter: 30s
EOF
```

### Step 4: Test Auto-Scaling
```bash
# Deploy a test workload
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-workload
spec:
  replicas: 5
  selector:
    matchLabels:
      app: test-workload
  template:
    metadata:
      labels:
        app: test-workload
    spec:
      containers:
      - name: test
        image: busybox
        command: ["sleep", "3600"]
        resources:
          requests:
            cpu: 4
            memory: 12Gi
EOF
```

## Common Configurations

### For IKS Clusters
```yaml
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: iks-nodeclass
spec:
  region: us-south
  vpc: r006-your-vpc-id
  image: ubuntu-20-04
  bootstrapMode: iks-api
  iksClusterID: your-iks-cluster-id
```

### For Custom Container Runtime
```yaml
spec:
  userData: |
    #!/bin/bash
    export CONTAINER_RUNTIME=crio
```

### For Specific CNI
```yaml
spec:
  userData: |
    #!/bin/bash
    export CNI_PLUGIN=cilium
```

## Troubleshooting Quick Fixes

### NodeClass Not Ready
```bash
# Check validation errors
kubectl describe ibmnodeclass default-nodeclass

# Common issues:
# - Invalid VPC/subnet IDs
# - Incorrect region
# - Missing API key permissions
```

### Nodes Not Provisioning
```bash
# Check NodeClaim status
kubectl get nodeclaims -o wide

# Check controller logs for errors
kubectl logs -n karpenter deployment/karpenter-controller --tail=50
```

### Pod Still Pending
```bash
# Verify pod resource requirements match instance types
kubectl describe pod your-pending-pod

# Check NodePool limits
kubectl describe nodepool default-nodepool
```
