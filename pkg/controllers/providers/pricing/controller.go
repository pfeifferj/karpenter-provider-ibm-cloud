package pricing

import (
	"context"
	"time"

	"github.com/awslabs/operatorpkg/controller"
	"github.com/awslabs/operatorpkg/controller/runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/pricing"
)

// Controller reconciles pricing information
type Controller struct {
	pricingProvider pricing.Provider
}

// NewController constructs a controller instance
func NewController(pricingProvider pricing.Provider) controller.Controller {
	return controller.NewWithOptions(&Controller{
		pricingProvider: pricingProvider,
	}, controller.Options{
		Name: "providers.pricing.karpenter.ibm.cloud",
	})
}

// Reconcile executes a control loop for the resource
func (c *Controller) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	// Refresh pricing information
	if err := c.pricingProvider.Refresh(ctx); err != nil {
		return reconcile.Result{}, err
	}

	// Requeue after 1 hour to refresh prices
	return reconcile.Result{RequeueAfter: time.Hour}, nil
}

// Name returns the name of the controller
func (c *Controller) Name() string {
	return "providers.pricing"
}

// Builder implements controller.Builder
func (c *Controller) Builder(_ context.Context, m manager.Manager) runtime.Builder {
	return runtime.NewBuilder(m).
		// This controller doesn't watch any resources
		// It runs on a timer-based reconciliation
		Complete()
}
