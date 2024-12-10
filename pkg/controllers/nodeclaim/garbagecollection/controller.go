package garbagecollection

import (
	"context"

	"github.com/awslabs/operatorpkg/singleton"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/apis/v1beta1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

// Controller reconciles NodeClaim objects for garbage collection
type Controller struct {
	kubeClient    client.Client
	cloudProvider cloudprovider.CloudProvider
}

// NewController constructs a controller instance
func NewController(kubeClient client.Client, cloudProvider cloudprovider.CloudProvider) *Controller {
	return &Controller{
		kubeClient:    kubeClient,
		cloudProvider: cloudProvider,
	}
}

// Reconcile executes a control loop for the resource
func (c *Controller) Reconcile(ctx context.Context) (reconcile.Result, error) {
	// List all NodeClaims
	nodeClaimList := &v1beta1.NodeClaimList{}
	if err := c.kubeClient.List(ctx, nodeClaimList); err != nil {
		return reconcile.Result{}, err
	}

	for _, nodeClaim := range nodeClaimList.Items {
		// If nodeclaim is not being deleted, skip it
		if nodeClaim.DeletionTimestamp == nil {
			continue
		}

		// Get the associated node
		if nodeClaim.Status.NodeName == "" {
			// No node associated, remove finalizer
			if err := c.removeFinalizer(ctx, &nodeClaim); err != nil {
				return reconcile.Result{}, err
			}
			continue
		}

		node := &v1.Node{}
		if err := c.kubeClient.Get(ctx, client.ObjectKey{Name: nodeClaim.Status.NodeName}, node); err != nil {
			if client.IgnoreNotFound(err) == nil {
				// Node doesn't exist, remove finalizer from nodeclaim
				if err := c.removeFinalizer(ctx, &nodeClaim); err != nil {
					return reconcile.Result{}, err
				}
				continue
			}
			return reconcile.Result{}, err
		}

		// Delete the node if it exists
		if err := c.kubeClient.Delete(ctx, node); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return reconcile.Result{}, err
			}
		}

		// Remove the finalizer from nodeclaim
		if err := c.removeFinalizer(ctx, &nodeClaim); err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (c *Controller) removeFinalizer(ctx context.Context, nodeClaim *v1beta1.NodeClaim) error {
	if !containsString(nodeClaim.Finalizers, "karpenter.ibm.sh/nodeclaim") {
		return nil
	}

	patch := client.MergeFrom(nodeClaim.DeepCopy())
	nodeClaim.Finalizers = removeString(nodeClaim.Finalizers, "karpenter.ibm.sh/nodeclaim")
	return c.kubeClient.Patch(ctx, nodeClaim, patch)
}

// Name returns the name of the controller
func (c *Controller) Name() string {
	return "nodeclaim.garbagecollection"
}

// Register registers the controller with the manager
func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return builder.ControllerManagedBy(m).
		For(&v1beta1.NodeClaim{}).
		Complete(singleton.AsReconciler(c))
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	var result []string
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}
