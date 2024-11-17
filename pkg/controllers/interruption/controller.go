package interruption

import (
	"context"
	"time"

	"github.com/awslabs/operatorpkg/controller"
	"	github.com/awslabs/operatorpkg/controller/runtime
"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/events"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cache"
)

// Controller handles instance interruption events from IBM Cloud
type Controller struct {
	kubeClient          client.Client
	recorder            events.Recorder
	unavailableOfferings *cache.UnavailableOfferings
}

// NewController constructs a controller instance
func NewController(kubeClient client.Client, recorder events.Recorder, unavailableOfferings *cache.UnavailableOfferings) controller.Controller {
	return controller.NewWithOptions(&Controller{
		kubeClient:          kubeClient,
		recorder:            recorder,
		unavailableOfferings: unavailableOfferings,
	}, controller.Options{
		Name: "interruption.karpenter.ibm.cloud",
	})
}

// Reconcile executes a control loop for the resource
func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	node := &v1.Node{}
	if err := c.kubeClient.Get(ctx, req.NamespacedName, node); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Check if node is being interrupted
	if !isNodeInterrupted(node) {
		return reconcile.Result{}, nil
	}

	// Record interruption event
	c.recorder.Eventf(node, v1.EventTypeWarning, "Interruption",
		"Node %s is being interrupted by IBM Cloud", node.Name)

	// Mark the instance type as unavailable if capacity related
	if isCapacityRelated(node) {
		instanceType := node.Labels["node.kubernetes.io/instance-type"]
		zone := node.Labels["topology.kubernetes.io/zone"]
		c.unavailableOfferings.Add(instanceType+":"+zone, time.Now().Add(time.Hour))
	}

	// Cordon the node
	if !node.Spec.Unschedulable {
		node.Spec.Unschedulable = true
		if err := c.kubeClient.Update(ctx, node); err != nil {
			return reconcile.Result{}, err
		}
	}

	// Delete the node to trigger replacement
	if err := c.kubeClient.Delete(ctx, node); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	return reconcile.Result{}, nil
}

// isNodeInterrupted checks if a node is being interrupted by IBM Cloud
func isNodeInterrupted(node *v1.Node) bool {
	// TODO: Implement logic to check for IBM Cloud interruption events
	// This would involve checking node conditions, labels, or annotations
	// that indicate an interruption event

	
	return false
}

// isCapacityRelated checks if the interruption is due to capacity constraints
func isCapacityRelated(node *v1.Node) bool {
	// TODO: Implement logic to determine if interruption is capacity related
	// This would involve checking the specific interruption reason from IBM Cloud
	return false
}

// Name returns the name of the controller
func (c *Controller) Name() string {
	return "interruption"
}

// Builder implements controller.Builder
func (c *Controller) Builder(_ context.Context, m manager.Manager) runtime.Builder {
	return runtime.NewBuilder(m).
		For(&v1.Node{})
}
