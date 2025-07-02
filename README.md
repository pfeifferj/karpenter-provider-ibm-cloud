# Karpenter Provider for IBM Cloud

[![Go Tests](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/actions/workflows/go-test.yaml/badge.svg)](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/actions/workflows/go-test.yaml)
[![codecov](https://codecov.io/github/pfeifferj/karpenter-provider-ibm-cloud/graph/badge.svg?token=VF3SOM6IMR)](https://codecov.io/github/pfeifferj/karpenter-provider-ibm-cloud)
[![Go Report Card](https://goreportcard.com/badge/github.com/pfeifferj/karpenter-provider-ibm-cloud)](https://goreportcard.com/report/github.com/pfeifferj/karpenter-provider-ibm-cloud)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![GitHub release (latest by date)](https://img.shields.io/github/v/release/pfeifferj/karpenter-provider-ibm-cloud)](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/releases)

This repository contains the Karpenter Provider implementation for IBM Cloud, enabling dynamic node provisioning in IBM Cloud Kubernetes clusters using Karpenter.

## Overview

Karpenter is an open-source node provisioning project for Kubernetes. This provider extends Karpenter's functionality to work with IBM Cloud, allowing for:

- Dynamic node provisioning in IBM Cloud VPC
- Instance type management and selection
- Automated scaling based on workload demands
- Integration with IBM Cloud APIs

## Prerequisites

Before installing the provider, ensure you have:

1. An IBM Cloud account with necessary permissions
2. IBM Cloud CLI installed
3. A Kubernetes cluster running on IBM Cloud
4. Required API keys:
   - IBM Cloud API key (for general IBM Cloud operations and Global Catalog access)
   - VPC API key (for VPC operations)

The provider uses the IBM Cloud API key to automatically generate short-lived tokens for Global Catalog API access, improving security by avoiding the need for a separate long-lived API key.

### Creating Required API Keys

Create an IBM Cloud API key:

```bash
ibmcloud iam api-key-create MyKey -d "Karpenter IBM Cloud Provider Key" --file key_file
```

## Installation

### Installing the Operator

1. Add the Helm repository:

   ```bash
   helm repo add karpenter-ibm-cloud <repository-url>
   helm repo update
   ```

2. Install using Helm:
   ```bash
   helm install karpenter karpenter-ibm-cloud/karpenter-ibm \
     --namespace karpenter \
     --create-namespace \
     --set credentials.ibm_api_key="<your-ibm-api-key>" \
     --set credentials.vpc_api_key="<your-vpc-api-key>" \
     --set credentials.region="<your-ibm-cloud-region>"
   ```

   Optional parameters for custom image:
   ```bash
   --set image.repository="<your-registry>/<your-image>" \
   --set image.tag="<your-tag>"
   ```

#### Example Installation

For reference, here's an example with the public repository:

```bash
# Add repository
helm repo add karpenter-ibm-cloud https://pfeifferj.github.io/karpenter-ibm-cloud
helm repo update

# Install with required credentials (replace with your actual values)
helm install karpenter karpenter-ibm-cloud/karpenter-ibm \
  --namespace karpenter \
  --create-namespace \
  --set credentials.ibm_api_key="your-actual-api-key-here" \
  --set credentials.vpc_api_key="your-actual-vpc-key-here" \
  --set credentials.region="your-region"
```

**Note**: Replace `your-region` with your IBM Cloud region (e.g., `us-south`, `eu-de`, `jp-tok`, etc.)

The provider uses the IBM Cloud API key to authenticate with various IBM Cloud services:

- For VPC operations, it uses the VPC API key directly
- For Global Catalog operations, it automatically generates and manages short-lived tokens using the IBM Cloud API key
  - Tokens are generated on-demand and cached
  - Tokens are automatically refreshed 5 minutes before expiry
  - This approach improves security by avoiding long-lived API keys

The VPC API endpoint URL is automatically constructed using your region (e.g., https://us-south.iaas.cloud.ibm.com/v1 for us-south region). You can override this by setting the `credentials.vpc_url` value if needed.

## Configuration

The provider can be configured through the following custom resources:

- `IBMNodeClass`: Defines the configuration for nodes to be provisioned
- `NodePool`: Karpenter's core resource for defining node provisioning rules
- `NodeClaim`: Represents a request for a node with specific requirements

### Example IBMNodeClass

```yaml
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: auto-placement
spec:
  region: us-south
  vpc: "123e4567-e89b-12d3-a456-426614174000"
  zone: us-south-1
  subnet: "123e4567-e89b-12d3-a456-426614174000"
  instanceRequirements:
    architecture: amd64
    minimumCPU: 2
    minimumMemory: 4
  placementStrategy:
    zoneBalance: Balanced
    subnetSelection:
      minimumAvailableIPs: 10
  image: ibm-ubuntu-24-04-6-minimal-amd64-2
```

### Example NodePool

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: default
spec:
  template:
    spec:
      nodeClassRef:
        group: karpenter.ibm.sh
        kind: IBMNodeClass
        name: auto-placement
      requirements:
        - key: "kubernetes.io/arch"
          operator: In
          values: ["amd64"]
        - key: "kubernetes.io/os"
          operator: In
          values: ["linux"]
        - key: "monitoring"
          operator: In
          values: ["true"]
  disruption:
    consolidateAfter: 30s
    consolidationPolicy: WhenEmpty
  limits:
    cpu: "1000"
  weight: 100
```

### Advanced Configuration Options

The Helm chart supports several advanced configuration options:

#### Logging Configuration

```yaml
logLevel: "debug" # Options: debug, info, warn, error
verboseLogLevel: "5" # Verbosity level (1-5)
debug: "true" # Enable debug mode
controllerVerbosity: "4" # Controller manager verbosity (0-5)
controllerLogLevel: "debug" # Controller manager log level
```

#### Metrics Configuration

```yaml
metrics:
  serviceMonitor:
    enabled: false # Enable ServiceMonitor creation
    additionalLabels: {} # Additional labels for ServiceMonitor
    endpointConfig: {} # Additional endpoint configuration
```

#### Resource Management

```yaml
resources:
  limits:
    cpu: 100m
    memory: 128Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

## Development

### Testing and CI

The project includes automated testing and continuous integration workflows:

#### Helm Chart Testing

Tests run automatically on:

- All pull requests (validates changes before merge)
- Manual trigger via GitHub Actions UI

The tests perform:

- Chart linting for syntax and best practices
- Template rendering validation
- Kubernetes manifest validation
- Custom Resource Definition (CRD) verification

#### Chart Publishing

After changes pass tests and are merged to main:

- The chart is automatically packaged
- The Helm repository index is updated
- Changes are published to GitHub Pages

These CI workflows ensure chart quality through pre-merge validation and maintain the Helm repository for easy installation.

## Contributing

Contributions are welcome! Please read our [Contributing Guidelines](CONTRIBUTING.md) for details on how to submit pull requests.

## License

This project is licensed under the [Apache 2](LICENSE) License.
