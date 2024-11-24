package tagging

import (
	"context"

	"github.com/awslabs/operatorpkg/controller"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/apis/v1beta1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
)

// Controller reconciles NodeClaim objects to ensure proper tagging of IBM Cloud instances
type Controller struct {
	kubeClient       client.Client
	instanceProvider instance.Provider
}

// NewController constructs a controller instance
func NewController(kubeClient client.Client, instanceProvider instance.Provider) controller.Controller {
	return controller.NewWithOptions(&Controller{
		kubeClient:       kubeClient,
		instanceProvider: instanceProvider,
	}, controller.Options{
		Name: "nodeclaim.tagging.karpenter.ibm.cloud",
	})
}

// Reconcile executes a control loop for the resource
func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	nodeClaim := &v1beta1.NodeClaim{}
	if err := c.kubeClient.Get(ctx, req.NamespacedName, nodeClaim); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Skip if node name is not set
	if nodeClaim.Status.NodeName == "" {
		return reconcile.Result{}, nil
	}

	// Get the node
	node := &v1.Node{}
	if err := c.kubeClient.Get(ctx, client.ObjectKey{Name: nodeClaim.Status.NodeName}, node); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Get the instance
	instance, err := c.instanceProvider.GetInstance(ctx, node)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Build tags map
	tags := map[string]string{
		"karpenter.ibm.cloud/nodeclaim": nodeClaim.Name,
		"karpenter.ibm.cloud/nodepool":  nodeClaim.Labels["karpenter.sh/nodepool"],
	}

	// Add custom tags from nodeclaim
	for key, value := range nodeClaim.Spec.Requirements.Tags() {
		tags[key] = value
	}

	// TODO: Implement tag update logic using IBM Cloud SDK
	// This would involve:
	// 1. Getting current tags
	// 2. Computing the difference
	// 3. Adding/removing tags as needed

	return reconcile.Result{}, nil
}

// Name returns the name of the controller
func (c *Controller) Name() string {
	return "nodeclaim.tagging"
}

// Builder implements controller.Builder
func (c *Controller) Builder(_ context.Context, m manager.Manager) *builder.Builder {
	return builder.ControllerManagedBy(m).
		For(&v1beta1.NodeClaim{}).
		Owns(&v1.Node{})
}
