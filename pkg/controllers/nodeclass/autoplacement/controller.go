/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
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
	// Condition types
	ConditionTypeAutoPlacement = "AutoPlacement"
)

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
		subnets:       subnets,
		log:           mgr.GetLogger().WithName("autoplacement-controller"),
	}

	// Use builder pattern to create and configure the controller
	err := builder.ControllerManagedBy(mgr).
		For(&v1alpha1.IBMNodeClass{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
			RateLimiter:             workqueue.DefaultTypedControllerRateLimiter[reconcile.Request](),
		}).
		Complete(c)

	if err != nil {
		return nil, fmt.Errorf("creating controller: %w", err)
	}

	return c, nil
}

// Start implements manager.Runnable
func (c *Controller) Start(ctx context.Context) error {
	// No-op: reconciliation is handled by controller-runtime
	return nil
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

	// Initialize status if needed
	if nodeClass.Status.Conditions == nil {
		nodeClass.Status.Conditions = []metav1.Condition{}
	}
	if nodeClass.Status.SelectedInstanceTypes == nil {
		nodeClass.Status.SelectedInstanceTypes = []string{}
	}
	if nodeClass.Status.SelectedSubnets == nil {
		nodeClass.Status.SelectedSubnets = []string{}
	}

	// Handle automatic instance type selection
	if nodeClass.Spec.InstanceProfile == "" && nodeClass.Spec.InstanceRequirements != nil {
		start := time.Now()

		c.log.Info("Starting instance type selection", "nodeclass", req.Name)

		// Get instance types matching requirements
		instanceTypes, err := c.instanceTypes.FilterInstanceTypes(ctx, nodeClass.Spec.InstanceRequirements)
		if err != nil {
			c.log.Error(err, "failed to select instance types", "nodeclass", req.Name)
			InstanceTypeSelections.WithLabelValues(nodeClass.Name, "failure").Inc()
			c.updateCondition(nodeClass, ConditionTypeAutoPlacement, metav1.ConditionFalse, "InstanceTypeSelectionFailed", err.Error())
			if updateErr := c.client.Status().Update(ctx, nodeClass); updateErr != nil {
				return reconcile.Result{}, fmt.Errorf("updating nodeclass status: %w", updateErr)
			}
			return reconcile.Result{}, err
		}

		if len(instanceTypes) == 0 {
			err := fmt.Errorf("no instance types found matching requirements")
			c.log.Error(err, "instance type selection failed", "nodeclass", req.Name)
			InstanceTypeSelections.WithLabelValues(nodeClass.Name, "failure").Inc()
			c.updateCondition(nodeClass, ConditionTypeAutoPlacement, metav1.ConditionFalse, "InstanceTypeSelectionFailed", err.Error())
			if updateErr := c.client.Status().Update(ctx, nodeClass); updateErr != nil {
				return reconcile.Result{}, fmt.Errorf("updating nodeclass status: %w", updateErr)
			}
			return reconcile.Result{}, err
		}

		// Update spec with selected instance type
		nodeClass.Spec.InstanceProfile = instanceTypes[0].Name
		if err := c.client.Update(ctx, nodeClass); err != nil {
			return reconcile.Result{}, fmt.Errorf("updating nodeclass spec: %w", err)
		}

		// Get fresh copy after spec update
		if err := c.client.Get(ctx, req.NamespacedName, nodeClass); err != nil {
			return reconcile.Result{}, fmt.Errorf("getting updated nodeclass: %w", err)
		}

		// Update status with selected instance types
		var selectedTypes []string
		for _, it := range instanceTypes {
			selectedTypes = append(selectedTypes, it.Name)
		}
		nodeClass.Status.SelectedInstanceTypes = selectedTypes

		c.updateCondition(nodeClass, ConditionTypeAutoPlacement, metav1.ConditionTrue, "InstanceTypeSelectionSucceeded", "Instance type selection completed successfully")

		if err := c.client.Status().Update(ctx, nodeClass); err != nil {
			return reconcile.Result{}, fmt.Errorf("updating nodeclass status: %w", err)
		}

		c.log.Info("Instance type selection completed",
			"nodeclass", req.Name,
			"instanceProfile", nodeClass.Spec.InstanceProfile,
			"selectedTypes", nodeClass.Status.SelectedInstanceTypes)

		InstanceTypeSelections.WithLabelValues(nodeClass.Name, "success").Inc()
		InstanceTypeSelectionLatency.WithLabelValues(nodeClass.Name).Observe(time.Since(start).Seconds())
		SelectedInstanceTypes.WithLabelValues(nodeClass.Name).Set(float64(len(nodeClass.Status.SelectedInstanceTypes)))
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
