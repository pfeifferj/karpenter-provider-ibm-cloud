//go:build e2e
// +build e2e

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

package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

func TestDriftClaimDeletionRequiresActualEvidence(t *testing.T) {
	claim := &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "original", Finalizers: []string{karpv1.TerminationFinalizer}}}
	kube := fake.NewClientBuilder().WithScheme(cleanupTestScheme(t)).WithObjects(claim).Build()
	done, err := nodeClaimDeletingOrAbsent(t.Context(), kube, claim.Name)
	require.NoError(t, err)
	require.False(t, done, "a successful Get of an active claim must keep waiting")

	require.NoError(t, kube.Delete(t.Context(), claim))
	done, err = nodeClaimDeletingOrAbsent(t.Context(), kube, claim.Name)
	require.NoError(t, err)
	require.True(t, done, "ordinary deletion with its finalizer intact is evidence of termination")

	done, err = nodeClaimDeletingOrAbsent(t.Context(), kube, "absent")
	require.NoError(t, err)
	require.True(t, done)

	failing := interceptor.NewClient(kube, interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return fmt.Errorf("API unavailable")
		},
	})
	done, err = nodeClaimDeletingOrAbsent(t.Context(), failing, claim.Name)
	require.Error(t, err)
	require.False(t, done)
}

func driftReadyClaim(name string, uid types.UID) *karpv1.NodeClaim {
	claim := &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{
		Name: name, UID: uid,
		Labels:      map[string]string{"test-name": "drift", karpv1.NodePoolLabelKey: "pool", corev1.LabelTopologyZone: "us-south-2"},
		Annotations: map[string]string{v1alpha1.AnnotationIBMNodeClaimSubnetID: "new-subnet", v1alpha1.AnnotationIBMNodeClaimSecurityGroups: "sg-default,sg-original"},
	}}
	claim.Status.ProviderID = "ibm:///us-south/" + name
	claim.StatusConditions().SetTrue("Ready")
	return claim
}

func TestDriftReplacementExcludesEveryPreMutationClaim(t *testing.T) {
	original := driftReadyClaim("original", "original-uid")
	alternate := driftReadyClaim("another-ready-claim", "alternate-uid")
	pending := driftReadyClaim("initial-pending-claim", "pending-uid")
	pending.Status.Conditions = nil
	foreign := driftReadyClaim("other-pool", "foreign-uid")
	foreign.Labels[karpv1.NodePoolLabelKey] = "foreign"
	kube := fake.NewClientBuilder().WithScheme(cleanupTestScheme(t)).WithStatusSubresource(&karpv1.NodeClaim{}).WithObjects(original, alternate, pending, foreign).Build()
	initialUIDs, err := driftClaimUIDs(t.Context(), kube, "drift", "pool")
	require.NoError(t, err)
	require.Len(t, initialUIDs, 3)
	require.Contains(t, initialUIDs, original.UID)
	require.Contains(t, initialUIDs, alternate.UID)
	require.Contains(t, initialUIDs, pending.UID)
	require.NotContains(t, initialUIDs, foreign.UID)

	pending.StatusConditions().SetTrue("Ready")
	require.NoError(t, kube.Status().Update(t.Context(), pending))
	suite := &E2ETestSuite{kubeClient: kube}
	for _, desired := range []driftReplacementPlacement{
		{Subnet: "new-subnet", Zone: "us-south-2"},
		{SecurityGroups: []string{"sg-original", "sg-default"}},
	} {
		candidate, findErr := suite.findDriftReplacement(t.Context(), "drift", "pool", initialUIDs, desired)
		require.NoError(t, findErr)
		require.Nil(t, candidate, "an alternate pre-mutation Ready claim must not count as replacement")
		newClaim := driftReadyClaim("replacement", "replacement-uid")
		require.NoError(t, kube.Create(t.Context(), newClaim))
		candidate, findErr = suite.findDriftReplacement(t.Context(), "drift", "pool", initialUIDs, desired)
		require.NoError(t, findErr)
		require.NotNil(t, candidate)
		require.Equal(t, newClaim.UID, candidate.UID)
		require.NoError(t, kube.Delete(t.Context(), newClaim))
	}
}

func TestDriftReplacementRequiresReadyAndDesiredPlacement(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*karpv1.NodeClaim)
	}{
		{"launched without Ready", func(claim *karpv1.NodeClaim) {
			claim.Status.Conditions = nil
			claim.StatusConditions().SetTrue("Launched")
		}},
		{"missing provider ID", func(claim *karpv1.NodeClaim) { claim.Status.ProviderID = "" }},
		{"deleting Ready claim", func(claim *karpv1.NodeClaim) {
			now := metav1.Now()
			claim.DeletionTimestamp = &now
			claim.Finalizers = []string{karpv1.TerminationFinalizer}
		}},
		{"wrong subnet", func(claim *karpv1.NodeClaim) {
			claim.Annotations[v1alpha1.AnnotationIBMNodeClaimSubnetID] = "old-subnet"
		}},
		{"wrong zone", func(claim *karpv1.NodeClaim) { claim.Labels[corev1.LabelTopologyZone] = "us-south-1" }},
		{"wrong groups", func(claim *karpv1.NodeClaim) {
			claim.Annotations[v1alpha1.AnnotationIBMNodeClaimSecurityGroups] = "sg-original"
		}},
		{"extra groups", func(claim *karpv1.NodeClaim) {
			claim.Annotations[v1alpha1.AnnotationIBMNodeClaimSecurityGroups] += ",sg-extra"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claim := driftReadyClaim("replacement", "new-uid")
			tc.mutate(claim)
			kube := fake.NewClientBuilder().WithScheme(cleanupTestScheme(t)).WithObjects(claim).Build()
			suite := &E2ETestSuite{kubeClient: kube}
			candidate, err := suite.findDriftReplacement(t.Context(), "drift", "pool", map[types.UID]struct{}{"original-uid": {}}, driftReplacementPlacement{Subnet: "new-subnet", Zone: "us-south-2", SecurityGroups: []string{"sg-original", "sg-default"}})
			require.NoError(t, err)
			require.Nil(t, candidate)
		})
	}
}

func TestDriftClaimReadsPropagateAPIError(t *testing.T) {
	kube := fake.NewClientBuilder().WithScheme(cleanupTestScheme(t)).Build()
	failing := interceptor.NewClient(kube, interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
		return fmt.Errorf("API unavailable")
	}})
	uids, err := driftClaimUIDs(t.Context(), failing, "drift", "pool")
	require.Error(t, err)
	require.Nil(t, uids)
	suite := &E2ETestSuite{kubeClient: failing}
	candidate, err := suite.findDriftReplacement(t.Context(), "drift", "pool", nil, driftReplacementPlacement{})
	require.Error(t, err)
	require.Nil(t, candidate)
}

func TestNodeClaimReadinessRequiresReadyCondition(t *testing.T) {
	suite := &E2ETestSuite{}
	claim := driftReadyClaim("worker", "claim-uid")
	claim.Status.Conditions = nil
	claim.StatusConditions().SetTrue("Launched")
	require.False(t, suite.isNodeClaimReady(*claim))
	claim.StatusConditions().SetTrue("Ready")
	require.True(t, suite.isNodeClaimReady(*claim))
	claim.StatusConditions().SetFalse("Ready", "NotReady", "worker is not ready")
	require.False(t, suite.isNodeClaimReady(*claim))
}

type driftNetworkFixture struct {
	subnet    *vpcv1.Subnet
	vpc       *vpcv1.VPC
	err       error
	reads     int
	vpcID     string
	groupID   string
	requested string
}

func (f *driftNetworkFixture) GetSubnet(_ context.Context, id string) (*vpcv1.Subnet, error) {
	f.reads++
	f.requested = id
	return f.subnet, f.err
}

func (f *driftNetworkFixture) GetVPC(_ context.Context, id, group string) (*vpcv1.VPC, error) {
	f.reads++
	f.vpcID, f.groupID = id, group
	return f.vpc, f.err
}

func driftFixtureSubnet() *vpcv1.Subnet {
	return &vpcv1.Subnet{
		ID: core.StringPtr("alternate"), VPC: &vpcv1.VPCReference{ID: core.StringPtr("test-vpc")},
		Zone:   &vpcv1.ZoneReference{Name: core.StringPtr("us-south-2")},
		Status: core.StringPtr(vpcv1.SubnetStatusAvailableConst), AvailableIpv4AddressCount: core.Int64Ptr(10),
	}
}

func TestDriftSubnetUsesVerifiedConfiguredAlternative(t *testing.T) {
	pool := &karpv1.NodePool{}
	fixture := &driftNetworkFixture{subnet: driftFixtureSubnet()}
	subnet, err := alternateDriftSubnet(t.Context(), fixture, "us-south-1=original, us-south-2=alternate", "original", "test-vpc", "us-south", pool)
	require.NoError(t, err)
	require.Same(t, fixture.subnet, subnet)
	require.Equal(t, "alternate", fixture.requested)
	require.Equal(t, 1, fixture.reads)

	pool.Spec.Template.Spec.Requirements = []karpv1.NodeSelectorRequirementWithMinValues{{
		Key: corev1.LabelTopologyZone, Operator: corev1.NodeSelectorOpIn, Values: []string{"us-south-1"},
	}}
	fixture.reads = 0
	subnet, err = alternateDriftSubnet(t.Context(), fixture, "us-south-1=original,us-south-2=alternate", "original", "test-vpc", "us-south", pool)
	require.NoError(t, err)
	require.Nil(t, subnet)
	require.Zero(t, fixture.reads)
}

func TestDriftSubnetRejectsUnprovenNetworkIdentity(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*vpcv1.Subnet)
	}{
		{name: "foreign VPC", mutate: func(subnet *vpcv1.Subnet) { subnet.VPC.ID = core.StringPtr("foreign") }},
		{name: "wrong zone", mutate: func(subnet *vpcv1.Subnet) { subnet.Zone.Name = core.StringPtr("us-south-3") }},
		{name: "wrong ID", mutate: func(subnet *vpcv1.Subnet) { subnet.ID = core.StringPtr("different") }},
		{name: "missing zone", mutate: func(subnet *vpcv1.Subnet) { subnet.Zone = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := &driftNetworkFixture{subnet: driftFixtureSubnet()}
			test.mutate(fixture.subnet)
			subnet, err := alternateDriftSubnet(t.Context(), fixture, "us-south-2=alternate", "original", "test-vpc", "us-south", &karpv1.NodePool{})
			require.Error(t, err)
			require.Nil(t, subnet)
		})
	}
	fixture := &driftNetworkFixture{err: fmt.Errorf("API unavailable")}
	subnet, err := alternateDriftSubnet(t.Context(), fixture, "us-south-2=alternate", "original", "test-vpc", "us-south", &karpv1.NodePool{})
	require.Error(t, err)
	require.Nil(t, subnet)

	fixture = &driftNetworkFixture{subnet: driftFixtureSubnet()}
	fixture.subnet.AvailableIpv4AddressCount = core.Int64Ptr(0)
	subnet, err = alternateDriftSubnet(t.Context(), fixture, "us-south-2=alternate", "original", "test-vpc", "us-south", &karpv1.NodePool{})
	require.NoError(t, err)
	require.Nil(t, subnet)
}

func TestDriftSecurityGroupComesFromExactVPC(t *testing.T) {
	fixture := &driftNetworkFixture{vpc: &vpcv1.VPC{
		ID: core.StringPtr("test-vpc"), DefaultSecurityGroup: &vpcv1.SecurityGroupReference{ID: core.StringPtr("default-group")},
	}}
	group, err := defaultDriftSecurityGroup(t.Context(), fixture, "test-vpc", "resource-group")
	require.NoError(t, err)
	require.Equal(t, "default-group", group)
	require.Equal(t, "test-vpc", fixture.vpcID)
	require.Equal(t, "resource-group", fixture.groupID)

	fixture.vpc.ID = core.StringPtr("foreign-vpc")
	group, err = defaultDriftSecurityGroup(t.Context(), fixture, "test-vpc", "resource-group")
	require.Error(t, err)
	require.Empty(t, group)
}
