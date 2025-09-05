# Block Device Mapping

Block Device Mapping allows you to customize the storage configuration for instances provisioned by Karpenter. This feature provides fine-grained control over boot volumes and additional data volumes, similar to AWS Karpenter's block device mapping functionality.

## Overview

By default, Karpenter provisions instances with a 100GB general-purpose boot volume. With Block Device Mapping, you can:

- **Customize boot volume size and performance** (capacity, IOPS, bandwidth)
- **Add additional data volumes** with specific configurations
- **Configure encryption** using IBM Key Protect or Hyper Protect Crypto Services
- **Control volume lifecycle** (delete on termination or persist)
- **Apply custom tags** for organization and billing

## Configuration

Block device mappings are configured in the `IBMNodeClass` specification using the `blockDeviceMappings` field:

```yaml
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: custom-storage
spec:
  # ... other IBMNodeClass fields ...
  blockDeviceMappings:
    - rootVolume: true
      volumeSpec:
        capacity: 200
        profile: "10iops-tier"
    - deviceName: "data-storage"
      volumeSpec:
        capacity: 500
        profile: "5iops-tier"
```

### Field Reference

#### `BlockDeviceMapping`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `deviceName` | `string` | No | Custom name for the volume attachment. Auto-generated if not specified. |
| `volumeSpec` | `VolumeSpec` | No | Volume configuration. Uses defaults for root volumes if not specified. |
| `rootVolume` | `boolean` | No | Indicates if this is the boot/root volume. Only one mapping can have this set to `true`. |

#### `VolumeSpec`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `capacity` | `int64` | No | Volume size in GB. Range: 1-16000 GB. Boot volumes max: 250 GB. |
| `profile` | `string` | No | Storage profile. Options: `general-purpose`, `5iops-tier`, `10iops-tier`, `custom`. |
| `iops` | `int64` | No | I/O operations per second. Only valid for `custom` profile. Range: 100-64000. |
| `bandwidth` | `int64` | No | Maximum bandwidth in Mbps. Range: 1-16000. Auto-calculated if not specified. |
| `encryptionKeyID` | `string` | No | CRN of customer root key for encryption. Uses provider-managed encryption if not specified. |
| `deleteOnTermination` | `boolean` | No | Whether to delete volume when instance terminates. Default: `true`. |
| `tags` | `[]string` | No | User tags to apply to the volume. Maximum: 10 tags. |

## IBM Cloud Storage Profiles

IBM Cloud VPC offers different storage profiles optimized for various workloads:

### Standard Profiles

| Profile | Description | IOPS/GB | Max IOPS | Use Case |
|---------|-------------|---------|----------|----------|
| `general-purpose` | Balanced performance and cost | 3 | 16,000 | General workloads |
| `5iops-tier` | Higher performance | 5 | 48,000 | I/O intensive applications |
| `10iops-tier` | High performance | 10 | 48,000 | Database workloads |

### Custom Profile

| Profile | Description | IOPS Range | Bandwidth Range |
|---------|-------------|------------|-----------------|
| `custom` | User-defined performance | 100-64,000 | Varies by capacity and IOPS |

## Examples

### Basic Custom Boot Volume

```yaml
apiVersion: karpenter.ibm.sh/v1alpha1
kind: IBMNodeClass
metadata:
  name: large-boot-volume
spec:
  region: us-south
  instanceProfile: bx2-4x16
  image: ubuntu-24-04-amd64
  vpc: r010-12345678-1234-5678-9abc-def012345678
  subnet: 0717-197e06f4-b500-426c-bc0f-900b215f996c
  securityGroups:
    - r010-87654321-4321-8765-cba9-876543210fed
  
  blockDeviceMappings:
    - rootVolume: true
      volumeSpec:
        capacity: 200                    # 200GB boot volume
        profile: "10iops-tier"          # High performance
        deleteOnTermination: true       # Clean up on termination
```

### Boot Volume with Encryption

```yaml
blockDeviceMappings:
  - rootVolume: true
    volumeSpec:
      capacity: 150
      profile: "general-purpose"
      encryptionKeyID: "crn:v1:bluemix:public:kms:us-south:a/123456789:key:12345678-1234-5678-9abc-def012345678"
      tags:
        - "encrypted"
        - "production"
```

### Multiple Data Volumes

```yaml
blockDeviceMappings:
  # Custom boot volume
  - rootVolume: true
    volumeSpec:
      capacity: 100
      profile: "general-purpose"
  
  # Application storage
  - deviceName: "app-data"
    volumeSpec:
      capacity: 500
      profile: "5iops-tier"
      deleteOnTermination: false      # Persist data
      tags:
        - "application-storage"
  
  # Database storage with custom IOPS
  - deviceName: "database"
    volumeSpec:
      capacity: 1000
      profile: "custom"
      iops: 8000                      # Custom IOPS
      bandwidth: 400                  # Custom bandwidth
      deleteOnTermination: false
      encryptionKeyID: "crn:v1:bluemix:public:kms:..."
      tags:
        - "database"
        - "high-performance"
```

### High-Performance Database Configuration

```yaml
blockDeviceMappings:
  - rootVolume: true
    volumeSpec:
      capacity: 100
      profile: "general-purpose"
  
  - deviceName: "database-logs"
    volumeSpec:
      capacity: 200
      profile: "10iops-tier"          # Fast logs
      deleteOnTermination: false
  
  - deviceName: "database-data"
    volumeSpec:
      capacity: 2000
      profile: "custom"
      iops: 16000                     # Maximum IOPS
      bandwidth: 1000                 # Maximum bandwidth
      deleteOnTermination: false
      encryptionKeyID: "crn:v1:bluemix:public:kms:..."
```

## Default Behavior

When `blockDeviceMappings` is not specified or empty:

- **Boot Volume**: 100GB general-purpose storage
- **Delete on Termination**: `true`
- **Profile**: `general-purpose`
- **Encryption**: Provider-managed
- **Data Volumes**: None

This maintains backward compatibility with existing configurations.

## Best Practices

### Performance Optimization

1. **Boot Volumes**: Use `general-purpose` for most workloads, `10iops-tier` for I/O intensive applications
2. **Data Volumes**: Use `custom` profile for precise IOPS control when needed
3. **IOPS Planning**: Consider workload requirements and cost implications
4. **Bandwidth**: Let IBM Cloud auto-calculate unless you have specific requirements

### Security

1. **Encryption**: Use customer-managed keys for sensitive workloads
2. **Key Management**: Ensure proper IAM policies for Key Protect/HPCS access
3. **Tags**: Apply consistent tagging for security governance

### Cost Management

1. **Right-sizing**: Start with appropriate capacity and profile
2. **Lifecycle**: Set `deleteOnTermination: false` only for persistent data
3. **Monitoring**: Use IBM Cloud monitoring to track storage utilization
4. **Tags**: Apply cost center and project tags for billing allocation

### Operational

1. **Naming**: Use descriptive `deviceName` values for easier management
2. **Limits**: Stay within IBM Cloud quotas (10 block device mappings max)
3. **Testing**: Validate configurations in non-production environments first

## Troubleshooting

### Common Issues

#### Volume Creation Failures

```yaml
# Check capacity limits for profiles
Error: "volume capacity exceeds profile maximum"
```

**Solution**: Verify profile capacity limits in IBM Cloud documentation.

#### Encryption Key Access

```yaml
# Check IAM permissions
Error: "access denied to encryption key"
```

**Solution**: Ensure the service has `Reader` access to the Key Protect/HPCS key.

#### IOPS Configuration

```yaml
# IOPS only valid for custom profile
Error: "iops specified but profile is not custom"
```

**Solution**: Use `profile: "custom"` when specifying custom IOPS.

### Validation Errors

The IBM provider validates block device mappings:

- Only one `rootVolume: true` mapping allowed
- `capacity` must be within valid ranges
- `iops` only valid with `custom` profile
- `encryptionKeyID` must be valid CRN format

### Monitoring and Debugging

1. **Node Events**: Check Kubernetes events for provisioning issues
2. **IBM Cloud Console**: Verify volumes are created with correct configuration
3. **Karpenter Logs**: Look for block device mapping errors in controller logs
4. **Instance Details**: Validate volume attachments in VPC console

## Migration from Default Configuration

To migrate existing NodeClasses to use custom block device mappings:

### Step 1: Identify Current Configuration
```bash
# Check current default volume size
kubectl describe ibmnodeclass <name>
```

### Step 2: Create Equivalent Mapping
```yaml
# Replace default behavior with explicit mapping
blockDeviceMappings:
  - rootVolume: true
    volumeSpec:
      capacity: 100              # Current default
      profile: "general-purpose" # Current default
      deleteOnTermination: true  # Current default
```

### Step 3: Test and Gradually Enhance
```yaml
# Gradually add features
blockDeviceMappings:
  - rootVolume: true
    volumeSpec:
      capacity: 150              # Increased size
      profile: "5iops-tier"      # Better performance
      deleteOnTermination: true
      tags:
        - "migrated"
```

## Related Documentation

- [VPC Integration](vpc-integration.md) - VPC configuration and networking
- [Security Considerations](security-considerations.md) - Security best practices
- [Troubleshooting](troubleshooting.md) - General troubleshooting guide
- [IBM Cloud VPC Storage Profiles](https://cloud.ibm.com/docs/vpc?topic=vpc-block-storage-profiles) - Detailed profile information

## API Reference

For complete API specification, see the [IBMNodeClass API reference](../pkg/apis/v1alpha1/ibmnodeclass_types.go).