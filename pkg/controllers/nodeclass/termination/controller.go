package termination

import (
	"context"

	"github.com/awslabs/operatorpkg/controller"
	"github.com/awslabs/operatorpkg/controller/runtime"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/events"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

// Controller reconciles IBMNodeClass deletion by terminating associated nodes
type Controller struct {
	kubeClient client.Client
	recorder   events.Recorder
}

// NewController constructs a controller instance
func NewController(kubeClient client.Client, recorder events.Recorder) controller.Controller {
	return controller.NewWithOptions(&Controller{
		kubeClient: kubeClient,
		recorder:   recorder,
	}, controller.Options{
		Name: "nodeclass.termination.karpenter.ibm.cloud",
	})
}

// Reconcile executes a control loop for the resource
func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	nc := &v1alpha1.IBMNodeClass{}
	if err := c.kubeClient.Get(ctx, req.NamespacedName, nc); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// If nodeclass is not being deleted, do nothing
	if nc.DeletionTimestamp == nil {
		return reconcile.Result{}, nil
	}

	// List all nodes using this nodeclass
	nodes := &v1.NodeList{}
	if err := c.kubeClient.List(ctx, nodes, client.MatchingLabels{
		"karpenter.ibm.cloud/nodeclass": nc.Name,
	}); err != nil {
		return reconcile.Result{}, err
	}

	// Delete all nodes
	for _, node := range nodes.Items {
		if err := c.kubeClient.Delete(ctx, &node); err != nil && !errors.IsNotFound(err) {
			c.recorder.Eventf(nc, v1.EventTypeWarning, "FailedToDeleteNode", 
				"Failed to delete node %s: %v", node.Name, err)
			return reconcile.Result{}, err
		}
		c.recorder.Eventf(nc, v1.EventTypeNormal, "DeletedNode",
			"Deleted node %s", node.Name)
	}

	return reconcile.Result{}, nil
}

// Name returns the name of the controller
func (c *Controller) Name() string {
	return "nodeclass.termination"
}

// Builder implements controller.Builder
func (c *Controller) Builder(_ context.Context, m manager.Manager) runtime.Builder {
	return runtime.NewBuilder(m).
		For(&v1alpha1.IBMNodeClass{})
}
