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

package hash

import (
	"context"
	"fmt"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/utils/nodeclass"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"testing"
)

func TestPartialMigrationPreservesPolicyOnlyChanges(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	scheme.AddKnownTypes(schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}, &karpv1.NodeClaim{}, &karpv1.NodeClaimList{})
	class := &v1alpha1.IBMNodeClass{ObjectMeta: metav1.ObjectMeta{Name: "review-class"}, Spec: v1alpha1.IBMNodeClassSpec{Region: "us-south", Image: "image", IKSDynamicPools: &v1alpha1.IKSDynamicPoolConfig{CleanupPolicy: &v1alpha1.IKSPoolCleanupPolicy{EmptyPoolTTL: "5m"}}}}
	oldHash, err := nodeclass.LegacyHash(class)
	require.NoError(t, err)
	projected, err := nodeclass.ProvisioningHash(class)
	require.NoError(t, err)
	class.Annotations = map[string]string{v1alpha1.AnnotationIBMNodeClassHash: oldHash, v1alpha1.AnnotationIBMNodeClassHashVersion: "1"}
	objects := []client.Object{class}
	for _, name := range []string{"a", "b"} {
		objects = append(objects, &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{v1alpha1.AnnotationIBMNodeClassHash: oldHash, v1alpha1.AnnotationIBMNodeClassHashVersion: "1"}}, Spec: karpv1.NodeClaimSpec{NodeClassRef: &karpv1.NodeClassReference{Name: class.Name, Group: v1alpha1.Group, Kind: "IBMNodeClass"}}})
	}
	fail := true
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
		if obj.GetName() == "b" && fail {
			fail = false
			return fmt.Errorf("interrupted")
		}
		return c.Patch(ctx, obj, patch, opts...)
	}}).Build()
	ctrl, err := NewController(kube)
	require.NoError(t, err)
	req := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(class)}
	_, err = ctrl.Reconcile(ctx, req)
	require.Error(t, err)
	latest := &v1alpha1.IBMNodeClass{}
	require.NoError(t, kube.Get(ctx, req.NamespacedName, latest))
	latest.Spec.IKSDynamicPools.CleanupPolicy.EmptyPoolTTL = "10m"
	require.NoError(t, kube.Update(ctx, latest))
	newProjected, err := nodeclass.ProvisioningHash(latest)
	require.NoError(t, err)
	require.Equal(t, projected, newProjected)
	_, err = ctrl.Reconcile(ctx, req)
	require.NoError(t, err)
	remaining := &karpv1.NodeClaim{}
	require.NoError(t, kube.Get(ctx, client.ObjectKey{Name: "b"}, remaining))
	require.Equal(t, v1alpha1.IBMNodeClassHashVersion, remaining.Annotations[v1alpha1.AnnotationIBMNodeClassHashVersion])
	require.Equal(t, projected, remaining.Annotations[v1alpha1.AnnotationIBMNodeClassHash])
}
