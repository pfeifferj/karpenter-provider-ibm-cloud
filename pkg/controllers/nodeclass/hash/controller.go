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
		return reconcile.Result{}, fmt.Errorf("failed to compute hash: %w", err)
	}

	// Debug logging
	fmt.Printf("DEBUG: Computed hash: %d (type: %T)\n", hash, hash)
	fmt.Printf("DEBUG: Current specHash: %d\n", nc.Status.SpecHash)

	// Update status if hash changed
	if nc.Status.SpecHash != hash {
		patch := client.MergeFrom(nc.DeepCopy())
		nc.Status.SpecHash = hash
		fmt.Printf("DEBUG: Setting specHash to: %d\n", nc.Status.SpecHash)
		if err := c.kubeClient.Status().Patch(ctx, nc, patch); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to patch status: %w", err)
		}
		fmt.Printf("DEBUG: Successfully patched status\n")
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
