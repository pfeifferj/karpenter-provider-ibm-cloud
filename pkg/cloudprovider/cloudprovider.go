package cloudprovider

import (
	"context"
	"fmt"

	"github.com/awslabs/operatorpkg/status"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"
	"sigs.k8s.io/karpenter/pkg/scheduling"
	"sigs.k8s.io/karpenter/pkg/utils/resources"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	ibmevents "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/events"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
)

const CloudProviderName = "ibmcloud"

var _ cloudprovider.CloudProvider = (*CloudProvider)(nil)

type CloudProvider struct {
	kubeClient client.Client
	recorder   events.Recorder

	instanceTypeProvider instancetype.Provider
	instanceProvider     instance.Provider
}

func New(kubeClient client.Client,
	recorder events.Recorder,
	instanceTypeProvider instancetype.Provider,
	instanceProvider instance.Provider) *CloudProvider {
	return &CloudProvider{
		kubeClient: kubeClient,
		recorder:   recorder,

		instanceTypeProvider: instanceTypeProvider,
		instanceProvider:     instanceProvider,
	}
}

func (c *CloudProvider) Get(ctx context.Context, providerID string) (*karpv1.NodeClaim, error) {
	log := log.FromContext(ctx).WithValues("providerID", providerID)
	log.Info("Getting instance details")

	node := &corev1.Node{
		Spec: corev1.NodeSpec{
			ProviderID: providerID,
		},
	}

	instance, err := c.instanceProvider.GetInstance(ctx, node)
	if err != nil {
		log.Error(err, "Failed to get instance")
		return nil, fmt.Errorf("getting instance, %w", err)
	}
	log.Info("Found instance", "type", instance.Type, "zone", instance.Zone)

	instanceTypes, err := c.instanceTypeProvider.List(ctx)
	if err != nil {
		log.Error(err, "Failed to list instance types")
		return nil, fmt.Errorf("listing instance types, %w", err)
	}

	instanceType, _ := lo.Find(instanceTypes, func(i *cloudprovider.InstanceType) bool {
		return i.Name == instance.Type
	})
	if instanceType != nil {
		log.Info("Resolved instance type", "type", instanceType.Name)
	}

	nc := &karpv1.NodeClaim{
		Status: karpv1.NodeClaimStatus{
			ProviderID: providerID,
		},
	}

	if instanceType != nil {
		nc.Status.Capacity = instanceType.Capacity
		nc.Status.Allocatable = instanceType.Allocatable()
	}

	log.Info("Successfully retrieved NodeClaim",
		"providerID", nc.Status.ProviderID,
		"capacity", nc.Status.Capacity,
		"allocatable", nc.Status.Allocatable)
	return nc, nil
}

func (c *CloudProvider) List(ctx context.Context) ([]*karpv1.NodeClaim, error) {
	log := log.FromContext(ctx)
	log.Info("Listing all instances")

	// Get all nodes from the Kubernetes API
	nodeList := &corev1.NodeList{}
	if err := c.kubeClient.List(ctx, nodeList); err != nil {
		return nil, fmt.Errorf("listing nodes, %w", err)
	}

	var nodeClaims []*karpv1.NodeClaim
	for _, node := range nodeList.Items {
		if node.Spec.ProviderID == "" {
			continue
		}

		_, err := c.instanceProvider.GetInstance(ctx, &node)
		if err != nil {
			log.Error(err, "Failed to get instance details", "node", node.Name)
			continue
		}

		nc := &karpv1.NodeClaim{
			Status: karpv1.NodeClaimStatus{
				ProviderID: node.Spec.ProviderID,
			},
		}
		nodeClaims = append(nodeClaims, nc)
	}

	return nodeClaims, nil
}

func (c *CloudProvider) Create(ctx context.Context, nodeClaim *karpv1.NodeClaim) (*karpv1.NodeClaim, error) {
	log := log.FromContext(ctx).WithValues(
		"nodeClaim", nodeClaim.Name,
		"requirements", nodeClaim.Spec.Requirements,
		"resources", nodeClaim.Spec.Resources)
	log.Info("Starting node creation")

	nodeClass := &v1alpha1.IBMNodeClass{}
	if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaim.Spec.NodeClassRef.Name}, nodeClass); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "Failed to resolve NodeClass")
			c.recorder.Publish(ibmevents.NodeClaimFailedToResolveNodeClass(nodeClaim))
		}
		return nil, cloudprovider.NewInsufficientCapacityError(fmt.Errorf("resolving node class, %w", err))
	}
	log.Info("Resolved NodeClass", "nodeClass", nodeClass.Name)

	// Check if the Ready condition exists and its status
	readyCondition := metav1.Condition{
		Type:   "Ready",
		Status: metav1.ConditionUnknown,
	}

	for _, condition := range nodeClass.Status.Conditions {
		if condition.Type == "Ready" {
			readyCondition = condition
			break
		}
	}

	if readyCondition.Status == metav1.ConditionFalse {
		log.Error(fmt.Errorf("%s", readyCondition.Message), "NodeClass not ready")
		return nil, cloudprovider.NewNodeClassNotReadyError(fmt.Errorf("%s", readyCondition.Message))
	}
	if readyCondition.Status == metav1.ConditionUnknown {
		log.Error(fmt.Errorf("%s", readyCondition.Message), "NodeClass readiness unknown")
		return nil, fmt.Errorf("resolving NodeClass readiness, NodeClass is in Ready=Unknown, %s", readyCondition.Message)
	}

	log.Info("Resolving instance types")
	instanceTypes, err := c.instanceTypeProvider.List(ctx)
	if err != nil {
		log.Error(err, "Failed to resolve instance types")
		return nil, fmt.Errorf("resolving instance types, %w", err)
	}

	reqs := scheduling.NewNodeSelectorRequirementsWithMinValues(nodeClaim.Spec.Requirements...)
	compatible := lo.Filter(instanceTypes, func(i *cloudprovider.InstanceType, _ int) bool {
		reqErr := reqs.Compatible(i.Requirements, scheduling.AllowUndefinedWellKnownLabels)
		offeringsCompatible := len(i.Offerings.Compatible(reqs).Available()) > 0
		resourcesFit := resources.Fits(nodeClaim.Spec.Resources.Requests, i.Allocatable())

		if reqErr != nil {
			log.Info("Instance type incompatible with requirements",
				"type", i.Name,
				"error", reqErr)
			return false
		}
		if !offeringsCompatible {
			log.Info("No compatible offerings available",
				"type", i.Name)
			return false
		}
		if !resourcesFit {
			log.Info("Resources don't fit",
				"type", i.Name,
				"requested", nodeClaim.Spec.Resources.Requests,
				"allocatable", i.Allocatable())
			return false
		}
		return true
	})

	if len(compatible) == 0 {
		log.Error(nil, "No compatible instance types found")
		return nil, cloudprovider.NewInsufficientCapacityError(fmt.Errorf("all requested instance types were unavailable during launch"))
	}
	log.Info("Found compatible instance types", "count", len(compatible), "types", lo.Map(compatible, func(it *cloudprovider.InstanceType, _ int) string { return it.Name }))

	log.Info("Creating instance")
	node, err := c.instanceProvider.Create(ctx, nodeClaim)
	if err != nil {
		log.Error(err, "Failed to create instance")
		return nil, fmt.Errorf("creating instance, %w", err)
	}
	log.Info("Successfully created instance", "providerID", node.Spec.ProviderID)

	instanceType, _ := lo.Find(compatible, func(i *cloudprovider.InstanceType) bool {
		return i.Name == node.Labels["node.kubernetes.io/instance-type"]
	})
	if instanceType != nil {
		log.Info("Selected instance type", "type", instanceType.Name, "capacity", instanceType.Capacity)
	}

	nc := &karpv1.NodeClaim{
		ObjectMeta: nodeClaim.ObjectMeta,
		Spec:       nodeClaim.Spec,
		Status: karpv1.NodeClaimStatus{
			ProviderID: node.Spec.ProviderID,
		},
	}

	if instanceType != nil {
		nc.Status.Capacity = instanceType.Capacity
		nc.Status.Allocatable = instanceType.Allocatable()
	}

	nc.Annotations = lo.Assign(nc.Annotations, map[string]string{
		v1alpha1.AnnotationIBMNodeClassHash:        fmt.Sprintf("%d", nodeClass.Status.SpecHash),
		v1alpha1.AnnotationIBMNodeClassHashVersion: v1alpha1.IBMNodeClassHashVersion,
	})

	log.Info("Node creation completed successfully",
		"providerID", nc.Status.ProviderID,
		"capacity", nc.Status.Capacity,
		"allocatable", nc.Status.Allocatable)
	return nc, nil
}

func (c *CloudProvider) Delete(ctx context.Context, nodeClaim *karpv1.NodeClaim) error {
	log := log.FromContext(ctx).WithValues("nodeClaim", nodeClaim.Name, "providerID", nodeClaim.Status.ProviderID)
	log.Info("Deleting node")

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeClaim.Name,
		},
		Spec: corev1.NodeSpec{
			ProviderID: nodeClaim.Status.ProviderID,
		},
	}

	if err := c.instanceProvider.Delete(ctx, node); err != nil {
		log.Error(err, "Failed to delete node")
		return err
	}

	log.Info("Successfully deleted node")
	return nil
}

func (c *CloudProvider) GetInstanceTypes(ctx context.Context, nodePool *karpv1.NodePool) ([]*cloudprovider.InstanceType, error) {
	log := log.FromContext(ctx).WithValues("nodePool", nodePool.Name)
	log.Info("Getting instance types for NodePool")

	nodeClass := &v1alpha1.IBMNodeClass{}
	if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: nodePool.Spec.Template.Spec.NodeClassRef.Name}, nodeClass); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "Failed to resolve NodeClass")
			c.recorder.Publish(ibmevents.NodePoolFailedToResolveNodeClass(nodePool))
		}
		return nil, err
	}
	log.Info("Resolved NodeClass", "name", nodeClass.Name)

	instanceTypes, err := c.instanceTypeProvider.List(ctx)
	if err != nil {
		log.Error(err, "Failed to list instance types")
		return nil, err
	}
	log.Info("Successfully retrieved instance types", "count", len(instanceTypes))
	return instanceTypes, nil
}

func (c *CloudProvider) IsDrifted(ctx context.Context, nodeClaim *karpv1.NodeClaim) (cloudprovider.DriftReason, error) {
	log := log.FromContext(ctx).WithValues("nodeClaim", nodeClaim.Name, "providerID", nodeClaim.Status.ProviderID)
	log.Info("Checking if node has drifted")

	// Get the current hash from the node's annotations
	currentHash := nodeClaim.Annotations[v1alpha1.AnnotationIBMNodeClassHash]
	currentVersion := nodeClaim.Annotations[v1alpha1.AnnotationIBMNodeClassHashVersion]

	// Get the NodeClass
	nodeClass := &v1alpha1.IBMNodeClass{}
	if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaim.Spec.NodeClassRef.Name}, nodeClass); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "NodeClass not found")
			return "NodeClassNotFound", nil
		}
		return "", fmt.Errorf("getting nodeclass, %w", err)
	}

	// Check if the hash version matches
	if currentVersion != v1alpha1.IBMNodeClassHashVersion {
		log.Info("NodeClass hash version mismatch", "current", currentVersion, "expected", v1alpha1.IBMNodeClassHashVersion)
		return "NodeClassHashVersionChanged", nil
	}

	// Check if the hash matches
	if fmt.Sprintf("%d", nodeClass.Status.SpecHash) != currentHash {
		log.Info("NodeClass hash mismatch", "current", currentHash, "expected", nodeClass.Status.SpecHash)
		return "NodeClassHashChanged", nil
	}

	return "", nil
}

func (c *CloudProvider) Name() string {
	return CloudProviderName
}

func (c *CloudProvider) GetSupportedNodeClasses() []status.Object {
	return []status.Object{&v1alpha1.IBMNodeClass{}}
}
