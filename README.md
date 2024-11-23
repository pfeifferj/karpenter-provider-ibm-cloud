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
   - IBM Cloud API key (for general IBM Cloud operations)
   - IAM API key (for VPC and Global Catalog APIs)

### Creating Required API Keys

1. Create an IBM Cloud API key:

   ```bash
   ibmcloud iam api-key-create MyKey -d "Karpenter IBM Cloud Provider Key" --file key_file
   ```

2. Set up the required environment variables:
   ```bash
   export VPC_URL=https://us-south.iaas.cloud.ibm.com/v1
   export VPC_AUTH_TYPE=iam
   export GLOBAL_CATALOG_AUTH_TYPE=iam
   export VPC_APIKEY=<your-vpc-api-key>
   export GLOBAL_CATALOG_APIKEY=<your-global-catalog-api-key>
   export IBM_CLOUD_API_KEY=<your-ibm-cloud-api-key>
   ```

## Installation

### Installing the Operator

1. Add the Helm repository:

   ```bash
   helm repo add karpenter-ibm-cloud https://[repository-url]
   helm repo update
   ```

2. Install using Helm:
   ```bash
   helm install karpenter-ibm-cloud karpenter-ibm-cloud/karpenter-ibm-cloud \
     --namespace karpenter \
     --create-namespace \
     --set ibmCloud.apiKey=<your-api-key>
   ```

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

## Generating Instance Types

To update the instance types catalog:

1. Set up the required environment variables as shown in the Prerequisites section
2. Run the generation tool:
   ```bash
   go run tools/gen_instance_types.go
   ```

## Contributing

Contributions are welcome! Please read our [Contributing Guidelines](CONTRIBUTING.md) for details on how to submit pull requests.

## License

This project is licensed under the [Apache 2](LICENSE) License.
