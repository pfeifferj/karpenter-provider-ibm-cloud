# Karpenter Provider for IBM Cloud

[![Go Tests](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/actions/workflows/go-test.yaml/badge.svg)](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/actions/workflows/go-test.yaml)
[![codecov](https://codecov.io/github/pfeifferj/karpenter-provider-ibm-cloud/graph/badge.svg?token=VF3SOM6IMR)](https://codecov.io/github/pfeifferj/karpenter-provider-ibm-cloud)
[![Go Report Card](https://goreportcard.com/badge/github.com/pfeifferj/karpenter-provider-ibm-cloud)](https://goreportcard.com/report/github.com/pfeifferj/karpenter-provider-ibm-cloud)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![GitHub release (latest by date)](https://img.shields.io/github/v/release/pfeifferj/karpenter-provider-ibm-cloud)](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/releases)
[![Documentation](https://img.shields.io/badge/docs-latest-blue.svg)](https://pfeifferj.github.io/karpenter-provider-ibm-cloud/docs/)

This repository contains the Karpenter Provider implementation for IBM Cloud, enabling dynamic node provisioning in IBM Cloud Kubernetes clusters using Karpenter.

**[View Full Documentation](https://pfeifferj.github.io/karpenter-provider-ibm-cloud/docs/)** | 🚀 **[Getting Started Guide](https://pfeifferj.github.io/karpenter-provider-ibm-cloud/docs/getting-started/)**

## Overview

Karpenter is an open-source node provisioning project for Kubernetes. This provider extends Karpenter's functionality to work with IBM Cloud, allowing for:

- Dynamic node provisioning in IBM Cloud VPC
- Instance type management and selection
- Automated scaling based on workload demands
- Integration with IBM Cloud APIs

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

## Getting Help

### Community Support

- 💬 **[Join our Slack](https://cloud-native.slack.com/archives/C094SDPCVLN)** - Get help from the community and maintainers
- 🐛 **[Report Issues](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/issues)** - Found a bug or have a feature request?
- 📖 **[Documentation](https://pfeifferj.github.io/karpenter-provider-ibm-cloud/docs/)** - Complete guides and API reference

## Contributing

Contributions are welcome! Please read our [Contributing Guidelines](CONTRIBUTING.md) for details on how to submit pull requests.

## License

This project is licensed under the [Apache 2](LICENSE) License.
