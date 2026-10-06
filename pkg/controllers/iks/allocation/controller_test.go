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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/providers/iks/workerpool"
)

type cleanupProvider struct {
	cleanupCalls int
	confirmCalls int
	result       error
}

func (p *cleanupProvider) Cleanup(context.Context, *v1.NodeClaim) error {
	p.cleanupCalls++
	return p.result
}
func (p *cleanupProvider) ConfirmGone(context.Context, *v1.NodeClaim) error {
	p.confirmCalls++
	return p.result
}

func TestCleanupControllerPreservesNormalTerminationAndTypedAbsence(t *testing.T) {
	for _, test := range []struct {
		name       string
		providerID string
		result     error
		release    bool
	}{
		{name: "pending allocation", result: nil},
		{name: "bound waits for core cloud deletion", providerID: "ibm://account///cluster/worker", result: nil},
		{name: "pending uncertainty", result: fmt.Errorf("read timeout")},
		{name: "bound uncertainty", providerID: "ibm://account///cluster/worker", result: fmt.Errorf("HTTP 429")},
		{name: "pending confirmed absence", result: cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("absent")), release: true},
		{name: "bound confirmed absence", providerID: "ibm://account///cluster/worker", result: cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("absent")), release: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			scheme.AddKnownTypes(schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}, &v1.NodeClaim{}, &v1.NodeClaimList{})
			claim := &v1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "claim", UID: "claim-uid", Finalizers: []string{workerpool.AllocationFinalizer, v1.TerminationFinalizer}}, Status: v1.NodeClaimStatus{ProviderID: test.providerID}}
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(claim).WithObjects(claim).Build()
			require.NoError(t, kubeClient.Delete(context.Background(), claim))
			provider := &cleanupProvider{result: test.result}
			controller := NewController(kubeClient, kubeClient, nil)
			controller.SetProvider(provider)
			result, err := controller.Reconcile(context.Background(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(claim)})
			if test.result != nil && !test.release {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if test.providerID == "" {
				require.Equal(t, 1, provider.cleanupCalls)
				require.Zero(t, provider.confirmCalls)
			} else {
				require.Equal(t, 1, provider.confirmCalls)
				require.Zero(t, provider.cleanupCalls)
			}
			fresh := &v1.NodeClaim{}
			require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKeyFromObject(claim), fresh))
			require.Equal(t, !test.release, slices.Contains(fresh.Finalizers, workerpool.AllocationFinalizer))
			require.Contains(t, fresh.Finalizers, v1.TerminationFinalizer)
			if test.result == nil {
				require.Positive(t, result.RequeueAfter)
			}
		})
	}
}
