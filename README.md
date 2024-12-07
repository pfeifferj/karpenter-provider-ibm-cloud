# Karpenter Provider for IBM Cloud

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
   helm repo add karpenter-ibm-cloud https://pfeifferj.github.io/karpenter-provider-ibm-cloud/index.yaml
   helm repo update
   ```

2. Install using Helm:
   ```bash
   helm install karpenter-ibm-cloud karpenter-ibm-cloud/karpenter-ibm-cloud \
     --namespace karpenter \
     --create-namespace \
     --set credentials.ibm_api_key=<your-ibm-api-key> \
     --set credentials.vpc_apikey=<your-vpc-api-key> \
     --set credentials.region=<your-region>
   ```

The provider uses the IBM Cloud API key to authenticate with various IBM Cloud services:

- For VPC operations, it uses the VPC API key directly
- For Global Catalog operations, it automatically generates and manages short-lived tokens using the IBM Cloud API key
  - Tokens are generated on-demand and cached
  - Tokens are automatically refreshed 5 minutes before expiry
  - This approach improves security by avoiding long-lived API keys

The VPC API endpoint URL is automatically constructed using your region (e.g., https://us-south.iaas.cloud.ibm.com/v1 for us-south region). You can override this by setting the `credentials.vpc_url` value if needed.

## Configuration

The provider can be configured through the following custom resources:

- `IBMCloudNodeClass`: Defines the configuration for nodes to be provisioned
- `NodePool`: Karpenter's core resource for defining node provisioning rules
- `NodeClaim`: Represents a request for a node with specific requirements

Example `IBMCloudNodeClass`:

```yaml
apiVersion: karpenter.ibm-cloud.sh/v1alpha1
kind: IBMCloudNodeClass
metadata:
  name: default
spec:
  region: us-south
  zone: us-south-1
  instanceProfile: bx2-2x8
```

## Development

### Testing and CI

The project includes automated testing and continuous integration workflows:

#### Helm Chart Testing

Pull requests that modify the Helm chart trigger automated tests that:

- Lint the chart for syntax and best practices
- Validate template rendering with test values
- Verify Kubernetes manifest validity
- Validate Custom Resource Definitions (CRDs)

#### Chart Publishing

When changes to the chart are merged to main:

- The chart is automatically packaged
- The Helm repository index is updated
- Changes are published to GitHub Pages

## Contributing

Contributions are welcome! Please read our [Contributing Guidelines](CONTRIBUTING.md) for details on how to submit pull requests.

## License

This project is licensed under the [Apache 2](LICENSE) License.
