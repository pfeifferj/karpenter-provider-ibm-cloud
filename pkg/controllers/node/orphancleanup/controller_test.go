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

package orphancleanup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/platform-services-go-sdk/globaltaggingv1"
	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/go-openapi/strfmt"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	mock_ibm "github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm/mock"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/utils/ownership"
)

type mockGlobalTaggingAPI struct {
	response *globaltaggingv1.TagList
	pages    map[int64]*globaltaggingv1.TagList
	err      error
	offsets  []int64
}

func (m *mockGlobalTaggingAPI) ListTagsWithContext(_ context.Context, options *globaltaggingv1.ListTagsOptions) (*globaltaggingv1.TagList, *core.DetailedResponse, error) {
	m.offsets = append(m.offsets, *options.Offset)
	if m.pages != nil {
		return m.pages[*options.Offset], nil, m.err
	}
	return m.response, nil, m.err
}

type mockVPCClientProvider struct {
	vpcClient *ibm.VPCClient
	err       error
}

func (m *mockVPCClientProvider) GetVPCClient(context.Context) (*ibm.VPCClient, error) {
	return m.vpcClient, m.err
}

func orphanScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	scheme.AddKnownTypes(gv, &karpv1.NodeClaim{}, &karpv1.NodeClaimList{})
	metav1.AddToGroupVersion(scheme, gv)
	return scheme
}

func ownershipTags() *globaltaggingv1.TagList {
	result := &globaltaggingv1.TagList{}
	for key, value := range ownership.VPCTags("cluster-a-uid", "claim-uid", "class-uid") {
		result.Items = append(result.Items, globaltaggingv1.Tag{Name: core.StringPtr(key + ":" + value)})
	}
	return result
}

func ownedInstance(age time.Duration) *vpcv1.Instance {
	return &vpcv1.Instance{
		ID:        core.StringPtr("instance"),
		CRN:       core.StringPtr("crn:v1:bluemix:public:is:us-south:a/account::instance:instance"),
		CreatedAt: func() *strfmt.DateTime { value := strfmt.DateTime(time.Now().Add(-age)); return &value }(),
	}
}

func orphanClient(objects ...client.Object) client.Client {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: "cluster-a-uid"}}
	return fake.NewClientBuilder().WithScheme(orphanScheme()).WithObjects(append(objects, namespace)...).Build()
}

func TestOwnershipRequiresImmutableClusterIdentity(t *testing.T) {
	for _, scenario := range []string{"owned", "foreign", "legacy", "default cluster", "incomplete", "conflicting", "tag error", "unknown cluster"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("CLUSTER_NAME", "default")
			tags := ownershipTags()
			kubeClient := orphanClient()
			tagging := &mockGlobalTaggingAPI{response: tags}
			switch scenario {
			case "foreign":
				for i := range tags.Items {
					if *tags.Items[i].Name == ownership.ClusterUIDTag+":cluster-a-uid" {
						tags.Items[i].Name = core.StringPtr(ownership.ClusterUIDTag + ":cluster-b-uid")
					}
				}
			case "legacy":
				tagging.response = &globaltaggingv1.TagList{Items: []globaltaggingv1.Tag{{Name: core.StringPtr("karpenter.sh/managed:true")}}}
			case "default cluster":
				tagging.response = &globaltaggingv1.TagList{Items: []globaltaggingv1.Tag{{Name: core.StringPtr("karpenter.sh/cluster:default")}}}
			case "incomplete":
				for i := range tags.Items {
					if *tags.Items[i].Name == ownership.ClaimUIDTag+":claim-uid" {
						tags.Items[i].Name = core.StringPtr("user:unrelated")
					}
				}
			case "conflicting":
				tags.Items = append(tags.Items, globaltaggingv1.Tag{Name: core.StringPtr(ownership.ClusterUIDTag + ":cluster-b-uid")})
			case "tag error":
				tagging.err = context.DeadlineExceeded
			case "unknown cluster":
				kubeClient = fake.NewClientBuilder().WithScheme(orphanScheme()).Build()
			}
			c := &Controller{kubeClient: kubeClient, apiReader: kubeClient, globalTagging: tagging}
			require.Equal(t, scenario == "owned", owns(c, ownedInstance(time.Hour).CRN))
		})
	}
}

func TestOwnershipReadsAllTagPages(t *testing.T) {
	tags := ownershipTags()
	first := &globaltaggingv1.TagList{Items: tags.Items[:2], TotalCount: core.Int64Ptr(int64(len(tags.Items)))}
	second := &globaltaggingv1.TagList{Items: tags.Items[2:], TotalCount: core.Int64Ptr(int64(len(tags.Items)))}
	tagging := &mockGlobalTaggingAPI{pages: map[int64]*globaltaggingv1.TagList{0: first, 2: second}}
	kubeClient := orphanClient()
	c := &Controller{kubeClient: kubeClient, globalTagging: tagging}
	require.True(t, owns(c, ownedInstance(time.Hour).CRN))
	require.Equal(t, []int64{0, 2}, tagging.offsets)
}

func TestOrphanInstanceDeletionRequiresProofAndAge(t *testing.T) {
	for _, scenario := range []string{"orphan", "new", "unknown age", "live claim", "pending claim", "live node", "foreign tags", "cloud error", "fresh claim", "tag error"} {
		t.Run(scenario, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockVPC := mock_ibm.NewMockvpcClientInterface(ctrl)
			instance := ownedInstance(time.Hour)
			tagging := &mockGlobalTaggingAPI{response: ownershipTags()}
			var objects []client.Object
			if scenario == "new" {
				instance = ownedInstance(time.Minute)
			}
			if scenario == "unknown age" {
				instance.CreatedAt = nil
			}
			if scenario == "live claim" {
				objects = append(objects, &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "live", UID: "other-uid"}, Status: karpv1.NodeClaimStatus{ProviderID: "ibm:///us-south/instance"}})
			}
			if scenario == "pending claim" || scenario == "fresh claim" {
				objects = append(objects, &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "pending", UID: "claim-uid"}})
			}
			if scenario == "live node" {
				objects = append(objects, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}, Spec: corev1.NodeSpec{ProviderID: "ibm:///us-south/instance"}})
			}
			if scenario == "foreign tags" {
				for i := range tagging.response.Items {
					if *tagging.response.Items[i].Name == ownership.ClusterUIDTag+":cluster-a-uid" {
						tagging.response.Items[i].Name = core.StringPtr(ownership.ClusterUIDTag + ":cluster-b-uid")
					}
				}
			}
			if scenario == "tag error" {
				tagging.err = context.DeadlineExceeded
			}
			lookupErr := error(nil)
			if scenario == "cloud error" {
				lookupErr = errors.New("cloud lookup failed")
			}
			mockVPC.EXPECT().GetInstanceWithContext(gomock.Any(), gomock.Any()).Return(instance, &core.DetailedResponse{StatusCode: 200}, lookupErr)
			if scenario == "orphan" {
				mockVPC.EXPECT().DeleteInstanceWithContext(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, options *vpcv1.DeleteInstanceOptions) (*core.DetailedResponse, error) {
					require.Equal(t, "instance", *options.ID)
					return &core.DetailedResponse{StatusCode: 202}, nil
				})
			}
			kubeClient := orphanClient(objects...)
			apiReader := client.Reader(kubeClient)
			if scenario == "fresh claim" {
				kubeClient = orphanClient()
			}
			c := &Controller{kubeClient: kubeClient, apiReader: apiReader, ibmClient: &mockVPCClientProvider{vpcClient: ibm.NewVPCClientWithMock(mockVPC)}, globalTagging: tagging, orphanTimeout: DefaultOrphanTimeout}
			err := c.processOrphanedInstance(context.Background(), "instance")
			if scenario == "cloud error" || scenario == "tag error" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestOrphanInstancePreservesFailedDeletionForRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockVPC := mock_ibm.NewMockvpcClientInterface(ctrl)
	mockVPC.EXPECT().GetInstanceWithContext(gomock.Any(), gomock.Any()).Return(ownedInstance(time.Hour), &core.DetailedResponse{}, nil)
	mockVPC.EXPECT().DeleteInstanceWithContext(gomock.Any(), gomock.Any()).Return(nil, context.DeadlineExceeded)
	kubeClient := orphanClient()
	c := &Controller{kubeClient: kubeClient, ibmClient: &mockVPCClientProvider{vpcClient: ibm.NewVPCClientWithMock(mockVPC)}, globalTagging: &mockGlobalTaggingAPI{response: ownershipTags()}, orphanTimeout: DefaultOrphanTimeout}
	require.ErrorIs(t, c.processOrphanedInstance(context.Background(), "instance"), context.DeadlineExceeded)
}

func TestProviderIDExtractionRejectsIKSIdentity(t *testing.T) {
	c := &Controller{}
	require.Equal(t, "02u7_instance", c.extractInstanceIDFromProviderID("ibm:///us-south/02u7_instance"))
	for _, invalid := range []string{"ibm://account///cluster/worker", "ibm:///us-south/", "ibm:///instance", "other:///region/instance"} {
		require.Empty(t, c.extractInstanceIDFromProviderID(invalid))
	}
}

func TestOrphanTimeoutHasMinimum(t *testing.T) {
	t.Setenv("KARPENTER_ORPHAN_TIMEOUT_MINUTES", "1")
	require.Equal(t, MinimumOrphanTimeout, getOrphanTimeoutFromEnv())
	t.Setenv("KARPENTER_ORPHAN_TIMEOUT_MINUTES", "20")
	require.Equal(t, 20*time.Minute, getOrphanTimeoutFromEnv())
	t.Setenv("KARPENTER_ORPHAN_TIMEOUT_MINUTES", "invalid")
	require.Equal(t, DefaultOrphanTimeout, getOrphanTimeoutFromEnv())
}

func TestReconcileSkipsReferencedInstances(t *testing.T) {
	t.Setenv("KARPENTER_ENABLE_ORPHAN_CLEANUP", "true")
	ctrl := gomock.NewController(t)
	mockVPC := mock_ibm.NewMockvpcClientInterface(ctrl)
	orphan := ownedInstance(time.Hour)
	live := ownedInstance(time.Hour)
	live.ID = core.StringPtr("live-instance")
	mockVPC.EXPECT().ListInstancesWithContext(gomock.Any(), gomock.Any()).Return(&vpcv1.InstanceCollection{Instances: []vpcv1.Instance{*live, *orphan}}, &core.DetailedResponse{}, nil)
	mockVPC.EXPECT().GetInstanceWithContext(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, options *vpcv1.GetInstanceOptions) (*vpcv1.Instance, *core.DetailedResponse, error) {
		require.Equal(t, "instance", *options.ID)
		return orphan, &core.DetailedResponse{}, nil
	})
	mockVPC.EXPECT().DeleteInstanceWithContext(gomock.Any(), gomock.Any()).Return(&core.DetailedResponse{}, nil)
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "live"}, Spec: corev1.NodeSpec{ProviderID: "ibm:///us-south/live-instance"}}
	kubeClient := orphanClient(node)
	tagging := &mockGlobalTaggingAPI{response: ownershipTags()}
	c := &Controller{kubeClient: kubeClient, ibmClient: &mockVPCClientProvider{vpcClient: ibm.NewVPCClientWithMock(mockVPC)}, globalTagging: tagging, orphanTimeout: DefaultOrphanTimeout}
	result, err := c.Reconcile(context.Background())
	require.NoError(t, err)
	require.Equal(t, OrphanCheckInterval, result.RequeueAfter)
	require.Len(t, tagging.offsets, 2)
}

func TestReconcileRequiresClusterIdentity(t *testing.T) {
	t.Setenv("KARPENTER_ENABLE_ORPHAN_CLEANUP", "true")
	ctrl := gomock.NewController(t)
	mockVPC := mock_ibm.NewMockvpcClientInterface(ctrl)
	kubeClient := fake.NewClientBuilder().WithScheme(orphanScheme()).Build()
	c := &Controller{kubeClient: kubeClient, ibmClient: &mockVPCClientProvider{vpcClient: ibm.NewVPCClientWithMock(mockVPC)}, globalTagging: &mockGlobalTaggingAPI{response: ownershipTags()}, orphanTimeout: DefaultOrphanTimeout}
	_, err := c.Reconcile(context.Background())
	require.Error(t, err)
}

func owns(c *Controller, crn *string) bool {
	tags, err := c.instanceTags(context.Background(), crn)
	if err != nil {
		return false
	}
	owned, err := c.ownsTags(context.Background(), tags)
	return owned && err == nil
}
