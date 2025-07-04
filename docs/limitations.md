# Current Limitations and Constraints

This document outlines the current limitations, constraints, and known issues with the Karpenter IBM Cloud Provider.

## IBM Cloud Platform Limitations

### Instance Types and Capacity

#### No Spot Instance Support

#### Limited Instance Type Availability

```yaml
# Recommended: Multi-zone placement strategy
spec:
  placementStrategy:
    zoneBalance: AvailabilityFirst
```

### Networking Constraints

#### Single Zone per NodeClass
- **Limitation**: Each IBMNodeClass can only specify one zone
- **Impact**: Cannot auto-balance across multiple zones in single NodeClass
- **Workaround**: Create multiple NodeClasses for different zones

```yaml
# Required: Separate NodeClass per zone
---
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: nodeclass-us-south-1
spec:
  zone: us-south-1
---
apiVersion: karpenter.ibm.sh/v1alpha1
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

## Scale and Performance Constraints

### Node Provisioning Limits

#### API Rate Limits
- **IBM Cloud VPC API**: Standard rate limits apply
- **Impact**: May slow provisioning during high-scale events
- **Mitigation**: Built-in retry logic and exponential backoff
- **Monitoring**: Check controller logs for rate limit errors

#### Concurrent Provisioning
- **Current**: Limited by IBM Cloud API quotas
- **Recommendation**: Monitor quota usage in IBM Cloud console
- **Scaling**: Contact IBM Cloud support for quota increases

### Resource Quotas

#### VPC Resource Limits
- **Instances per VPC**: Subject to IBM Cloud quotas
- **Security Groups**: Limited per VPC
- **Subnets**: Limited per VPC
- **Monitoring**: Track usage via IBM Cloud monitoring

## Integration Limitations

### Kubernetes Integration

#### **Limited Admission Controller Support**
- **Current**: Basic validation only
- **Missing**: Advanced admission controller features
- **Impact**: Complex validation rules not supported

#### **Custom Resource Validation**
- **CEL Validation**: Basic implementation
- **Missing**: Complex cross-field validation
- **Workaround**: Additional validation in controllers

### Monitoring and Observability

#### **Limited Metrics**
- **Available**: Basic provisioning metrics
- **Missing**:
  - Detailed cost metrics
  - Advanced performance metrics
  - IBM Cloud specific metrics
- **Roadmap**: Enhanced metrics in future releases

## Getting Help with Limitations

### Report New Limitations
If you encounter limitations not documented here:

1. **Check existing issues**: [GitHub Issues](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/issues)
2. **Create new issue**
3. **Provide context**

### Request Feature Priority
To prioritize development of specific features:

1. **Upvote existing issues**: Show demand for features
2. **Comment with use case**: Explain business impact
3. **Contribute**: Submit PRs for high-priority features
