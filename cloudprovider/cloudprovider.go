package ibmcloud

import (
    "context"
    "fmt"
    "strings"

    "github.com/pfeifferj/cloud-provider-ibm/pkg/ibm"
    "github.com/awslabs/operatorpkg/status"
    "github.com/samber/lo"
    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	coreapis "sigs.k8s.io/karpenter/pkg/apis"
    v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"
	"sigs.k8s.io/karpenter/pkg/scheduling"
	"sigs.k8s.io/karpenter/pkg/utils/resources"

	"github.com/pfeifferj/karpenter-ibm-cloud/pkg/apis"
	"github.com/pfeifferj/karpenter-ibm-cloud/pkg/apis/v1alpha1"
	cloudproviderevents "github.com/pfeifferj/karpenter-ibm-cloud/pkg/cloudprovider/events"
	"github.com/pfeifferj/karpenter-ibm-cloud/pkg/providers/instance"
	"github.com/pfeifferj/karpenter-ibm-cloud/pkg/providers/instancetype"
)

const CloudProviderName = "ibmcloud"
const ibmProviderPrefix = "ibmcloud://"

func NewCloudProvider(ctx context.Context, kubeClient client.Client, instanceTypes []*cloudprovider.InstanceType) *CloudProvider {
    ibmClient, err := ibm.NewClient()
    if err != nil {
        // Handle initialization error
    }
    return &CloudProvider{
        kubeClient:    kubeClient,
        ibmClient:     ibmClient,
        instanceTypes: instanceTypes,
    }
}

type CloudProvider struct {
    kubeClient    client.Client
    ibmClient     *ibm.Client
    instanceTypes []*cloudprovider.InstanceType
}

func (c *CloudProvider) Create(ctx context.Context, nodeClaim *v1.NodeClaim) (*v1.NodeClaim, error) {
    // Use IBM Cloud SDK to create a new instance
    instanceID, err := c.ibmClient.CreateInstance(ctx, nodeClaim)
    if err != nil {
        return nil, fmt.Errorf("creating instance in IBM Cloud: %w", err)
    }

    // Create the Node object in Kubernetes
    node, err := c.toNode(nodeClaim, instanceID)
    if err != nil {
        return nil, fmt.Errorf("converting to node: %w", err)
    }
    if err := c.kubeClient.Create(ctx, node); err != nil {
        return nil, fmt.Errorf("creating node in Kubernetes: %w", err)
    }

    // Update NodeClaim status
    nodeClaim.Status.ProviderID = node.Spec.ProviderID
    nodeClaim.Status.NodeName = node.Name

    return nodeClaim, nil
}

func (c *CloudProvider) Delete(ctx context.Context, nodeClaim *v1.NodeClaim) error {
    instanceID := strings.TrimPrefix(nodeClaim.Status.ProviderID, ibmProviderPrefix)
    if err := c.ibmClient.DeleteInstance(ctx, instanceID); err != nil {
        if errors.IsNotFound(err) {
            return cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("instance not found in IBM Cloud: %w", err))
        }
        return fmt.Errorf("deleting instance in IBM Cloud: %w", err)
    }
    return nil
}

func (c *CloudProvider) Get(ctx context.Context, providerID string) (*v1.NodeClaim, error) {
    instanceID := strings.TrimPrefix(providerID, ibmProviderPrefix)

    instance, err := c.ibmClient.GetInstance(ctx, instanceID)
    if err != nil {
        if errors.IsNotFound(err) {
            return nil, cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("instance not found in IBM Cloud: %w", err))
        }
        return nil, fmt.Errorf("getting instance from IBM Cloud: %w", err)
    }

    nodeClaim, err := c.instanceToNodeClaim(instance)
    if err != nil {
        return nil, fmt.Errorf("converting instance to NodeClaim: %w", err)
    }
    return nodeClaim, nil
}

func (c *CloudProvider) List(ctx context.Context) ([]*v1.NodeClaim, error) {
    instances, err := c.ibmClient.ListInstances(ctx)
    if err != nil {
        return nil, fmt.Errorf("listing instances from IBM Cloud: %w", err)
    }

    var nodeClaims []*v1.NodeClaim
    for _, instance := range instances {
        nodeClaim, err := c.instanceToNodeClaim(instance)
        if err != nil {
            return nil, fmt.Errorf("converting instance to NodeClaim: %w", err)
        }
        nodeClaims = append(nodeClaims, nodeClaim)
    }
    return nodeClaims, nil
}

func (c *CloudProvider) GetInstanceTypes(ctx context.Context, nodePool *v1.NodePool) ([]*cloudprovider.InstanceType, error) {
    ibmInstanceTypes, err := c.ibmClient.ListInstanceTypes(ctx)
    if err != nil {
        return nil, fmt.Errorf("listing instance types from IBM Cloud: %w", err)
    }

    var instanceTypes []*cloudprovider.InstanceType
    for _, ibmType := range ibmInstanceTypes {
        instanceType := &cloudprovider.InstanceType{
            Name: ibmType.Name,
            // Map other fields as necessary
        }
        instanceTypes = append(instanceTypes, instanceType)
    }
    return instanceTypes, nil
}

func (c *CloudProvider) DisruptionReasons() []v1.DisruptionReason {
    // Implement IBM Cloud-specific disruption reasons if any
    return []v1.DisruptionReason{}
}

func (c *CloudProvider) IsDrifted(ctx context.Context, nodeClaim *v1.NodeClaim) (cloudprovider.DriftReason, error) {
    // Implement drift detection logic based on IBM Cloud specifics
    return "", nil
}

func (c *CloudProvider) Name() string {
    return "ibmcloud"
}

func (c *CloudProvider) GetSupportedNodeClasses() []status.Object {
    // Return IBM Cloud-specific NodeClass
    return []status.Object{&v1alpha1.IBMCloudNodeClass{}}
}

func (c *CloudProvider) toNode(nodeClaim *v1.NodeClaim, instanceID string) (*corev1.Node, error) {
    instanceTypeName := nodeClaim.Spec.NodeClassRef.Name // Adjust as per your NodeClassRef
    instanceType, err := c.getInstanceType(instanceTypeName)
    if err != nil {
        return nil, fmt.Errorf("instance type %s not found: %w", instanceTypeName, err)
    }

    labels := map[string]string{
        corev1.LabelInstanceTypeStable: instanceType.Name,
        // Add other labels as needed
    }
    for k, v := range nodeClaim.Labels {
        labels[k] = v
    }

    node := &corev1.Node{
        ObjectMeta: metav1.ObjectMeta{
            Name:        instanceID,
            Labels:      labels,
            Annotations: nodeClaim.Annotations,
        },
        Spec: corev1.NodeSpec{
            ProviderID: ibmProviderPrefix + instanceID,
        },
        Status: corev1.NodeStatus{
            Capacity:    instanceType.Capacity,
            Allocatable: instanceType.Allocatable(),
        },
    }
    return node, nil
}

func (c *CloudProvider) getInstanceType(name string) (*cloudprovider.InstanceType, error) {
    it, found := lo.Find(c.instanceTypes, func(it *cloudprovider.InstanceType) bool {
        return it.Name == name
    })
    if !found {
        return nil, fmt.Errorf("instance type %q not found", name)
    }
    return it, nil
}

func (c *CloudProvider) instanceToNodeClaim(instance *ibm.Instance) (*v1.NodeClaim, error) {
    // Map IBM Cloud instance to Karpenter's NodeClaim
    nodeClaim := &v1.NodeClaim{
        ObjectMeta: metav1.ObjectMeta{
            Name: instance.ID,
            Labels: map[string]string{
                corev1.LabelInstanceTypeStable: instance.Type,
                // Add other labels as necessary
            },
        },
        Spec: v1.NodeClaimSpec{
            // Populate spec fields as needed
        },
        Status: v1.NodeClaimStatus{
            ProviderID: ibmProviderPrefix + instance.ID,
            // Populate status fields as needed
        },
    }
    return nodeClaim, nil
}