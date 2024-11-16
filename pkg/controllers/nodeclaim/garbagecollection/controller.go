package garbagecollection

import (
	"context"

	"github.com/awslabs/operatorpkg/controller"
	"github.com/awslabs/operatorpkg/controller/runtime"
	v1 "k8s.io/api/core/v1"
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
func NewController(kubeClient client.Client, cloudProvider cloudprovider.CloudProvider) controller.Controller {
	return controller.NewWithOptions(&Controller{
		kubeClient:    kubeClient,
		cloudProvider: cloudProvider,
	}, controller.Options{
		Name: "nodeclaim.garbagecollection.karpenter.ibm.cloud",
	})
}

// Reconcile executes a control loop for the resource
func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	nodeClaim := &v1beta1.NodeClaim{}
	if err := c.kubeClient.Get(ctx, req.NamespacedName, nodeClaim); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// If nodeclaim is not being deleted, do nothing
	if nodeClaim.DeletionTimestamp == nil {
		return reconcile.Result{}, nil
	}

	// Get the associated node
	node := &v1.Node{}
	if err := c.kubeClient.Get(ctx, client.ObjectKey{Name: nodeClaim.Status.NodeName}, node); err != nil {
		if !client.IgnoreNotFound(err) {
			return reconcile.Result{}, err
		}
		// Node doesn't exist, remove finalizer from nodeclaim
		return reconcile.Result{}, c.removeFinalizer(ctx, nodeClaim)
	}

	// Delete the node if it exists
	if err := c.kubeClient.Delete(ctx, node); err != nil {
		if !client.IgnoreNotFound(err) {
			return reconcile.Result{}, err
		}
	}

	// Remove the finalizer from nodeclaim
	return reconcile.Result{}, c.removeFinalizer(ctx, nodeClaim)
}

func (c *Controller) removeFinalizer(ctx context.Context, nodeClaim *v1beta1.NodeClaim) error {
	if !containsString(nodeClaim.Finalizers, "karpenter.ibm.cloud/nodeclaim") {
		return nil
	}

	patch := client.MergeFrom(nodeClaim.DeepCopy())
	nodeClaim.Finalizers = removeString(nodeClaim.Finalizers, "karpenter.ibm.cloud/nodeclaim")
	return c.kubeClient.Patch(ctx, nodeClaim, patch)
}

// Name returns the name of the controller
func (c *Controller) Name() string {
	return "nodeclaim.garbagecollection"
}

// Builder implements controller.Builder
func (c *Controller) Builder(_ context.Context, m manager.Manager) runtime.Builder {
	return runtime.NewBuilder(m).
		For(&v1beta1.NodeClaim{})
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
