package interruption

import (
	"context"
	"time"

	"github.com/awslabs/operatorpkg/singleton"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cache"
)

// Controller handles instance interruption events from IBM Cloud
type Controller struct {
	kubeClient          client.Client
	recorder            record.EventRecorder
	unavailableOfferings *cache.UnavailableOfferings
}

// NewController constructs a controller instance
func NewController(kubeClient client.Client, recorder record.EventRecorder, unavailableOfferings *cache.UnavailableOfferings) *Controller {
	return &Controller{
		kubeClient:          kubeClient,
		recorder:            recorder,
		unavailableOfferings: unavailableOfferings,
	}
}

// Reconcile executes a control loop for the resource
func (c *Controller) Reconcile(ctx context.Context) (reconcile.Result, error) {
	// Since we're using singleton pattern, we don't get a request object
	// Instead, we'll process all nodes in the cluster

	nodeList := &v1.NodeList{}
	if err := c.kubeClient.List(ctx, nodeList); err != nil {
		return reconcile.Result{}, err
	}

	for _, node := range nodeList.Items {
		// Check if node is being interrupted
		if !isNodeInterrupted(&node) {
			continue
		}

		// Record interruption event
		c.recorder.Event(&node, v1.EventTypeWarning, "Interruption",
			"Node is being interrupted by IBM Cloud")

		// Mark the instance type as unavailable if capacity related
		if isCapacityRelated(&node) {
			instanceType := node.Labels["node.kubernetes.io/instance-type"]
			zone := node.Labels["topology.kubernetes.io/zone"]
			c.unavailableOfferings.Add(instanceType+":"+zone, time.Now().Add(time.Hour))
		}

		// Cordon the node
		if !node.Spec.Unschedulable {
			nodeCopy := node.DeepCopy()
			nodeCopy.Spec.Unschedulable = true
			if err := c.kubeClient.Update(ctx, nodeCopy); err != nil {
				return reconcile.Result{}, err
			}
		}

		// Delete the node to trigger replacement
		if err := c.kubeClient.Delete(ctx, &node); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return reconcile.Result{}, err
			}
		}
	}

	return reconcile.Result{RequeueAfter: time.Minute}, nil
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

// Register registers the controller with the manager
func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return builder.ControllerManagedBy(m).
		For(&v1.Node{}).
		Complete(singleton.AsReconciler(c))
}
