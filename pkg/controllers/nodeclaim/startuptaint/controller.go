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

package startuptaint

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

const (
	StartupTaintLifecycleFinalizer = "startuptaint.nodeclaim.ibm.sh/finalizer"
	StartupTaintsAppliedLabel      = "karpenter-ibm.sh/startup-taints-applied"
	RegularTaintsAppliedLabel      = "karpenter-ibm.sh/regular-taints-applied"
	CiliumNotReadyTaint            = "node.cilium.io/agent-not-ready"
	NodeNotReadyTaint              = "node.kubernetes.io/not-ready"
)

type Controller struct {
	kubeClient client.Client
	apiReader  client.Reader
}

func NewController(kubeClient client.Client, readers ...client.Reader) *Controller {
	reader := client.Reader(kubeClient)
	if len(readers) > 0 && readers[0] != nil {
		reader = readers[0]
	}
	return &Controller{kubeClient: kubeClient, apiReader: reader}
}

func (c *Controller) Register(_ context.Context, mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).
		Named("startuptaint.lifecycle").
		For(&karpv1.NodeClaim{}).
		Complete(c)
}

func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	nodeClaim := &karpv1.NodeClaim{}
	if err := c.apiReader.Get(ctx, req.NamespacedName, nodeClaim); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if nodeClaim.Spec.NodeClassRef == nil || nodeClaim.Spec.NodeClassRef.Group != v1alpha1.Group || nodeClaim.Spec.NodeClassRef.Kind != "IBMNodeClass" {
		return reconcile.Result{}, nil
	}
	// Core registration copied NodeClaim labels onto the Node, so legacy labels are removed from both.
	if err := c.removeNodeLabels(ctx, nodeClaim); err != nil {
		if apierrors.IsConflict(err) {
			return reconcile.Result{RequeueAfter: time.Millisecond}, nil
		}
		return reconcile.Result{}, err
	}
	stored := nodeClaim.DeepCopy()
	changed := controllerutil.RemoveFinalizer(nodeClaim, StartupTaintLifecycleFinalizer)
	changed = removeLegacyLabels(nodeClaim.Labels) || changed
	if !changed {
		return reconcile.Result{}, nil
	}
	if err := c.kubeClient.Patch(ctx, nodeClaim, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
		if apierrors.IsConflict(err) {
			return reconcile.Result{RequeueAfter: time.Millisecond}, nil
		}
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	return reconcile.Result{}, nil
}

func removeLegacyLabels(labels map[string]string) bool {
	changed := false
	for _, label := range []string{StartupTaintsAppliedLabel, RegularTaintsAppliedLabel, "karpenter-ibm.sh/startup-taint-lifecycle"} {
		if _, ok := labels[label]; ok {
			delete(labels, label)
			changed = true
		}
	}
	return changed
}

func (c *Controller) removeNodeLabels(ctx context.Context, nodeClaim *karpv1.NodeClaim) error {
	if nodeClaim.Status.NodeName == "" || nodeClaim.Status.ProviderID == "" {
		return nil
	}
	node := &corev1.Node{}
	if err := c.apiReader.Get(ctx, client.ObjectKey{Name: nodeClaim.Status.NodeName}, node); err != nil {
		return client.IgnoreNotFound(err)
	}
	if node.Spec.ProviderID != nodeClaim.Status.ProviderID {
		return nil
	}
	stored := node.DeepCopy()
	if !removeLegacyLabels(node.Labels) {
		return nil
	}
	return client.IgnoreNotFound(c.kubeClient.Patch(ctx, node, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})))
}
