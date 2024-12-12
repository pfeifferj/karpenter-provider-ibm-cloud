package autoplacement

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/subnet"
)

const (
	defaultRequeueTime = 5 * time.Second
	maxRequeueTime    = 5 * time.Minute

	// Condition types
	ConditionTypeAutoPlacement = "AutoPlacement"
)

// typedRateLimiter wraps a workqueue.RateLimiter to implement TypedRateLimiter
type typedRateLimiter struct {
	workqueue.RateLimiter
}

func (t *typedRateLimiter) When(item reconcile.Request) time.Duration {
	return t.RateLimiter.When(item)
}

func (t *typedRateLimiter) NumRequeues(item reconcile.Request) int {
	return t.RateLimiter.NumRequeues(item)
}

func (t *typedRateLimiter) Forget(item reconcile.Request) {
	t.RateLimiter.Forget(item)
}

// Controller reconciles IBMNodeClass resources
type Controller struct {
	client        client.Client
	instanceTypes instancetype.Provider
	subnets       subnet.Provider
	log           logr.Logger
}

// NewController creates a new autoplacement controller
func NewController(mgr manager.Manager, instanceTypes instancetype.Provider, subnets subnet.Provider) (*Controller, error) {
	c := &Controller{
		client:        mgr.GetClient(),
		instanceTypes: instanceTypes,
		subnets:      subnets,
		log:          mgr.GetLogger().WithName("autoplacement-controller"),
	}

	// Create a typed rate limiter that wraps the default controller rate limiter
	rateLimiter := &typedRateLimiter{
		RateLimiter: workqueue.DefaultControllerRateLimiter(),
	}

	// Use builder pattern to create and configure the controller
	err := builder.ControllerManagedBy(mgr).
		For(&v1alpha1.IBMNodeClass{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
			RateLimiter:            rateLimiter,
		}).
		Complete(c)

	if err != nil {
		return nil, fmt.Errorf("creating controller: %w", err)
	}

	return c, nil
}

// Reconcile handles IBMNodeClass reconciliation
func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	nodeClass := &v1alpha1.IBMNodeClass{}
	if err := c.client.Get(ctx, req.NamespacedName, nodeClass); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("getting nodeclass: %w", err)
	}

	nodeClass = nodeClass.DeepCopy()

	// Initialize status if needed
	if nodeClass.Status.Conditions == nil {
		nodeClass.Status.Conditions = []metav1.Condition{}
	}

	// Track if any automatic placement was performed
	autoPlacementPerformed := false

	// Handle automatic instance type selection
	if nodeClass.Spec.InstanceProfile == "" && nodeClass.Spec.InstanceRequirements != nil {
		autoPlacementPerformed = true
		start := time.Now()

		if err := c.handleInstanceTypeSelection(ctx, nodeClass); err != nil {
			c.log.Error(err, "failed to select instance types", "nodeclass", req.Name)
			instanceTypeSelections.WithLabelValues(nodeClass.Name, "failure").Inc()
			c.updateCondition(nodeClass, ConditionTypeAutoPlacement, metav1.ConditionFalse, "InstanceTypeSelectionFailed", err.Error())
			return reconcile.Result{}, err
		}

		instanceTypeSelections.WithLabelValues(nodeClass.Name, "success").Inc()
		instanceTypeSelectionLatency.WithLabelValues(nodeClass.Name).Observe(time.Since(start).Seconds())
		selectedInstanceTypes.WithLabelValues(nodeClass.Name).Set(float64(len(nodeClass.Status.SelectedInstanceTypes)))
	}

	// Handle automatic subnet selection
	if nodeClass.Spec.Subnet == "" && nodeClass.Spec.PlacementStrategy != nil {
		autoPlacementPerformed = true
		start := time.Now()

		if err := c.handleSubnetSelection(ctx, nodeClass); err != nil {
			c.log.Error(err, "failed to select subnets", "nodeclass", req.Name)
			subnetSelections.WithLabelValues(nodeClass.Name, "failure").Inc()
			c.updateCondition(nodeClass, ConditionTypeAutoPlacement, metav1.ConditionFalse, "SubnetSelectionFailed", err.Error())
			return reconcile.Result{}, err
		}

		subnetSelections.WithLabelValues(nodeClass.Name, "success").Inc()
		subnetSelectionLatency.WithLabelValues(nodeClass.Name).Observe(time.Since(start).Seconds())
		selectedSubnets.WithLabelValues(nodeClass.Name).Set(float64(len(nodeClass.Status.SelectedSubnets)))
	}

	// Update condition if auto-placement was performed
	if autoPlacementPerformed {
		c.updateCondition(nodeClass, ConditionTypeAutoPlacement, metav1.ConditionTrue, "AutoPlacementSucceeded", "Automatic placement completed successfully")
	}

	// Update status
	if err := c.client.Status().Update(ctx, nodeClass); err != nil {
		return reconcile.Result{}, fmt.Errorf("updating nodeclass status: %w", err)
	}

	return reconcile.Result{}, nil
}

// updateCondition updates a condition in the nodeclass status
func (c *Controller) updateCondition(nodeClass *v1alpha1.IBMNodeClass, conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	newCondition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: nodeClass.Generation,
	}

	// Find and update existing condition or append new one
	for i, existingCond := range nodeClass.Status.Conditions {
		if existingCond.Type == conditionType {
			if existingCond.Status != status {
				nodeClass.Status.Conditions[i] = newCondition
			}
			return
		}
	}

	// Append new condition if not found
	nodeClass.Status.Conditions = append(nodeClass.Status.Conditions, newCondition)
}

// handleInstanceTypeSelection implements automatic instance type selection
func (c *Controller) handleInstanceTypeSelection(ctx context.Context, nodeClass *v1alpha1.IBMNodeClass) error {
	// Get instance types matching requirements
	instanceTypes, err := c.instanceTypes.FilterInstanceTypes(ctx, nodeClass.Spec.InstanceRequirements)
	if err != nil {
		return fmt.Errorf("filtering instance types: %w", err)
	}

	if len(instanceTypes) == 0 {
		return fmt.Errorf("no instance types found matching requirements")
	}

	// Update status with selected instance types
	var selectedTypes []string
	for _, it := range instanceTypes {
		selectedTypes = append(selectedTypes, it.Name)
	}
	nodeClass.Status.SelectedInstanceTypes = selectedTypes

	// Use the most cost-efficient instance type
	nodeClass.Spec.InstanceProfile = instanceTypes[0].Name

	return nil
}

// handleSubnetSelection implements automatic subnet selection
func (c *Controller) handleSubnetSelection(ctx context.Context, nodeClass *v1alpha1.IBMNodeClass) error {
	// Select subnets based on placement strategy
	selectedSubnets, err := c.subnets.SelectSubnets(ctx, nodeClass.Spec.VPC, nodeClass.Spec.PlacementStrategy)
	if err != nil {
		return fmt.Errorf("selecting subnets: %w", err)
	}

	if len(selectedSubnets) == 0 {
		return fmt.Errorf("no subnets selected")
	}

	// Update status with selected subnets
	var subnetIDs []string
	for _, subnet := range selectedSubnets {
		subnetIDs = append(subnetIDs, subnet.ID)
	}
	nodeClass.Status.SelectedSubnets = subnetIDs

	// Use the first subnet (highest scored) as the primary
	nodeClass.Spec.Subnet = selectedSubnets[0].ID

	// If zone not specified, use the zone from selected subnet
	if nodeClass.Spec.Zone == "" {
		nodeClass.Spec.Zone = selectedSubnets[0].Zone
	}

	return nil
}
