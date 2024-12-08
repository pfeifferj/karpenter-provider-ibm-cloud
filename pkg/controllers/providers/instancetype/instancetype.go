package instancetype

import (
	"context"
	"fmt"
	"time"

	"github.com/awslabs/operatorpkg/singleton"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/operator/injection"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
)

type Controller struct {
	instanceTypeProvider instancetype.Provider
}

func NewController() (*Controller, error) {
	provider, err := instancetype.NewProvider()
	if err != nil {
		return nil, fmt.Errorf("creating instance type provider: %w", err)
	}

	return &Controller{
		instanceTypeProvider: provider,
	}, nil
}

func (c *Controller) Reconcile(ctx context.Context) (reconcile.Result, error) {
	ctx = injection.WithControllerName(ctx, "providers.instancetype")

	// Refresh instance types by listing them
	if _, err := c.instanceTypeProvider.List(ctx); err != nil {
		return reconcile.Result{}, fmt.Errorf("refreshing instance types: %w", err)
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
