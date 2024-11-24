package hash

import (
	"context"

	"github.com/awslabs/operatorpkg/controller"
	"github.com/mitchellh/hashstructure/v2"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

// Controller computes a hash of the IBMNodeClass spec and stores it in the status
type Controller struct {
	kubeClient client.Client
}

// NewController constructs a controller instance
func NewController(kubeClient client.Client) controller.Controller {
	return controller.NewWithOptions(&Controller{
		kubeClient: kubeClient,
	}, controller.Options{
		Name: "nodeclass.hash.karpenter.ibm.cloud",
	})
}

// Reconcile executes a control loop for the resource
func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	nc := &v1alpha1.IBMNodeClass{}
	if err := c.kubeClient.Get(ctx, req.NamespacedName, nc); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Compute hash of the spec
	hash, err := hashstructure.Hash(nc.Spec, hashstructure.FormatV2, nil)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Update status if hash changed
	if nc.Status.SpecHash != hash {
		patch := client.MergeFrom(nc.DeepCopy())
		nc.Status.SpecHash = hash
		if err := c.kubeClient.Status().Patch(ctx, nc, patch); err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// Name returns the name of the controller
func (c *Controller) Name() string {
	return "nodeclass.hash"
}

// Builder implements controller.Builder
func (c *Controller) Builder(_ context.Context, m manager.Manager) *builder.Builder {
	return builder.ControllerManagedBy(m).
		For(&v1alpha1.IBMNodeClass{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(object client.Object) bool {
			return true // Only reconcile on spec changes
		}))
}
