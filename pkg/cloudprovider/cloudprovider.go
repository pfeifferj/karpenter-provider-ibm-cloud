/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cloudprovider

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"time"

	"github.com/awslabs/operatorpkg/status"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	coreapis "sigs.k8s.io/karpenter/pkg/apis"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"
	"sigs.k8s.io/karpenter/pkg/scheduling"
	"sigs.k8s.io/karpenter/pkg/utils/resources"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	ibmevents "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/events"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/utils"
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

func (c *CloudProvider) Create(ctx context.Context, nodeClaim *karpv1.NodeClaim) (*karpv1.NodeClaim, error) {
	log := log.FromContext(ctx).WithValues(
		"nodeClaim", nodeClaim.Name,
		"requirements", nodeClaim.Spec.Requirements,
		"resources", nodeClaim.Spec.Resources)
	log.Info("Starting node creation")

	nodeClass, err := c.resolveNodeClassFromNodeClaim(ctx, nodeClaim)
	if err != nil {
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
			readyCondition = metav1.Condition{
				Type:               condition.Type,
				Status:             metav1.ConditionStatus(condition.Status),
				Reason:             condition.Reason,
				Message:            condition.Message,
				LastTransitionTime: condition.LastTransitionTime,
				ObservedGeneration: condition.ObservedGeneration,
			}
			break
		}
	}

	if readyCondition.Status == metav1.ConditionFalse {
		log.Error(stderrors.New(readyCondition.Message), "NodeClass not ready")
		return nil, cloudprovider.NewNodeClassNotReadyError(stderrors.New(readyCondition.Message))
	}
	if readyCondition.Status == metav1.ConditionUnknown {
		log.Error(stderrors.New(readyCondition.Message), "NodeClass readiness unknown")
		return nil, fmt.Errorf("resolving NodeClass readiness, NodeClass is in Ready=Unknown, %s", readyCondition.Message)
	}

	log.Info("Resolving instance types")
	instanceTypes, err := c.resolveInstanceTypes(ctx, nodeClaim)
	if err != nil {
		log.Error(err, "Failed to resolve instance types")
		return nil, fmt.Errorf("resolving instance types, %w", err)
	}
	if len(instanceTypes) == 0 {
		log.Error(nil, "No compatible instance types found")
		return nil, cloudprovider.NewInsufficientCapacityError(fmt.Errorf("all requested instance types were unavailable during launch"))
	}
	log.Info("Found compatible instance types", "count", len(instanceTypes), "types", lo.Map(instanceTypes, func(it *cloudprovider.InstanceType, _ int) string { return it.Name }))

	log.Info("Creating instance")
	node, err := c.instanceProvider.Create(ctx, nodeClaim)
	if err != nil {
		log.Error(err, "Failed to create instance")
		return nil, fmt.Errorf("creating instance, %w", err)
	}
	log.Info("Successfully created instance", "providerID", node.Spec.ProviderID)

	instanceType, _ := lo.Find(instanceTypes, func(i *cloudprovider.InstanceType) bool {
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

func (c *CloudProvider) Get(ctx context.Context, providerID string) (*karpv1.NodeClaim, error) {
	log := log.FromContext(ctx).WithValues("providerID", providerID)
	log.Info("Getting instance details")

	// Create a Node object to pass to GetInstance
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

	instanceType, err := c.resolveInstanceTypeFromInstance(ctx, instance)
	if err != nil {
		log.Error(err, "Failed to resolve instance type")
		return nil, fmt.Errorf("resolving instance type, %w", err)
	}
	if instanceType != nil {
		log.Info("Resolved instance type", "type", instanceType.Name)
	}

	nc, err := c.resolveNodeClassFromInstance(ctx, instance)
	if client.IgnoreNotFound(err) != nil {
		log.Error(err, "Failed to resolve NodeClass")
		return nil, fmt.Errorf("resolving nodeclass, %w", err)
	}
	if nc != nil {
		log.Info("Resolved NodeClass", "name", nc.Name)
	}

	nodeClaim := c.instanceToNodeClaim(instance, instanceType, nc)
	log.Info("Successfully retrieved NodeClaim",
		"providerID", nodeClaim.Status.ProviderID,
		"capacity", nodeClaim.Status.Capacity,
		"allocatable", nodeClaim.Status.Allocatable)
	return nodeClaim, nil
}

func (c *CloudProvider) List(ctx context.Context) ([]*karpv1.NodeClaim, error) {
	log := log.FromContext(ctx)
	log.Info("Listing all nodes")

	// Get all nodes in the cluster
	nodeList := &corev1.NodeList{}
	if err := c.kubeClient.List(ctx, nodeList); err != nil {
		log.Error(err, "Failed to list nodes")
		return nil, fmt.Errorf("listing nodes, %w", err)
	}

	var nodeClaims []*karpv1.NodeClaim
	for _, node := range nodeList.Items {
		nodeLog := log.WithValues("node", node.Name)
		// Skip nodes without provider IDs
		if node.Spec.ProviderID == "" {
			nodeLog.Info("Skipping node without providerID")
			continue
		}

		// Get instance details for each node
		instance, err := c.instanceProvider.GetInstance(ctx, &node)
		if err != nil {
			nodeLog.Error(err, "Failed to get instance for node")
			continue
		}
		nodeLog.Info("Found instance", "type", instance.Type, "zone", instance.Zone)

		instanceType, err := c.resolveInstanceTypeFromInstance(ctx, instance)
		if err != nil {
			nodeLog.Error(err, "Failed to resolve instance type")
			continue
		}
		if instanceType != nil {
			nodeLog.Info("Resolved instance type", "type", instanceType.Name)
		}

		nc, err := c.resolveNodeClassFromInstance(ctx, instance)
		if client.IgnoreNotFound(err) != nil {
			nodeLog.Error(err, "Failed to resolve NodeClass")
			continue
		}
		if nc != nil {
			nodeLog.Info("Resolved NodeClass", "name", nc.Name)
		}

		nodeClaim := c.instanceToNodeClaim(instance, instanceType, nc)
		nodeClaims = append(nodeClaims, nodeClaim)
	}

	log.Info("Successfully listed NodeClaims", "count", len(nodeClaims))
	return nodeClaims, nil
}

func (c *CloudProvider) LivenessProbe(req *http.Request) error {
	return nil
}

func (c *CloudProvider) GetInstanceTypes(ctx context.Context, nodePool *karpv1.NodePool) ([]*cloudprovider.InstanceType, error) {
	log := log.FromContext(ctx).WithValues("nodePool", nodePool.Name)
	log.Info("Getting instance types for NodePool")

	nodeClass, err := c.resolveNodeClassFromNodePool(ctx, nodePool)
	if err != nil {
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

func (c *CloudProvider) Delete(ctx context.Context, nodeClaim *karpv1.NodeClaim) error {
	log := log.FromContext(ctx).WithValues("nodeClaim", nodeClaim.Name, "providerID", nodeClaim.Status.ProviderID)
	log.Info("Deleting node")

	// Create a Node object from the NodeClaim
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

func (c *CloudProvider) IsDrifted(ctx context.Context, nodeClaim *karpv1.NodeClaim) (cloudprovider.DriftReason, error) {
	log := log.FromContext(ctx).WithValues("nodeClaim", nodeClaim.Name)
	log.Info("Checking for drift")

	nodePoolName, ok := nodeClaim.Labels[karpv1.NodePoolLabelKey]
	if !ok {
		log.Info("NodeClaim has no NodePool label")
		return "", nil
	}

	nodePool := &karpv1.NodePool{}
	if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: nodePoolName}, nodePool); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "Failed to get NodePool")
		}
		return "", client.IgnoreNotFound(err)
	}

	if nodePool.Spec.Template.Spec.NodeClassRef == nil {
		log.Info("NodePool has no NodeClassRef")
		return "", nil
	}

	if _, err := c.resolveNodeClassFromNodePool(ctx, nodePool); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "Failed to resolve NodeClass")
			c.recorder.Publish(ibmevents.NodePoolFailedToResolveNodeClass(nodePool))
		}
		return "", client.IgnoreNotFound(fmt.Errorf("resolving node class, %w", err))
	}

	log.Info("No drift detected")
	return "", nil
}

func (c *CloudProvider) Name() string {
	return CloudProviderName
}

func (c *CloudProvider) GetSupportedNodeClasses() []status.Object {
	return []status.Object{&v1alpha1.IBMNodeClass{}}
}

func (c *CloudProvider) resolveInstanceTypeFromInstance(ctx context.Context, instance *instance.Instance) (*cloudprovider.InstanceType, error) {
	log := log.FromContext(ctx)

	nodePool, err := c.resolveNodePoolFromInstance(ctx, instance)
	if err != nil {
		return nil, client.IgnoreNotFound(fmt.Errorf("resolving nodepool, %w", err))
	}

	instanceTypes, err := c.GetInstanceTypes(ctx, nodePool)
	if err != nil {
		return nil, client.IgnoreNotFound(fmt.Errorf("resolving nodeclass, %w", err))
	}

	instanceType, _ := lo.Find(instanceTypes, func(i *cloudprovider.InstanceType) bool {
		return i.Name == instance.Type
	})

	if instanceType != nil {
		log.Info("Resolved instance type", "type", instanceType.Name)
	}

	return instanceType, nil
}

func (c *CloudProvider) resolveNodePoolFromInstance(ctx context.Context, instance *instance.Instance) (*karpv1.NodePool, error) {
	if nodePoolName, ok := instance.Tags[karpv1.NodePoolLabelKey]; ok {
		nodePool := &karpv1.NodePool{}
		if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: nodePoolName}, nodePool); err != nil {
			return nil, err
		}
		return nodePool, nil
	}
	return nil, errors.NewNotFound(schema.GroupResource{Group: coreapis.Group, Resource: "nodepools"}, "")
}

func (c *CloudProvider) resolveNodeClassFromInstance(ctx context.Context, instance *instance.Instance) (*v1alpha1.IBMNodeClass, error) {
	np, err := c.resolveNodePoolFromInstance(ctx, instance)
	if err != nil {
		return nil, fmt.Errorf("resolving nodepool, %w", err)
	}
	return c.resolveNodeClassFromNodePool(ctx, np)
}

func (c *CloudProvider) resolveNodeClassFromNodePool(ctx context.Context, nodePool *karpv1.NodePool) (*v1alpha1.IBMNodeClass, error) {
	nodeClass := &v1alpha1.IBMNodeClass{}
	if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: nodePool.Spec.Template.Spec.NodeClassRef.Name}, nodeClass); err != nil {
		return nil, err
	}
	if !nodeClass.DeletionTimestamp.IsZero() {
		return nil, newTerminatingNodeClassError(nodeClass.Name)
	}
	return nodeClass, nil
}

func newTerminatingNodeClassError(name string) *errors.StatusError {
	qualifiedResource := schema.GroupResource{Group: v1alpha1.Group, Resource: "ibmnodeclasses"}
	err := errors.NewNotFound(qualifiedResource, name)
	err.ErrStatus.Message = fmt.Sprintf("%s %q is terminating, treating as not found", qualifiedResource.String(), name)
	return err
}

func (c *CloudProvider) instanceToNodeClaim(i *instance.Instance, instanceType *cloudprovider.InstanceType, _ *v1alpha1.IBMNodeClass) *karpv1.NodeClaim {
	nodeClaim := &karpv1.NodeClaim{}
	labels := map[string]string{}
	annotations := map[string]string{}

	if instanceType != nil {
		labels = utils.GetAllSingleValuedRequirementLabels(instanceType)
		resourceFilter := func(n corev1.ResourceName, v resource.Quantity) bool {
			return !resources.IsZero(v)
		}
		nodeClaim.Status.Capacity = lo.PickBy(instanceType.Capacity, resourceFilter)
		nodeClaim.Status.Allocatable = lo.PickBy(instanceType.Allocatable(), resourceFilter)
	}
	labels[corev1.LabelTopologyZone] = i.Zone
	labels[karpv1.CapacityTypeLabelKey] = i.CapacityType
	if v, ok := i.Tags[karpv1.NodePoolLabelKey]; ok {
		labels[karpv1.NodePoolLabelKey] = v
	}
	nodeClaim.Labels = labels
	nodeClaim.Annotations = annotations

	nodeClaim.CreationTimestamp = metav1.Time{Time: i.CreationTime}

	if i.Status == instance.InstanceStatusStopping || i.Status == instance.InstanceStatusStopped {
		nodeClaim.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	}

	nodeClaim.Status.ProviderID = fmt.Sprintf("%s.%s", i.Region, i.ID)
	nodeClaim.Status.ImageID = i.ImageID
	return nodeClaim
}

func (c *CloudProvider) resolveNodeClassFromNodeClaim(ctx context.Context, nodeClaim *karpv1.NodeClaim) (*v1alpha1.IBMNodeClass, error) {
	nodeClass := &v1alpha1.IBMNodeClass{}
	if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaim.Spec.NodeClassRef.Name}, nodeClass); err != nil {
		return nil, err
	}
	if !nodeClass.DeletionTimestamp.IsZero() {
		return nil, newTerminatingNodeClassError(nodeClass.Name)
	}
	return nodeClass, nil
}

func (c *CloudProvider) resolveInstanceTypes(ctx context.Context, nodeClaim *karpv1.NodeClaim) ([]*cloudprovider.InstanceType, error) {
	log := log.FromContext(ctx)
	log.Info("Resolving instance types for NodeClaim",
		"requirements", nodeClaim.Spec.Requirements,
		"resources", nodeClaim.Spec.Resources)

	instanceTypes, err := c.instanceTypeProvider.List(ctx)
	if err != nil {
		log.Error(err, "Failed to get instance types")
		return nil, fmt.Errorf("getting instance types, %w", err)
	}
	log.Info("Retrieved instance types", "count", len(instanceTypes))

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

	log.Info("Found compatible instance types",
		"total", len(instanceTypes),
		"compatible", len(compatible),
		"types", lo.Map(compatible, func(it *cloudprovider.InstanceType, _ int) string { return it.Name }))
	return compatible, nil
}
