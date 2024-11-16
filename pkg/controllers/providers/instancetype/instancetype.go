package instancetype

import (
	"context"
	"fmt"
	"time"

	"github.com/awslabs/operatorpkg/singleton"
	lop "github.com/samber/lo/parallel"
	"go.uber.org/multierr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/operator/injection"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
)

type Controller struct {
	instancetypeProvider instancetype.Provider
}

func NewController() (*Controller, error) {
	provider, err := instancetype.NewProvider()
	if err != nil {
		return nil, fmt.Errorf("creating instance type provider: %w", err)
	}

	return &Controller{
		instancetypeProvider: provider,
	}, nil
}

func (c *Controller) Reconcile(ctx context.Context) (reconcile.Result, error) {
	ctx = injection.WithControllerName(ctx, "providers.instancetype")

	work := []func(ctx context.Context) error{
		c.instancetypeProvider.UpdateInstanceTypes,
		c.instancetypeProvider.UpdateInstanceTypeOfferings,
	}
	errs := make([]error, len(work))
	lop.ForEach(work, func(f func(ctx context.Context) error, i int) {
		if err := f(ctx); err != nil {
			errs[i] = err
		}
	})
	if err := multierr.Combine(errs...); err != nil {
		return reconcile.Result{}, fmt.Errorf("updating instancetype, %w", err)
	}

	// Reconcile every hour to refresh instance type information
	return reconcile.Result{RequeueAfter: time.Hour}, nil
}

func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(m).
		Named("providers.instancetype").
		WatchesRawSource(singleton.Source()).
		Complete(singleton.AsReconciler(c))
}
