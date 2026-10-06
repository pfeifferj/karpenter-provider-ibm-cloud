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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

func getTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	scheme.AddKnownTypes(gv, &karpv1.NodeClaim{}, &karpv1.NodeClaimList{})
	metav1.AddToGroupVersion(scheme, gv)
	_ = v1alpha1.AddToScheme(scheme)
	return scheme
}

func registrationClaim() *karpv1.NodeClaim {
	return &karpv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim", UID: "claim-uid"},
		Spec: karpv1.NodeClaimSpec{
			NodeClassRef:  &karpv1.NodeClassReference{Group: v1alpha1.Group, Kind: "IBMNodeClass", Name: "class"},
			StartupTaints: []corev1.Taint{{Key: "example.com/startup", Effect: corev1.TaintEffectNoSchedule}},
		},
		Status: karpv1.NodeClaimStatus{ProviderID: "ibm:///us-south/instance", NodeName: "node"},
	}
}

func registrationNode() *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node", UID: "node-uid"},
		Spec: corev1.NodeSpec{
			ProviderID: "ibm:///us-south/instance",
			Taints: []corev1.Taint{
				{Key: "example.com/startup", Effect: corev1.TaintEffectNoSchedule},
				{Key: karpv1.UnregisteredTaintKey, Effect: corev1.TaintEffectNoExecute},
			},
		},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	}
}

func TestRegistrationPreservesCoreLifecycle(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		t.Run(map[bool]string{false: "waiting for initialization", true: "initialized"}[initialized], func(t *testing.T) {
			ctx := context.Background()
			claim, node := registrationClaim(), registrationNode()
			if initialized {
				claim.StatusConditions().SetTrue(karpv1.ConditionTypeRegistered)
				claim.StatusConditions().SetTrue(karpv1.ConditionTypeInitialized)
			}
			storedTaints := append([]corev1.Taint(nil), node.Spec.Taints...)
			kubeClient := fake.NewClientBuilder().WithScheme(getTestScheme()).WithStatusSubresource(claim, node).WithObjects(claim, node).Build()
			persisted := &karpv1.NodeClaim{}
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(claim), persisted))
			storedStatus := persisted.Status.DeepCopy()
			c, err := NewController(kubeClient)
			require.NoError(t, err)
			_, err = c.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(claim)})
			require.NoError(t, err)
			updatedClaim, updatedNode := &karpv1.NodeClaim{}, &corev1.Node{}
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(claim), updatedClaim))
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(node), updatedNode))
			require.Equal(t, *storedStatus, updatedClaim.Status)
			require.Equal(t, storedTaints, updatedNode.Spec.Taints)
			require.NotContains(t, updatedNode.Labels, InitializedLabel)
			require.NotContains(t, updatedNode.Labels, RegisteredLabel)
			require.Empty(t, updatedClaim.Finalizers)
		})
	}
}

func TestRegistrationMigratesOnlyItsFinalizer(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "deleting"}[deleting], func(t *testing.T) {
			ctx := context.Background()
			claim, node := registrationClaim(), registrationNode()
			claim.Finalizers = []string{karpv1.TerminationFinalizer, NodeClaimRegistrationFinalizer, "example.com/finalizer"}
			node.Finalizers = []string{karpv1.TerminationFinalizer, NodeClaimRegistrationFinalizer, "loadbalancer.nodeclaim.ibm.sh/finalizer"}
			if deleting {
				claim.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				node.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			}
			kubeClient := fake.NewClientBuilder().WithScheme(getTestScheme()).WithObjects(claim, node).Build()
			c, err := NewController(kubeClient)
			require.NoError(t, err)
			_, err = c.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(claim)})
			require.NoError(t, err)
			updated := &karpv1.NodeClaim{}
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(claim), updated))
			require.Equal(t, []string{karpv1.TerminationFinalizer, "example.com/finalizer"}, updated.Finalizers)
			updatedNode := &corev1.Node{}
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(node), updatedNode))
			require.Equal(t, []string{karpv1.TerminationFinalizer, "loadbalancer.nodeclaim.ibm.sh/finalizer"}, updatedNode.Finalizers)
			require.Equal(t, node.Spec.Taints, updatedNode.Spec.Taints)
		})
	}
}

func TestRegistrationRequiresMatchingProviderIdentity(t *testing.T) {
	ctx := context.Background()
	claim, node := registrationClaim(), registrationNode()
	node.Spec.ProviderID = "ibm:///us-south/replacement"
	kubeClient := fake.NewClientBuilder().WithScheme(getTestScheme()).WithObjects(claim, node).Build()
	c, err := NewController(kubeClient)
	require.NoError(t, err)
	claim.StatusConditions().SetTrue(karpv1.ConditionTypeRegistered)
	require.NoError(t, kubeClient.Update(ctx, claim))
	_, err = c.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(claim)})
	require.NoError(t, err)
	updated := &corev1.Node{}
	require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(node), updated))
	require.Empty(t, updated.Finalizers)
	require.Empty(t, updated.OwnerReferences)
}

func TestRegistrationNeverUsesClaimNameAsNodeIdentity(t *testing.T) {
	ctx := context.Background()
	claim, node := registrationClaim(), registrationNode()
	claim.Status.NodeName = ""
	node.Name = claim.Name
	node.Spec.ProviderID = "ibm:///us-south/unrelated"
	kubeClient := fake.NewClientBuilder().WithScheme(getTestScheme()).WithObjects(claim, node).Build()
	c, err := NewController(kubeClient)
	require.NoError(t, err)
	found, err := c.findNodeForNodeClaim(ctx, claim)
	require.NoError(t, err)
	require.Nil(t, found)
}

func TestRegistrationUsesFreshReader(t *testing.T) {
	ctx := context.Background()
	claim, staleNode := registrationClaim(), registrationNode()
	freshNode := staleNode.DeepCopy()
	freshNode.UID = types.UID("replacement-uid")
	freshNode.Spec.ProviderID = "ibm:///us-south/replacement"
	freshReader := fake.NewClientBuilder().WithScheme(getTestScheme()).WithObjects(claim, freshNode).Build()
	writes := 0
	cached := fake.NewClientBuilder().WithScheme(getTestScheme()).WithObjects(claim, staleNode).WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
			writes++
			return nil
		},
	}).Build()
	c, err := NewController(cached, freshReader)
	require.NoError(t, err)
	_, err = c.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(claim)})
	require.NoError(t, err)
	require.Zero(t, writes)
}

func TestRegistrationIgnoresOtherProviderClaims(t *testing.T) {
	ctx := context.Background()
	claim := registrationClaim()
	claim.Spec.NodeClassRef.Group = "other.example.com"
	claim.Finalizers = []string{NodeClaimRegistrationFinalizer}
	kubeClient := fake.NewClientBuilder().WithScheme(getTestScheme()).WithObjects(claim).Build()
	c, err := NewController(kubeClient)
	require.NoError(t, err)
	_, err = c.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(claim)})
	require.NoError(t, err)
	updated := &karpv1.NodeClaim{}
	require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(claim), updated))
	require.Equal(t, claim.Finalizers, updated.Finalizers)
}

func TestLegacyNodeFinalizerCleanupWithoutClaim(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "deleting"}[deleting], func(t *testing.T) {
			ctx := context.Background()
			node := registrationNode()
			node.Finalizers = []string{karpv1.TerminationFinalizer, NodeClaimRegistrationFinalizer, "loadbalancer.nodeclaim.ibm.sh/finalizer"}
			node.Labels = map[string]string{InitializedLabel: "true", "example.com/owned": "preserved"}
			if deleting {
				node.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			}
			kube := fake.NewClientBuilder().WithScheme(getTestScheme()).WithObjects(node).Build()
			c, err := NewController(kube)
			require.NoError(t, err)
			_, err = c.reconcileNode(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(node)})
			require.NoError(t, err)
			updated := &corev1.Node{}
			require.NoError(t, kube.Get(ctx, client.ObjectKeyFromObject(node), updated))
			require.Equal(t, []string{karpv1.TerminationFinalizer, "loadbalancer.nodeclaim.ibm.sh/finalizer"}, updated.Finalizers)
			require.Equal(t, node.Labels, updated.Labels)
			require.Equal(t, node.Spec.Taints, updated.Spec.Taints)
		})
	}
}

func TestLegacyNodeFinalizerCleanupRequiresFreshIdentity(t *testing.T) {
	ctx := context.Background()
	node := registrationNode()
	node.Finalizers = []string{NodeClaimRegistrationFinalizer, karpv1.TerminationFinalizer}
	reads, writes := 0, 0
	reader := fake.NewClientBuilder().WithScheme(getTestScheme()).WithObjects(node).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if err := c.Get(ctx, key, obj, opts...); err != nil {
				return err
			}
			reads++
			if reads > 1 {
				obj.SetUID("replacement-uid")
				obj.(*corev1.Node).Spec.ProviderID = "ibm:///us-south/replacement"
			}
			return nil
		},
	}).Build()
	kube := fake.NewClientBuilder().WithScheme(getTestScheme()).WithObjects(node).WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
			writes++
			return nil
		},
	}).Build()
	c, err := NewController(kube, reader)
	require.NoError(t, err)
	_, err = c.reconcileNode(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(node)})
	require.Error(t, err)
	require.Zero(t, writes)
}

func TestLegacyNodeFinalizerCleanupPreservesOtherFinalizers(t *testing.T) {
	ctx := context.Background()
	node := registrationNode()
	node.Finalizers = []string{karpv1.TerminationFinalizer, "loadbalancer.nodeclaim.ibm.sh/finalizer"}
	kube := fake.NewClientBuilder().WithScheme(getTestScheme()).WithObjects(node).Build()
	c, err := NewController(kube)
	require.NoError(t, err)
	_, err = c.reconcileNode(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(node)})
	require.NoError(t, err)
	updated := &corev1.Node{}
	require.NoError(t, kube.Get(ctx, client.ObjectKeyFromObject(node), updated))
	require.Equal(t, node.Finalizers, updated.Finalizers)
}

func TestRegistrationRepairsLegacyRegisteredNode(t *testing.T) {
	for _, registered := range []bool{false, true} {
		t.Run(map[bool]string{false: "core still registering", true: "registered by removed controller"}[registered], func(t *testing.T) {
			ctx := context.Background()
			claim, node := registrationClaim(), registrationNode()
			if registered {
				claim.StatusConditions().SetTrue(karpv1.ConditionTypeRegistered)
			}
			kubeClient := fake.NewClientBuilder().WithScheme(getTestScheme()).WithObjects(claim, node).Build()
			c, err := NewController(kubeClient)
			require.NoError(t, err)
			_, err = c.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(claim)})
			require.NoError(t, err)
			updated := &corev1.Node{}
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(node), updated))
			if !registered {
				require.Empty(t, updated.Finalizers)
				require.Empty(t, updated.OwnerReferences)
				return
			}
			require.Equal(t, []string{karpv1.TerminationFinalizer}, updated.Finalizers)
			require.Len(t, updated.OwnerReferences, 1)
			require.Equal(t, metav1.OwnerReference{APIVersion: "karpenter.sh/v1", Kind: "NodeClaim", Name: claim.Name, UID: claim.UID, BlockOwnerDeletion: ptr.To(true)}, updated.OwnerReferences[0])
		})
	}
}
