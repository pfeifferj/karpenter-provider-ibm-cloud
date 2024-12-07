package pricing

import (
	"context"
	"fmt"
	"time"

	"github.com/awslabs/operatorpkg/singleton"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/operator/injection"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/pricing"
)

type Controller struct {
	pricingProvider pricing.Provider
}

func NewController(pricingProvider pricing.Provider) (*Controller, error) {
	if pricingProvider == nil {
		return nil, fmt.Errorf("pricing provider cannot be nil")
	}
	return &Controller{
		pricingProvider: pricingProvider,
	}, nil
}

func (c *Controller) Reconcile(ctx context.Context) (reconcile.Result, error) {
	ctx = injection.WithControllerName(ctx, "providers.pricing")

	// Refresh pricing information
	if err := c.pricingProvider.Refresh(ctx); err != nil {
		return reconcile.Result{}, fmt.Errorf("refreshing pricing information: %w", err)
	}

	// Requeue after 12 hours to refresh prices
	return reconcile.Result{RequeueAfter: 12 * time.Hour}, nil
}

func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(m).
		Named("providers.pricing").
		WatchesRawSource(singleton.Source()).
		Complete(singleton.AsReconciler(c))
}
