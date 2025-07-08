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

package registration

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	// NodeClaimRegistrationFinalizer is added to NodeClaims to ensure proper cleanup
	NodeClaimRegistrationFinalizer = "registration.nodeclaim.ibm.sh/finalizer"
	
	// Labels and taints for registered nodes
	RegisteredLabel     = "karpenter.sh/registered"
	UnregisteredTaint   = "karpenter.sh/unregistered"
	NodePoolLabel       = "karpenter.sh/nodepool"
	NodeClassLabel      = "karpenter.ibm.sh/ibmnodeclass"
	ProvisionerLabel    = "provisioner"
	ProvisionedTaint    = "karpenter.ibm.sh/provisioned"
)

// Controller reconciles NodeClaim registration with corresponding Nodes
//+kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=karpenter.ibm.sh,resources=ibmnodeclasses,verbs=get;list;watch
type Controller struct {
	kubeClient client.Client
}

// NewController constructs a controller instance
func NewController(kubeClient client.Client) (*Controller, error) {
	return &Controller{
		kubeClient: kubeClient,
	}, nil
}

// Reconcile executes a control loop for NodeClaim registration
func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("nodeclaim", req.Name)
	
	// Get the NodeClaim
	nodeClaim := &karpv1.NodeClaim{}
	if err := c.kubeClient.Get(ctx, req.NamespacedName, nodeClaim); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion
	if !nodeClaim.DeletionTimestamp.IsZero() {
		return c.handleDeletion(ctx, nodeClaim)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(nodeClaim, NodeClaimRegistrationFinalizer) {
		patch := client.MergeFrom(nodeClaim.DeepCopy())
		controllerutil.AddFinalizer(nodeClaim, NodeClaimRegistrationFinalizer)
		if err := c.kubeClient.Patch(ctx, nodeClaim, patch); err != nil {
			return reconcile.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		logger.V(1).Info("added registration finalizer to nodeclaim")
	}

	// Skip if already registered
	if c.isNodeClaimRegistered(nodeClaim) {
		return reconcile.Result{}, nil
	}

	// Find the corresponding node
	node, err := c.findNodeForNodeClaim(ctx, nodeClaim)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("finding node for nodeclaim: %w", err)
	}

	if node == nil {
		// Node not found yet, requeue to check again
		logger.V(1).Info("node not found for nodeclaim, requeueing")
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Sync NodeClaim properties to Node
	if err := c.syncNodeClaimToNode(ctx, nodeClaim, node); err != nil {
		return reconcile.Result{}, fmt.Errorf("syncing nodeclaim properties to node: %w", err)
	}

	// Update NodeClaim status to mark as registered
	if err := c.updateNodeClaimStatus(ctx, nodeClaim, node); err != nil {
		return reconcile.Result{}, fmt.Errorf("updating nodeclaim status: %w", err)
	}

	logger.Info("successfully registered nodeclaim with node", "node", node.Name)
	return reconcile.Result{}, nil
}

// handleDeletion handles NodeClaim deletion cleanup
func (c *Controller) handleDeletion(ctx context.Context, nodeClaim *karpv1.NodeClaim) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("nodeclaim", nodeClaim.Name)
	
	if !controllerutil.ContainsFinalizer(nodeClaim, NodeClaimRegistrationFinalizer) {
		return reconcile.Result{}, nil
	}

	// Clean up any registration-specific resources if needed
	// For now, just remove the finalizer
	patch := client.MergeFrom(nodeClaim.DeepCopy())
	controllerutil.RemoveFinalizer(nodeClaim, NodeClaimRegistrationFinalizer)
	if err := c.kubeClient.Patch(ctx, nodeClaim, patch); err != nil {
		return reconcile.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	
	logger.V(1).Info("removed registration finalizer from nodeclaim")
	return reconcile.Result{}, nil
}

// isNodeClaimRegistered checks if a NodeClaim is already registered
func (c *Controller) isNodeClaimRegistered(nodeClaim *karpv1.NodeClaim) bool {
	return nodeClaim.StatusConditions().Get(karpv1.ConditionTypeRegistered).IsTrue()
}

// findNodeForNodeClaim finds the Node corresponding to a NodeClaim
func (c *Controller) findNodeForNodeClaim(ctx context.Context, nodeClaim *karpv1.NodeClaim) (*corev1.Node, error) {
	// If NodeClaim already has a node name, use it
	if nodeClaim.Status.NodeName != "" {
		node := &corev1.Node{}
		if err := c.kubeClient.Get(ctx, client.ObjectKey{Name: nodeClaim.Status.NodeName}, node); err != nil {
			return nil, client.IgnoreNotFound(err)
		}
		return node, nil
	}

	// Find node by provider ID
	if nodeClaim.Status.ProviderID != "" {
		nodeList := &corev1.NodeList{}
		if err := c.kubeClient.List(ctx, nodeList); err != nil {
			return nil, err
		}

		for _, node := range nodeList.Items {
			if node.Spec.ProviderID == nodeClaim.Status.ProviderID {
				return &node, nil
			}
		}
	}

	return nil, nil
}

// syncNodeClaimToNode syncs labels and taints from NodeClaim to Node
func (c *Controller) syncNodeClaimToNode(ctx context.Context, nodeClaim *karpv1.NodeClaim, node *corev1.Node) error {
	patch := client.MergeFrom(node.DeepCopy())
	modified := false

	// Add finalizer to node
	if !controllerutil.ContainsFinalizer(node, NodeClaimRegistrationFinalizer) {
		controllerutil.AddFinalizer(node, NodeClaimRegistrationFinalizer)
		modified = true
	}

	// Sync required labels
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	// Add NodePool label
	if nodePoolName, exists := nodeClaim.Labels[NodePoolLabel]; exists {
		if node.Labels[NodePoolLabel] != nodePoolName {
			node.Labels[NodePoolLabel] = nodePoolName
			modified = true
		}
	}

	// Add NodeClass label
	if nodeClaim.Spec.NodeClassRef != nil {
		if node.Labels[NodeClassLabel] != nodeClaim.Spec.NodeClassRef.Name {
			node.Labels[NodeClassLabel] = nodeClaim.Spec.NodeClassRef.Name
			modified = true
		}
	}

	// Add provisioner label from NodeClaim template
	if provisioner, exists := nodeClaim.Labels[ProvisionerLabel]; exists {
		if node.Labels[ProvisionerLabel] != provisioner {
			node.Labels[ProvisionerLabel] = provisioner
			modified = true
		}
	}

	// Add registered label
	if node.Labels[RegisteredLabel] != "true" {
		node.Labels[RegisteredLabel] = "true"
		modified = true
	}

	// Sync taints from NodeClaim to Node (unless do-not-sync label is set)
	if _, skipSync := nodeClaim.Labels["karpenter.sh/do-not-sync-taints"]; !skipSync {
		if c.syncTaintsToNode(nodeClaim, node) {
			modified = true
		}
	}

	// Remove unregistered taint
	if c.removeTaintFromNode(node, UnregisteredTaint) {
		modified = true
	}

	if modified {
		if err := c.kubeClient.Patch(ctx, node, patch); err != nil {
			return fmt.Errorf("patching node: %w", err)
		}
	}

	return nil
}

// syncTaintsToNode syncs taints from NodeClaim to Node
func (c *Controller) syncTaintsToNode(nodeClaim *karpv1.NodeClaim, node *corev1.Node) bool {
	modified := false
	
	for _, ncTaint := range nodeClaim.Spec.Taints {
		found := false
		for _, nodeTaint := range node.Spec.Taints {
			if nodeTaint.Key == ncTaint.Key && nodeTaint.Effect == ncTaint.Effect {
				found = true
				break
			}
		}
		
		if !found {
			node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{
				Key:    ncTaint.Key,
				Value:  ncTaint.Value,
				Effect: ncTaint.Effect,
			})
			modified = true
		}
	}
	
	return modified
}

// removeTaintFromNode removes a taint from a node
func (c *Controller) removeTaintFromNode(node *corev1.Node, taintKey string) bool {
	initialLen := len(node.Spec.Taints)
	node.Spec.Taints = lo.Filter(node.Spec.Taints, func(taint corev1.Taint, _ int) bool {
		return taint.Key != taintKey
	})
	return len(node.Spec.Taints) != initialLen
}

// updateNodeClaimStatus updates the NodeClaim status to mark it as registered
func (c *Controller) updateNodeClaimStatus(ctx context.Context, nodeClaim *karpv1.NodeClaim, node *corev1.Node) error {
	patch := client.MergeFrom(nodeClaim.DeepCopy())
	
	// Update node name if not set
	if nodeClaim.Status.NodeName != node.Name {
		nodeClaim.Status.NodeName = node.Name
	}

	// Set registered condition using the proper StatusConditions interface
	nodeClaim.StatusConditions().SetTrue(karpv1.ConditionTypeRegistered)

	return c.kubeClient.Status().Patch(ctx, nodeClaim, patch)
}

// Register registers the controller with the manager
func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return builder.ControllerManagedBy(m).
		Named("nodeclaim.registration.ibm").
		For(&karpv1.NodeClaim{}).
		Owns(&corev1.Node{}).
		Complete(c)
}