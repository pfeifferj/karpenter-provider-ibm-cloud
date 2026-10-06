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

package garbagecollection

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/awslabs/operatorpkg/reconciler"
	"github.com/awslabs/operatorpkg/singleton"
	"go.uber.org/multierr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/operator/injection"
)

type Controller struct {
	kubeClient      client.Client
	apiReader       client.Reader
	cloudProvider   cloudprovider.CloudProvider
	successfulCount uint64
}

func NewController(kubeClient client.Client, cloudProvider cloudprovider.CloudProvider, readers ...client.Reader) *Controller {
	reader := client.Reader(kubeClient)
	if len(readers) > 0 && readers[0] != nil {
		reader = readers[0]
	}
	return &Controller{
		kubeClient:    kubeClient,
		apiReader:     reader,
		cloudProvider: cloudProvider,
	}
}

func (c *Controller) Reconcile(ctx context.Context) (reconciler.Result, error) {
	ctx = injection.WithControllerName(ctx, "nodeclaim.garbagecollection.ibm")
	cloudNodeClaims, err := c.cloudProvider.List(ctx)
	if err != nil {
		return reconciler.Result{}, fmt.Errorf("listing cloudprovider nodeclaims: %w", err)
	}
	nodes := &corev1.NodeList{}
	if err := c.apiReader.List(ctx, nodes); err != nil {
		return reconciler.Result{}, err
	}
	if err := c.handleOrphanedNodes(ctx, nodes, cloudNodeClaims); err != nil {
		return reconciler.Result{}, err
	}
	count := atomic.AddUint64(&c.successfulCount, 1)
	interval := 2 * time.Minute
	if count <= 20 {
		interval = 10 * time.Second
	}
	return reconciler.Result{RequeueAfter: interval}, nil
}

func (c *Controller) handleOrphanedNodes(ctx context.Context, nodes *corev1.NodeList, cloudNodeClaims []*karpv1.NodeClaim) error {
	present := map[string]bool{}
	for _, nc := range cloudNodeClaims {
		present[c.normalizeProviderID(nc.Status.ProviderID)] = true
	}
	var candidates []*corev1.Node
	var errs []error
	for i := range nodes.Items {
		node := &nodes.Items[i]
		if !strings.HasPrefix(node.Spec.ProviderID, "ibm://") || !c.isKarpenterManagedNode(node) || nodeReady(node) || present[c.normalizeProviderID(node.Spec.ProviderID)] {
			continue
		}
		_, err := c.cloudProvider.Get(ctx, node.Spec.ProviderID)
		if cloudprovider.IsNodeClaimNotFoundError(err) {
			candidates = append(candidates, node)
		} else if err != nil {
			errs = append(errs, fmt.Errorf("confirming instance absence for node %s: %w", node.Name, err))
		}
	}
	if err := multierr.Combine(errs...); err != nil {
		return err
	}
	for _, candidate := range candidates {
		node := &corev1.Node{}
		if err := c.apiReader.Get(ctx, client.ObjectKeyFromObject(candidate), node); err != nil {
			if client.IgnoreNotFound(err) != nil {
				errs = append(errs, err)
			}
			continue
		}
		if node.UID != candidate.UID || node.Spec.ProviderID != candidate.Spec.ProviderID || nodeReady(node) || !c.isKarpenterManagedNode(node) || !node.DeletionTimestamp.IsZero() {
			continue
		}
		_, err := c.cloudProvider.Get(ctx, node.Spec.ProviderID)
		if !cloudprovider.IsNodeClaimNotFoundError(err) {
			if err != nil {
				errs = append(errs, fmt.Errorf("rechecking instance absence for node %s: %w", node.Name, err))
			}
			continue
		}
		if err := c.kubeClient.Delete(ctx, node, deletePreconditions(node)); client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
		}
	}
	return multierr.Combine(errs...)
}

func (c *Controller) isKarpenterManagedNode(node *corev1.Node) bool {
	_, hasPool := node.Labels[karpv1.NodePoolLabelKey]
	_, hasClass := node.Labels["karpenter-ibm.sh/ibmnodeclass"]
	return hasPool || hasClass
}

func nodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func deletePreconditions(object client.Object) *client.DeleteOptions {
	uid := object.GetUID()
	version := object.GetResourceVersion()
	return &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}}
}

func (c *Controller) normalizeProviderID(providerID string) string {
	parts := strings.Split(providerID, "/")
	if len(parts) < 4 {
		return providerID
	}
	region := parts[len(parts)-2]
	instanceID := parts[len(parts)-1]
	if strings.Contains(instanceID, "_") {
		instanceID = strings.SplitN(instanceID, "_", 2)[1]
	}
	return fmt.Sprintf("ibm:///%s/%s", region, instanceID)
}

func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(m).
		Named("nodeclaim.garbagecollection.ibm").
		WatchesRawSource(singleton.Source()).
		Complete(singleton.AsReconciler(c))
}
