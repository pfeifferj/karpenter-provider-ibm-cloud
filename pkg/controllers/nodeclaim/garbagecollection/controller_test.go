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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

type mockCloudProvider struct {
	mu                 sync.Mutex
	nodeClaims         map[string]*karpv1.NodeClaim
	deletedProviderIDs []string
	listError          error
	deleteError        error
	getError           error
}

func newMockCloudProvider() *mockCloudProvider {
	return &mockCloudProvider{
		nodeClaims:         make(map[string]*karpv1.NodeClaim),
		deletedProviderIDs: []string{},
	}
}

func (m *mockCloudProvider) Create(ctx context.Context, nodeClaim *karpv1.NodeClaim) (*karpv1.NodeClaim, error) {
	return nodeClaim, nil
}

func (m *mockCloudProvider) Delete(ctx context.Context, nodeClaim *karpv1.NodeClaim) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.deleteError != nil {
		return m.deleteError
	}
	m.deletedProviderIDs = append(m.deletedProviderIDs, nodeClaim.Status.ProviderID)
	delete(m.nodeClaims, nodeClaim.Status.ProviderID)
	return nil
}

func (m *mockCloudProvider) Get(ctx context.Context, providerID string) (*karpv1.NodeClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.getError != nil {
		return nil, m.getError
	}
	nc, ok := m.nodeClaims[providerID]
	if !ok {
		return nil, cloudprovider.NewNodeClaimNotFoundError(errors.New("nodeclaim not found"))
	}
	return nc, nil
}

func (m *mockCloudProvider) List(ctx context.Context) ([]*karpv1.NodeClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.listError != nil {
		return nil, m.listError
	}
	result := make([]*karpv1.NodeClaim, 0, len(m.nodeClaims))
	for _, nc := range m.nodeClaims {
		result = append(result, nc)
	}
	return result, nil
}

func (m *mockCloudProvider) IsDrifted(ctx context.Context, nodeClaim *karpv1.NodeClaim) (cloudprovider.DriftReason, error) {
	return "", nil
}

func (m *mockCloudProvider) Name() string {
	return "mock"
}

func (m *mockCloudProvider) GetInstanceTypes(ctx context.Context, nodePool *karpv1.NodePool) ([]*cloudprovider.InstanceType, error) {
	return nil, nil
}

func (m *mockCloudProvider) GetSupportedNodeClasses() []status.Object {
	return []status.Object{&v1alpha1.IBMNodeClass{}}
}

func (m *mockCloudProvider) RepairPolicies() []cloudprovider.RepairPolicy {
	return []cloudprovider.RepairPolicy{}
}

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = corev1.AddToScheme(s)

	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	s.AddKnownTypes(gv,
		&karpv1.NodeClaim{},
		&karpv1.NodeClaimList{},
		&karpv1.NodePool{},
		&karpv1.NodePoolList{},
	)
	metav1.AddToGroupVersion(s, gv)

	ibmGV := schema.GroupVersion{Group: "karpenter-ibm.sh", Version: "v1alpha1"}
	s.AddKnownTypes(ibmGV,
		&v1alpha1.IBMNodeClass{},
		&v1alpha1.IBMNodeClassList{},
	)
	metav1.AddToGroupVersion(s, ibmGV)

	return s
}

func testNodeClaim(name, providerID string, creationTime time.Time) *karpv1.NodeClaim {
	return &karpv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(creationTime),
		},
		Spec: karpv1.NodeClaimSpec{
			NodeClassRef: &karpv1.NodeClassReference{
				Group: "karpenter-ibm.sh",
				Kind:  "IBMNodeClass",
				Name:  "test-nodeclass",
			},
		},
		Status: karpv1.NodeClaimStatus{
			ProviderID: providerID,
		},
	}
}

func testNode(name, providerID string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.NodeSpec{
			ProviderID: providerID,
		},
	}
}

type emptyInventoryProvider struct{ *mockCloudProvider }

func (p *emptyInventoryProvider) List(context.Context) ([]*karpv1.NodeClaim, error) {
	return nil, p.listError
}

func managedNode(name string) *corev1.Node {
	node := testNode(name, "ibm:///us-south/"+name)
	node.UID = types.UID(name + "-uid")
	node.Labels = map[string]string{karpv1.NodePoolLabelKey: "pool"}
	return node
}

func TestUnavailableInventoryPreservesNodes(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(map[bool]string{false: "not ready", true: "ready"}[ready], func(t *testing.T) {
			ctx := context.Background()
			node := managedNode("live-node")
			node.Finalizers = []string{karpv1.TerminationFinalizer}
			if ready {
				node.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}
			}
			claim := testNodeClaim("live-claim", node.Spec.ProviderID, time.Now().Add(-time.Hour))
			claim.Status.NodeName = node.Name
			provider := &emptyInventoryProvider{newMockCloudProvider()}
			provider.listError = context.DeadlineExceeded
			kubeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(node, claim).Build()
			_, err := NewController(kubeClient, provider).Reconcile(ctx)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			updated := &corev1.Node{}
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(node), updated))
			require.Equal(t, node.Finalizers, updated.Finalizers)
			require.True(t, updated.DeletionTimestamp.IsZero())
			require.Empty(t, provider.deletedProviderIDs)
		})
	}
}

func TestIncompleteInventoryRequiresDirectConfirmation(t *testing.T) {
	for _, lookup := range []string{"live", "timeout", "ready", "missing"} {
		t.Run(lookup, func(t *testing.T) {
			ctx := context.Background()
			node := managedNode("node")
			node.Finalizers = []string{karpv1.TerminationFinalizer, "loadbalancer.nodeclaim.ibm.sh/finalizer", "example.com/finalizer"}
			provider := &emptyInventoryProvider{newMockCloudProvider()}
			switch lookup {
			case "live":
				provider.nodeClaims[node.Spec.ProviderID] = testNodeClaim("claim", node.Spec.ProviderID, time.Now())
			case "timeout":
				provider.getError = context.DeadlineExceeded
			case "ready":
				node.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}
			}
			kubeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(node).Build()
			_, err := NewController(kubeClient, provider).Reconcile(ctx)
			if lookup == "timeout" {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			} else {
				require.NoError(t, err)
			}
			updated := &corev1.Node{}
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(node), updated))
			require.Equal(t, node.Finalizers, updated.Finalizers)
			require.Equal(t, lookup == "missing", !updated.DeletionTimestamp.IsZero())
			require.Empty(t, provider.deletedProviderIDs)
		})
	}
}

func TestTerminatingClaimsRetainGraceAndFinalizers(t *testing.T) {
	for _, grace := range []*metav1.Duration{nil, {Duration: time.Hour}} {
		name := "indefinite grace"
		if grace != nil {
			name = "hour grace"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			node := managedNode("draining-node")
			node.Finalizers = []string{karpv1.TerminationFinalizer}
			node.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}
			claim := testNodeClaim("draining-claim", node.Spec.ProviderID, time.Now().Add(-time.Hour))
			claim.DeletionTimestamp = &metav1.Time{Time: time.Now().Add(-11 * time.Minute)}
			claim.Finalizers = []string{karpv1.TerminationFinalizer, "loadbalancer.nodeclaim.ibm.sh/finalizer"}
			claim.Spec.TerminationGracePeriod = grace
			claim.Status.NodeName = node.Name
			claim.StatusConditions().SetTrue(karpv1.ConditionTypeRegistered)
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "protected-pod", Namespace: "default", Labels: map[string]string{"app": "protected"}}, Spec: corev1.PodSpec{NodeName: node.Name}}
			pdb := &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "pdb", Namespace: "default"}, Spec: policyv1.PodDisruptionBudgetSpec{MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "protected"}}}}
			kubeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(node, claim, pod, pdb).Build()
			provider := newMockCloudProvider()
			provider.deleteError = context.DeadlineExceeded
			provider.nodeClaims[node.Spec.ProviderID] = claim
			_, err := NewController(kubeClient, provider).Reconcile(ctx)
			require.NoError(t, err)
			updatedClaim := &karpv1.NodeClaim{}
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(claim), updatedClaim))
			require.Equal(t, claim.Finalizers, updatedClaim.Finalizers)
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(node), &corev1.Node{}))
			require.Empty(t, provider.deletedProviderIDs)
		})
	}
}

func TestOrphanDeletionCarriesIdentityPreconditions(t *testing.T) {
	ctx := context.Background()
	node := managedNode("node")
	node.Finalizers = []string{karpv1.TerminationFinalizer}
	calls := 0
	kubeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(node).WithInterceptorFuncs(interceptor.Funcs{
		Delete: func(ctx context.Context, base client.WithWatch, object client.Object, options ...client.DeleteOption) error {
			calls++
			opts := (&client.DeleteOptions{}).ApplyOptions(options)
			require.NotNil(t, opts.Preconditions)
			require.Equal(t, object.GetUID(), *opts.Preconditions.UID)
			require.Equal(t, object.GetResourceVersion(), *opts.Preconditions.ResourceVersion)
			return base.Delete(ctx, object, options...)
		},
	}).Build()
	_, err := NewController(kubeClient, newMockCloudProvider()).Reconcile(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
}

func TestOrphanCleanupUsesFreshNodeState(t *testing.T) {
	ctx := context.Background()
	candidate := managedNode("node")
	fresh := candidate.DeepCopy()
	fresh.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}
	reader := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(fresh).Build()
	kubeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(candidate).Build()
	c := NewController(kubeClient, newMockCloudProvider(), reader)
	require.NoError(t, c.handleOrphanedNodes(ctx, &corev1.NodeList{Items: []corev1.Node{*candidate}}, nil))
	require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(candidate), &corev1.Node{}))
}

func TestOrphanCleanupIgnoresUnmanagedNodes(t *testing.T) {
	ctx := context.Background()
	node := testNode("foreign", "ibm:///us-south/foreign")
	kubeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(node).Build()
	_, err := NewController(kubeClient, newMockCloudProvider()).Reconcile(ctx)
	require.NoError(t, err)
	require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(node), &corev1.Node{}))
}

func TestNodeDeletionWithoutFinalizersCompletes(t *testing.T) {
	ctx := context.Background()
	node := managedNode("missing")
	kubeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(node).Build()
	_, err := NewController(kubeClient, newMockCloudProvider()).Reconcile(ctx)
	require.NoError(t, err)
	require.True(t, apierrors.IsNotFound(kubeClient.Get(ctx, client.ObjectKeyFromObject(node), &corev1.Node{})))
}
