# Troubleshooting Guide

This guide helps diagnose and resolve common issues with the Karpenter IBM Cloud Provider.

## Quick Diagnostics

### Check Controller Status
```bash
# Check if Karpenter controller is running
kubectl get pods -n karpenter

# Check controller logs
kubectl logs -n karpenter deployment/karpenter -f

# Check controller startup messages
kubectl logs -n karpenter deployment/karpenter | grep "Starting Controller"
```

## Common Issues

### Authentication Issues

!!! failure "Failed to authenticate with IBM Cloud API"
    ```
    Failed to create VPC client: authentication failed
    Error: {"errorMessage":"Unauthorized","errorCode":"401"}
    ```

    **Solution:**
    
    1. Verify API keys are correctly set
    2. Check Service ID permissions
    3. Update Kubernetes secret:
    ```bash
    kubectl create secret generic karpenter-ibm-credentials \
      --from-literal=api-key="$IBM_API_KEY" \
      --from-literal=vpc-api-key="$VPC_API_KEY" \
      --namespace karpenter --dry-run=client -o yaml | kubectl apply -f -
    ```

### Instance Provisioning Issues

!!! warning "No suitable subnets found"
    **Diagnosis:**
    ```bash
    # Check available subnets
    ibmcloud is subnets --output json
    
    # Check subnet capacity
    ibmcloud is subnet SUBNET_ID --output json
    ```
    
    **Solutions:**
    
    - Verify subnet exists in specified zone
    - Ensure subnet has available IP addresses
    - Consider using auto-subnet selection

### Node Registration Issues

!!! error "Node failed to join cluster"
    **Common causes:**
    
    - API server endpoint issues
    - Bootstrap token expiration
    - Network connectivity problems
    
    **Debug steps:**
    ```bash
    # Check bootstrap logs on instance
    ssh ubuntu@INSTANCE_IP "sudo journalctl -u cloud-final"
    
    # Check kubelet status
    ssh ubuntu@INSTANCE_IP "sudo systemctl status kubelet"
    ```

## Debug Mode

Enable debug logging for detailed information:

```yaml
apiVersion: v1
kind: Deployment
metadata:
  name: karpenter
  namespace: karpenter
spec:
  template:
    spec:
      containers:
      - name: controller
        env:
        - name: KARPENTER_LOG_LEVEL
          value: debug
```

## Getting Help

- [GitHub Issues](https://github.com/pfeifferj/karpenter-provider-ibm-cloud/issues)