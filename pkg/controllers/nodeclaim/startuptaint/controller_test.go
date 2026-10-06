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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

func startupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	scheme.AddKnownTypes(gv, &karpv1.NodeClaim{}, &karpv1.NodeClaimList{})
	metav1.AddToGroupVersion(scheme, gv)
	return scheme
}

func TestStartupTaintMigrationPreservesCoreOwnership(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "deleting"}[deleting], func(t *testing.T) {
			ctx := context.Background()
			claim := &karpv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim", UID: "claim-uid",
					Finalizers: []string{karpv1.TerminationFinalizer, StartupTaintLifecycleFinalizer, "example.com/finalizer"},
					Labels: map[string]string{
						StartupTaintsAppliedLabel:                  "true",
						RegularTaintsAppliedLabel:                  "true",
						"karpenter-ibm.sh/startup-taint-lifecycle": "true",
						karpv1.NodePoolLabelKey:                    "pool",
					},
				},
				Spec: karpv1.NodeClaimSpec{
					NodeClassRef:  &karpv1.NodeClassReference{Group: v1alpha1.Group, Kind: "IBMNodeClass", Name: "class"},
					StartupTaints: []corev1.Taint{{Key: "example.com/startup", Effect: corev1.TaintEffectNoSchedule}},
					Taints:        []corev1.Taint{{Key: "example.com/regular", Effect: corev1.TaintEffectNoSchedule}},
				},
				Status: karpv1.NodeClaimStatus{NodeName: "node"},
			}
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node"},
				Spec: corev1.NodeSpec{Taints: []corev1.Taint{
					{Key: "example.com/startup", Effect: corev1.TaintEffectNoSchedule},
					{Key: "karpenter-ibm.sh/provisioned", Effect: corev1.TaintEffectNoSchedule},
				}},
				Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
			}
			if deleting {
				claim.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			}
			kubeClient := fake.NewClientBuilder().WithScheme(startupScheme()).WithObjects(claim, node).WithStatusSubresource(claim, node).Build()
			c := NewController(kubeClient)
			_, err := c.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(claim)})
			require.NoError(t, err)
			updatedClaim, updatedNode := &karpv1.NodeClaim{}, &corev1.Node{}
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(claim), updatedClaim))
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(node), updatedNode))
			require.Equal(t, []string{karpv1.TerminationFinalizer, "example.com/finalizer"}, updatedClaim.Finalizers)
			require.Equal(t, map[string]string{karpv1.NodePoolLabelKey: "pool"}, updatedClaim.Labels)
			require.Equal(t, claim.Status, updatedClaim.Status)
			require.Equal(t, node.Spec.Taints, updatedNode.Spec.Taints)
		})
	}
}

func TestStartupTaintMigrationDoesNotAddMetadata(t *testing.T) {
	ctx := context.Background()
	claim := &karpv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim"},
		Spec:       karpv1.NodeClaimSpec{NodeClassRef: &karpv1.NodeClassReference{Group: v1alpha1.Group, Kind: "IBMNodeClass", Name: "class"}},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(startupScheme()).WithObjects(claim).Build()
	_, err := NewController(kubeClient).Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(claim)})
	require.NoError(t, err)
	updated := &karpv1.NodeClaim{}
	require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(claim), updated))
	require.Empty(t, updated.Finalizers)
	require.Empty(t, updated.Labels)
}

func TestStartupTaintMigrationIgnoresForeignClaims(t *testing.T) {
	ctx := context.Background()
	claim := &karpv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim", Finalizers: []string{StartupTaintLifecycleFinalizer}},
		Spec:       karpv1.NodeClaimSpec{NodeClassRef: &karpv1.NodeClassReference{Group: "other.example.com", Kind: "OtherClass", Name: "class"}},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(startupScheme()).WithObjects(claim).Build()
	_, err := NewController(kubeClient).Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(claim)})
	require.NoError(t, err)
	updated := &karpv1.NodeClaim{}
	require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(claim), updated))
	require.Equal(t, claim.Finalizers, updated.Finalizers)
}

func TestStartupTaintMigrationRemovesLegacyNodeLabels(t *testing.T) {
	ctx := context.Background()
	legacy := map[string]string{StartupTaintsAppliedLabel: "true", RegularTaintsAppliedLabel: "true", karpv1.NodePoolLabelKey: "pool"}
	claim := &karpv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim", UID: "claim-uid", Labels: map[string]string{karpv1.NodePoolLabelKey: "pool"}},
		Spec:       karpv1.NodeClaimSpec{NodeClassRef: &karpv1.NodeClassReference{Group: v1alpha1.Group, Kind: "IBMNodeClass", Name: "class"}},
		Status:     karpv1.NodeClaimStatus{ProviderID: "ibm:///us-south/instance", NodeName: "node"},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", Labels: legacy}, Spec: corev1.NodeSpec{ProviderID: claim.Status.ProviderID}}
	kubeClient := fake.NewClientBuilder().WithScheme(startupScheme()).WithObjects(claim, node).Build()
	_, err := NewController(kubeClient).Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(claim)})
	require.NoError(t, err)
	updated := &corev1.Node{}
	require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(node), updated))
	require.Equal(t, map[string]string{karpv1.NodePoolLabelKey: "pool"}, updated.Labels)
}
