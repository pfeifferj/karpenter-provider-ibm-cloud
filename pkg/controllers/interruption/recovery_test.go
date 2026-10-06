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

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestInterruptionRetriesBeforeAcknowledgement(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker", UID: types.UID("worker-uid"), CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)), Labels: map[string]string{"karpenter.sh/nodepool": "managed"}},
		Spec:   corev1.NodeSpec{ProviderID: "ibm://account///cluster/worker"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue}, {Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
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
	controller := NewController(kube, nil, nil, nil)
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

func TestInterruptionRetriesLegacyAcknowledgement(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{InterruptionAnnotation: "true", InterruptionReasonAnnotation: string(HostMaintenance)}}}
	controller := &Controller{}
	interrupted, reason := controller.isNodeInterrupted(context.Background(), node)
	require.True(t, interrupted)
	require.Equal(t, HostMaintenance, reason)
}
