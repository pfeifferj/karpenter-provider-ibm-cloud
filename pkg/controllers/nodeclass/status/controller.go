package status

import (
	"context"
	"fmt"

	"github.com/awslabs/operatorpkg/controller"
	"	github.com/awslabs/operatorpkg/controller/runtime
"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

// Controller reconciles an IBMNodeClass object to update its status
type Controller struct {
	kubeClient client.Client
}

// NewController constructs a controller instance
func NewController(kubeClient client.Client) controller.Controller {
	return controller.NewWithOptions(&Controller{
		kubeClient: kubeClient,
	}, controller.Options{
		Name: "nodeclass.status.karpenter.ibm.cloud",
	})
}

// Reconcile executes a control loop for the resource
func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	nc := &v1alpha1.IBMNodeClass{}
	if err := c.kubeClient.Get(ctx, req.NamespacedName, nc); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Validate the nodeclass configuration
	if err := c.validateNodeClass(ctx, nc); err != nil {
		patch := client.MergeFrom(nc.DeepCopy())
		nc.Status.LastValidationTime = metav1.Now()
		nc.Status.ValidationError = err.Error()
		if err := c.kubeClient.Status().Patch(ctx, nc, patch); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, err
	}

	// Clear any previous validation error
	if nc.Status.ValidationError != "" {
		patch := client.MergeFrom(nc.DeepCopy())
		nc.Status.LastValidationTime = metav1.Now()
		nc.Status.ValidationError = ""
		if err := c.kubeClient.Status().Patch(ctx, nc, patch); err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// validateNodeClass performs validation of the IBMNodeClass configuration
func (c *Controller) validateNodeClass(ctx context.Context, nc *v1alpha1.IBMNodeClass) error {
	if nc.Spec.Region == "" {
		return fmt.Errorf("region is required")
	}
	if nc.Spec.InstanceProfile == "" {
		return fmt.Errorf("instanceProfile is required")
	}
	if nc.Spec.Image == "" {
		return fmt.Errorf("image is required")
	}
	if nc.Spec.VPC == "" {
		return fmt.Errorf("vpc is required")
	}
	if nc.Spec.Subnet == "" {
		return fmt.Errorf("subnet is required")
	}
	return nil
}

// Name returns the name of the controller
func (c *Controller) Name() string {
	return "nodeclass.status"
}

// Builder implements controller.Builder
func (c *Controller) Builder(_ context.Context, m manager.Manager) runtime.Builder {
	return runtime.NewBuilder(m).
		For(&v1alpha1.IBMNodeClass{})
}
