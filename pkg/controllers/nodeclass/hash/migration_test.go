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
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/utils/nodeclass"
)

func TestHashMigrationResumesAfterPartialWrite(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	scheme.AddKnownTypes(schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}, &karpv1.NodeClaim{}, &karpv1.NodeClaimList{})
	class := &v1alpha1.IBMNodeClass{ObjectMeta: metav1.ObjectMeta{Name: "class"}, Spec: v1alpha1.IBMNodeClassSpec{Region: "us-south", Image: "image"}}
	legacy, err := nodeclass.LegacyHash(class)
	require.NoError(t, err)
	projected, err := nodeclass.ProvisioningHash(class)
	require.NoError(t, err)
	class.Annotations = map[string]string{v1alpha1.AnnotationIBMNodeClassHash: legacy, v1alpha1.AnnotationIBMNodeClassHashVersion: "1"}
	claims := []client.Object{}
	for _, name := range []string{"a", "b", "drifted"} {
		hash := legacy
		if name == "drifted" {
			hash = "older-provisioning-config"
		}
		claims = append(claims, &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{v1alpha1.AnnotationIBMNodeClassHash: hash, v1alpha1.AnnotationIBMNodeClassHashVersion: "1"}}, Spec: karpv1.NodeClaimSpec{NodeClassRef: &karpv1.NodeClassReference{Name: class.Name, Group: v1alpha1.Group, Kind: "IBMNodeClass"}}})
	}
	fail := true
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(claims, class)...).WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if obj.GetName() == "b" && fail {
				fail = false
				return fmt.Errorf("write interrupted")
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	}).Build()
	controller, err := NewController(kube)
	require.NoError(t, err)
	request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(class)}
	_, err = controller.Reconcile(ctx, request)
	require.Error(t, err)
	current := &v1alpha1.IBMNodeClass{}
	require.NoError(t, kube.Get(ctx, request.NamespacedName, current))
	require.Equal(t, "1", current.Annotations[v1alpha1.AnnotationIBMNodeClassHashVersion])
	_, err = controller.Reconcile(ctx, request)
	require.NoError(t, err)
	for _, name := range []string{"a", "b"} {
		claim := &karpv1.NodeClaim{}
		require.NoError(t, kube.Get(ctx, client.ObjectKey{Name: name}, claim))
		require.Equal(t, projected, claim.Annotations[v1alpha1.AnnotationIBMNodeClassHash])
		require.Equal(t, v1alpha1.IBMNodeClassHashVersion, claim.Annotations[v1alpha1.AnnotationIBMNodeClassHashVersion])
	}
	drifted := &karpv1.NodeClaim{}
	require.NoError(t, kube.Get(ctx, client.ObjectKey{Name: "drifted"}, drifted))
	require.Equal(t, "1", drifted.Annotations[v1alpha1.AnnotationIBMNodeClassHashVersion])
	require.Equal(t, "older-provisioning-config", drifted.Annotations[v1alpha1.AnnotationIBMNodeClassHash])
	require.NoError(t, kube.Get(ctx, request.NamespacedName, current))
	require.Equal(t, projected, current.Annotations[v1alpha1.AnnotationIBMNodeClassHash])
}

func TestHashMigrationSkipsDriftedClaimsAndStopsAfterVersionBump(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	scheme.AddKnownTypes(schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}, &karpv1.NodeClaim{}, &karpv1.NodeClaimList{})
	class := &v1alpha1.IBMNodeClass{ObjectMeta: metav1.ObjectMeta{Name: "class"}, Spec: v1alpha1.IBMNodeClassSpec{Region: "us-south", Image: "image"}}
	legacy, err := nodeclass.LegacyHash(class)
	require.NoError(t, err)
	class.Annotations = map[string]string{v1alpha1.AnnotationIBMNodeClassHash: legacy, v1alpha1.AnnotationIBMNodeClassHashVersion: "1"}
	claim := &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "drifted", Annotations: map[string]string{v1alpha1.AnnotationIBMNodeClassHash: legacy, v1alpha1.AnnotationIBMNodeClassHashVersion: "1"}},
		Spec: karpv1.NodeClaimSpec{NodeClassRef: &karpv1.NodeClassReference{Name: class.Name, Group: v1alpha1.Group, Kind: "IBMNodeClass"}}}
	claim.StatusConditions().SetTrue(karpv1.ConditionTypeDrifted)
	lists := 0
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(claim, class).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			lists++
			return c.List(ctx, list, opts...)
		},
	}).Build()
	controller, err := NewController(kube)
	require.NoError(t, err)
	request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(class)}
	_, err = controller.Reconcile(ctx, request)
	require.NoError(t, err)
	current := &karpv1.NodeClaim{}
	require.NoError(t, kube.Get(ctx, client.ObjectKeyFromObject(claim), current))
	require.Equal(t, "1", current.Annotations[v1alpha1.AnnotationIBMNodeClassHashVersion])

	stamped := &v1alpha1.IBMNodeClass{}
	require.NoError(t, kube.Get(ctx, request.NamespacedName, stamped))
	stamped.Spec.Image = "replacement-image"
	require.NoError(t, kube.Update(ctx, stamped))
	listsBefore := lists
	_, err = controller.Reconcile(ctx, request)
	require.NoError(t, err)
	require.Equal(t, listsBefore, lists)
	require.NoError(t, kube.Get(ctx, request.NamespacedName, stamped))
	require.Empty(t, stamped.Annotations[nodeclass.HashMigrationAnnotation])
}
