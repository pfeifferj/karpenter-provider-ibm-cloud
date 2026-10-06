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

package allocation

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/providers/iks/workerpool"
)

type Provider interface {
	Cleanup(context.Context, *v1.NodeClaim) error
	ConfirmGone(context.Context, *v1.NodeClaim) error
}

type Controller struct {
	kubeClient client.Client
	apiReader  client.Reader
	provider   Provider
}

func NewController(kubeClient client.Client, apiReader client.Reader, ibmClient *ibm.Client) *Controller {
	provider, _ := workerpool.NewIKSWorkerPoolProvider(ibmClient, kubeClient, workerpool.WithAPIReader(apiReader))
	cleanupProvider, _ := provider.(Provider)
	if apiReader == nil {
		apiReader = kubeClient
	}
	return &Controller{kubeClient: kubeClient, apiReader: apiReader, provider: cleanupProvider}
}

func (c *Controller) SetProvider(provider Provider) { c.provider = provider }

func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	claim := &v1.NodeClaim{}
	if operationErr := c.apiReader.Get(ctx, req.NamespacedName, claim); operationErr != nil {
		return reconcile.Result{}, client.IgnoreNotFound(operationErr)
	}
	if claim.DeletionTimestamp.IsZero() || !controllerutil.ContainsFinalizer(claim, workerpool.AllocationFinalizer) {
		return reconcile.Result{}, nil
	}
	if c.provider == nil {
		return reconcile.Result{}, fmt.Errorf("IKS allocation provider is unavailable")
	}
	var err error
	if claim.Status.ProviderID == "" {
		err = c.provider.Cleanup(ctx, claim)
	} else {
		err = c.provider.ConfirmGone(ctx, claim)
	}
	if err != nil && !cloudprovider.IsNodeClaimNotFoundError(err) {
		return reconcile.Result{}, err
	}
	if !cloudprovider.IsNodeClaimNotFoundError(err) {
		return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
	}
	stored := claim.DeepCopy()
	controllerutil.RemoveFinalizer(claim, workerpool.AllocationFinalizer)
	if operationErr := c.kubeClient.Patch(ctx, claim, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); operationErr != nil {
		if apierrors.IsConflict(operationErr) {
			return reconcile.Result{RequeueAfter: time.Second}, nil
		}
		return reconcile.Result{}, client.IgnoreNotFound(operationErr)
	}
	return reconcile.Result{}, nil
}

func (c *Controller) Register(_ context.Context, mgr manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(mgr).Named("iks.allocation").For(&v1.NodeClaim{}).Complete(c)
}
