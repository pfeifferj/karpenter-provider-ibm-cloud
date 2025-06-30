package hash

import (
	"context"
	"fmt"

	"github.com/mitchellh/hashstructure/v2"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

// Controller computes a hash of the IBMNodeClass spec and stores it in the status
//+kubebuilder:rbac:groups=karpenter.ibm.sh,resources=ibmnodeclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=karpenter.ibm.sh,resources=ibmnodeclasses/status,verbs=get;update;patch
type Controller struct {
	kubeClient client.Client
}

// NewController constructs a controller instance
func NewController(kubeClient client.Client) (*Controller, error) {
	if kubeClient == nil {
		return nil, fmt.Errorf("kubeClient cannot be nil")
	}
	return &Controller{
		kubeClient: kubeClient,
	}, nil
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

// Register registers the controller with the manager
func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(m).
		Named("nodeclass.hash").
		For(&v1alpha1.IBMNodeClass{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(object client.Object) bool {
			return true // Only reconcile on spec changes
		})).
		Complete(c)
}
