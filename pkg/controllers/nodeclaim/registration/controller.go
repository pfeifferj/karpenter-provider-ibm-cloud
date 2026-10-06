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

package registration

import (
	"context"
	"fmt"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/apis"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

const (
	NodeClaimRegistrationFinalizer = "registration.nodeclaim.ibm.sh/finalizer"
	RegisteredLabel                = "karpenter.sh/registered"
	InitializedLabel               = "karpenter.sh/initialized"
	NodePoolLabel                  = "karpenter.sh/nodepool"
	NodeClassLabel                 = "karpenter-ibm.sh/ibmnodeclass"
	ProvisionerLabel               = "provisioner"
	ProvisionedTaint               = "karpenter-ibm.sh/provisioned"
)

type Controller struct {
	kubeClient client.Client
	apiReader  client.Reader
}

func NewController(kubeClient client.Client, readers ...client.Reader) (*Controller, error) {
	if kubeClient == nil {
		return nil, fmt.Errorf("kubernetes client cannot be nil")
	}
	reader := client.Reader(kubeClient)
	if len(readers) > 0 && readers[0] != nil {
		reader = readers[0]
	}
	return &Controller{kubeClient: kubeClient, apiReader: reader}, nil
}

func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	nodeClaim := &karpv1.NodeClaim{}
	if err := c.apiReader.Get(ctx, req.NamespacedName, nodeClaim); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if nodeClaim.Spec.NodeClassRef == nil || nodeClaim.Spec.NodeClassRef.Group != v1alpha1.Group || nodeClaim.Spec.NodeClassRef.Kind != "IBMNodeClass" {
		return reconcile.Result{}, nil
	}
	if controllerutil.ContainsFinalizer(nodeClaim, NodeClaimRegistrationFinalizer) {
		stored := nodeClaim.DeepCopy()
		controllerutil.RemoveFinalizer(nodeClaim, NodeClaimRegistrationFinalizer)
		if err := c.kubeClient.Patch(ctx, nodeClaim, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
			if apierrors.IsConflict(err) {
				return reconcile.Result{RequeueAfter: time.Millisecond}, nil
			}
			return reconcile.Result{}, client.IgnoreNotFound(err)
		}
	}
	// Waiting for registration belongs to core; this controller only migrates nodes that already have one.
	node, err := c.findNodeForNodeClaim(ctx, nodeClaim)
	if err != nil || node == nil {
		return reconcile.Result{}, err
	}
	node, err = c.removeLegacyNodeFinalizer(ctx, node)
	if err != nil {
		if apierrors.IsConflict(err) {
			return reconcile.Result{RequeueAfter: time.Millisecond}, nil
		}
		return reconcile.Result{}, err
	}
	if node == nil || !nodeClaim.DeletionTimestamp.IsZero() || !node.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}
	// The removed provider controller set Registered itself, so core registration returned early
	// and never ran syncNode, which is where the termination finalizer and owner reference come from.
	if !nodeClaim.StatusConditions().Get(karpv1.ConditionTypeRegistered).IsTrue() {
		return reconcile.Result{}, nil
	}
	stored := node.DeepCopy()
	controllerutil.AddFinalizer(node, karpv1.TerminationFinalizer)
	if !slices.ContainsFunc(node.OwnerReferences, func(ref metav1.OwnerReference) bool { return ref.UID == nodeClaim.UID }) {
		node.OwnerReferences = append(node.OwnerReferences, metav1.OwnerReference{
			APIVersion: apis.Group + "/v1", Kind: "NodeClaim", Name: nodeClaim.Name, UID: nodeClaim.UID, BlockOwnerDeletion: ptr.To(true),
		})
	}
	if equality.Semantic.DeepEqual(stored, node) {
		return reconcile.Result{}, nil
	}
	if err := c.kubeClient.Patch(ctx, node, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
		if apierrors.IsConflict(err) {
			return reconcile.Result{RequeueAfter: time.Millisecond}, nil
		}
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	return reconcile.Result{}, nil
}

func (c *Controller) findNodeForNodeClaim(ctx context.Context, nodeClaim *karpv1.NodeClaim) (*corev1.Node, error) {
	if nodeClaim.Status.ProviderID == "" || nodeClaim.Status.NodeName == "" {
		return nil, nil
	}
	node := &corev1.Node{}
	if err := c.apiReader.Get(ctx, client.ObjectKey{Name: nodeClaim.Status.NodeName}, node); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	if node.Spec.ProviderID != nodeClaim.Status.ProviderID {
		return nil, nil
	}
	return node, nil
}

func (c *Controller) removeLegacyNodeFinalizer(ctx context.Context, node *corev1.Node) (*corev1.Node, error) {
	if !controllerutil.ContainsFinalizer(node, NodeClaimRegistrationFinalizer) {
		return node, nil
	}
	current := &corev1.Node{}
	if err := c.apiReader.Get(ctx, client.ObjectKeyFromObject(node), current); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	if current.UID != node.UID || current.Spec.ProviderID != node.Spec.ProviderID {
		return nil, fmt.Errorf("node identity changed while cleaning registration finalizer")
	}
	if !controllerutil.ContainsFinalizer(current, NodeClaimRegistrationFinalizer) {
		return current, nil
	}
	stored := current.DeepCopy()
	controllerutil.RemoveFinalizer(current, NodeClaimRegistrationFinalizer)
	if err := c.kubeClient.Patch(ctx, current, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	return current, nil
}

func (c *Controller) reconcileNode(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	node := &corev1.Node{}
	if err := c.apiReader.Get(ctx, req.NamespacedName, node); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	_, err := c.removeLegacyNodeFinalizer(ctx, node)
	return reconcile.Result{}, err
}

func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	if err := builder.ControllerManagedBy(m).
		Named("nodeclaim.registration.ibm").
		For(&karpv1.NodeClaim{}).
		Complete(c); err != nil {
		return err
	}
	return builder.ControllerManagedBy(m).
		Named("node.registrationfinalizer.ibm").
		For(&corev1.Node{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(object client.Object) bool {
			return controllerutil.ContainsFinalizer(object, NodeClaimRegistrationFinalizer)
		})).
		Complete(reconcile.Func(c.reconcileNode))
}
