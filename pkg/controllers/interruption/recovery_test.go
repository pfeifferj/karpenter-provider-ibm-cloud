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

package interruption

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cache"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestInterruptionRetriesBeforeAcknowledgement(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker", UID: types.UID("worker-uid"), Finalizers: []string{karpv1.TerminationFinalizer}, CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)), Labels: map[string]string{"karpenter.sh/nodepool": "managed"}},
		Spec:   corev1.NodeSpec{ProviderID: "ibm://account///cluster/worker"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
	node.Annotations = map[string]string{"ibm-cloud.kubernetes.io/maintenance": "true"}
	first := true
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if first {
				first = false
				return fmt.Errorf("temporary write failure")
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	}).Build()
	controller := NewController(kube, nil, nil)
	_, err := controller.Reconcile(ctx)
	require.Error(t, err)
	current := &corev1.Node{}
	require.NoError(t, kube.Get(ctx, client.ObjectKeyFromObject(node), current))
	require.Empty(t, current.Annotations[InterruptionCompletedAnnotation])
	_, err = controller.Reconcile(ctx)
	require.NoError(t, err)
	require.NoError(t, kube.Get(ctx, client.ObjectKeyFromObject(node), current))
	require.True(t, current.Spec.Unschedulable)
	require.Equal(t, "true", current.Annotations[InterruptionCompletedAnnotation])
}

func TestLegacyAnnotationWithoutCurrentSignalDoesNotTrigger(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{InterruptionAnnotation: "true", InterruptionReasonAnnotation: string(HostMaintenance)}}}
	controller := &Controller{}
	interrupted, _ := controller.isNodeInterrupted(context.Background(), node)
	require.False(t, interrupted)
}

func TestKubeletConditionsAreLeftToNodeRepair(t *testing.T) {
	for _, condition := range []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(time.Now())},
		{Type: corev1.NodeReady, Status: corev1.ConditionUnknown, LastTransitionTime: metav1.NewTime(time.Now())},
		{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue},
		{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue},
		{Type: corev1.NodeNetworkUnavailable, Status: corev1.ConditionTrue},
	} {
		t.Run(string(condition.Type)+"="+string(condition.Status), func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour))},
				Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{condition}},
			}
			interrupted, _ := (&Controller{}).isNodeInterrupted(context.Background(), node)
			require.False(t, interrupted)
		})
	}
}

func TestReadyNodeProvisioningAnnotationsDoNotTriggerReplacement(t *testing.T) {
	for _, annotations := range []map[string]string{
		{"karpenter-ibm.sh/vpc-launch": `{"CapacityType":"on-demand","Capacity":{"cpu":"2","memory":"8Gi"},"Subnet":"network-subnet"}`},
		{"karpenter-ibm.sh/iks-allocation": `{"Request":{"Labels":{"description":"network capacity"}}}`},
		{"ibm.example.com/description": "network capacity for this application"},
		{"ibm-cloud.kubernetes.io/status": "capacity available"},
		{"ibm-cloud.kubernetes.io/error": "network resources available"},
		{InterruptionReasonAnnotation: string(CapacityUnavailable)},
		{InterruptionAnnotation: "true", InterruptionReasonAnnotation: "unknown-capacity-network-event"},
	} {
		t.Run(fmt.Sprint(annotations), func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ready-worker", UID: "ready-worker-uid", CreationTimestamp: metav1.NewTime(time.Now().Add(-35 * time.Second)),
					Finalizers: []string{karpv1.TerminationFinalizer}, Annotations: annotations,
					Labels: map[string]string{karpv1.NodePoolLabelKey: "managed", karpv1.CapacityTypeLabelKey: karpv1.CapacityTypeOnDemand, corev1.LabelInstanceTypeStable: "bx4-2x8", corev1.LabelTopologyZone: "us-south-1"},
				},
				Spec:   corev1.NodeSpec{ProviderID: "ibm:///us-south/worker-id"},
				Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
			}
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
			offerings := cache.NewUnavailableOfferings()
			controller := NewController(kube, nil, offerings)
			_, err := controller.Reconcile(ctx)
			require.NoError(t, err)
			current := &corev1.Node{}
			require.NoError(t, kube.Get(ctx, client.ObjectKeyFromObject(node), current))
			require.Nil(t, current.DeletionTimestamp)
			require.False(t, current.Spec.Unschedulable)
			require.Empty(t, current.Annotations[InterruptionCompletedAnnotation])
			require.False(t, offerings.IsUnavailable("bx4-2x8:us-south-1:on-demand"))
		})
	}
}
