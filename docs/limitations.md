# Current Limitations and Constraints

This document outlines the current limitations, constraints, and known issues with the Karpenter IBM Cloud Provider.

## IBM Cloud Platform Limitations

### Networking Constraints

#### Single Zone per NodeClass
- **Limitation**: Each IBMNodeClass can only specify one zone
- **Impact**: Cannot auto-balance across multiple zones in single NodeClass
- **Workaround**: Create multiple NodeClasses for different zones

```yaml
# Required: Separate NodeClass per zone
---
apiVersion: karpenter-ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: nodeclass-us-south-1
spec:
  zone: us-south-1
---
apiVersion: karpenter-ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: nodeclass-us-south-2
spec:
  zone: us-south-2
```

#### No Multi-Zone Auto-Placement
- **Status**: Not implemented
- **Impact**: Manual zone specification required

### Storage Limitations

#### Limited Storage Integration
- **Current**: Basic boot volume support only
- **Missing**:
  - Dynamic storage provisioning during node creation
  - Multiple storage attachments
  - Custom storage profiles
- **Workaround**: Configure storage post-provisioning via storage classes

#### No Instance Store Support
- **Status**: Not implemented
- **Impact**: Cannot use local NVMe storage
- **Alternative**: Use IBM Cloud Block Storage

## Provider-Specific Limitations

### Bootstrap Mode Limitations

#### VPC Launch Recovery

VPC launches store their original configuration and use a name derived from the cluster and NodeClaim UIDs. Retries adopt a matching instance after a lost response. An uncertain submission with no visible instance remains pending for 15 minutes instead of issuing another create request; after that the checkpoint is discarded and the claim relaunches under the same name. Restore access to the original account and region and investigate the launch checkpoint before intervening in the claim's finalizer.

New VPC launches resolve and record the account from the VPC API key. Helm's optional `credentials.accountId` checks that the credential belongs to the expected account. Credential changes to another account block recovery and deletion instead of treating that account's 404 as proof of absence.

Older claims record account identity after a successful lookup of their exact instance. If that instance is already absent and the claim has no recorded account, it remains quarantined until an operator verifies the original account and records its ID in the `karpenter-ibm.sh/account-id` annotation. Existing resources without immutable ownership tags are excluded from automatic orphan deletion. Retained volumes are not removed by launch rollback.

#### IKS Mode Instance Type Constraints {#iks-mode-instance-type-constraints}

IKS provisioning requires `iksDynamicPools.enabled: true`, an explicit zone and subnet, and capacity for one dedicated pool per NodeClaim. Shared pools are not resized. `instanceProfile` selects the flavor; an optional `iksWorkerPoolID` supplies a flavor template. The provider checks the chosen flavor against the NodeClaim requirements and completes launch once the worker exists, reporting the flavor's catalog capacity until the Node registers its own resources.

Allocations preserve their original account, cluster, pool, and worker identity. Ambiguous cloud responses retain their allocation finalizer until ownership and deletion can be verified. Existing pools without immutable cluster and NodeClass ownership are excluded from automatic cleanup.

See [IKS integration](iks-integration.md) for configuration and recovery behavior.

### Tagging and Metadata

#### Basic Tagging Support
- **Current**: Limited tag management
- **Missing**:
  - Tag propagation from NodePool to instances
  - Dynamic tag updates
  - Cost allocation tags

#### No Interruption Detection
- **Impact**: Cannot preemptively handle instance interruptions

### Networking Features

#### No Load Balancer Integration
- **Missing**: Automatic load balancer target registration
- **Impact**: Manual load balancer configuration required
- **Workaround**: Use Kubernetes ingress controllers

#### Limited Security Group Management
- **Current**: Uses default or specified security groups
- **Missing**: Dynamic security group creation and management
- **Workaround**: Pre-create security groups with required rules

## Integration Limitations

## Getting Help with Limitations

### Report New Limitations
If you encounter limitations not documented here:

1. **Check existing issues**: [GitHub Issues](https://github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/issues)
2. **Create new issue**
3. **Provide context**

### Request Feature Priority
To prioritize development of specific features:

1. **Upvote existing issues**: Show demand for features
2. **Comment with use case**: Explain business impact
3. **Contribute**: Submit PRs for high-priority features
