# karpenter-provider-ibm-cloud

[![CI](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/actions/workflows/ci.yaml/badge.svg?branch=main)](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/actions/workflows/ci.yaml)
![GitHub stars](https://img.shields.io/github/stars/pfeifferj/karpenter-provider-ibm-cloud)
![GitHub forks](https://img.shields.io/github/forks/pfeifferj/karpenter-provider-ibm-cloud)
[![GitHub License](https://img.shields.io/badge/License-Apache%202.0-ff69b4.svg)](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/blob/main/LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/pfeifferj/karpenter-provider-pfeifferj)](https://goreportcard.com/report/github.com/pfeifferj/karpenter-provider-ibm-cloud)
[![Coverage Status](https://coveralls.io/repos/github/pfeifferj/karpenter-provider-ibm-cloud/badge.svg?branch=main)](https://coveralls.io/github/pfeifferj/karpenter-provider-ibm-cloud?branch=main)
[![contributions welcome](https://img.shields.io/badge/contributions-welcome-brightgreen.svg?style=flat)](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/issues)

![](website/static/banner.png)

Karpenter is an open-source node provisioning project built for Kubernetes.
Karpenter improves the efficiency and cost of running workloads on Kubernetes clusters by:

- **Watching** for pods that the Kubernetes scheduler has marked as unschedulable
- **Evaluating** scheduling constraints (resource requests, nodeselectors, affinities, tolerations, and topology spread constraints) requested by the pods
- **Provisioning** nodes that meet the requirements of the pods
- **Removing** the nodes when the nodes are no longer needed

Come discuss Karpenter in the [#karpenter](https://kubernetes.slack.com/archives/C02SFFZSA2K) channel, in the [Kubernetes slack](https://slack.k8s.io/) or join the [Karpenter working group](https://karpenter.sh/docs/contributing/working-group/) bi-weekly calls. If you want to contribute to the Karpenter project, please refer to the Karpenter docs.

Check out the [Docs](https://karpenter.sh/docs/) to learn more.
