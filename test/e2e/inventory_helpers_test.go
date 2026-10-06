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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	mockibm "github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm/mock"
)

type inventoryFunc func(context.Context) ([]vpcv1.Instance, error)

func (f inventoryFunc) ListInstances(ctx context.Context) ([]vpcv1.Instance, error) { return f(ctx) }

func inventoryInstance(id, vpcID string) vpcv1.Instance {
	return vpcv1.Instance{ID: core.StringPtr(id), Name: core.StringPtr(id), VPC: &vpcv1.VPCReference{ID: core.StringPtr(vpcID)}}
}

func TestCloudInventoryIncludesBaselineAndEveryPageInExactVPC(t *testing.T) {
	mock := mockibm.NewMockvpcClientInterface(gomock.NewController(t))
	pages := 0
	mock.EXPECT().ListInstancesWithContext(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, options *vpcv1.ListInstancesOptions) (*vpcv1.InstanceCollection, *core.DetailedResponse, error) {
		_, bounded := ctx.Deadline()
		require.True(t, bounded)
		pages++
		if options.Start == nil {
			return &vpcv1.InstanceCollection{Instances: []vpcv1.Instance{
				inventoryInstance("control-plane", "test-vpc"), inventoryInstance("static-worker", "test-vpc"), inventoryInstance("foreign", "other-vpc"),
			}, Next: &vpcv1.PageLink{Href: core.StringPtr("https://example.com/instances?start=second")}}, nil, nil
		}
		require.Equal(t, "second", *options.Start)
		return &vpcv1.InstanceCollection{Instances: []vpcv1.Instance{inventoryInstance("managed-worker", "test-vpc")}}, nil, nil
	}).Times(2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	instances, err := instancesInVPC(ctx, ibm.NewVPCClientWithMock(mock), "test-vpc")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"control-plane": "control-plane", "static-worker": "static-worker", "managed-worker": "managed-worker"}, instances)
	require.Equal(t, 2, pages)
}

func TestCloudInventoryDoesNotTurnIncompleteEvidenceIntoEmptySuccess(t *testing.T) {
	for _, inventory := range []inventoryFunc{
		func(context.Context) ([]vpcv1.Instance, error) { return nil, fmt.Errorf("API unavailable") },
		func(context.Context) ([]vpcv1.Instance, error) {
			return []vpcv1.Instance{{VPC: &vpcv1.VPCReference{ID: core.StringPtr("test-vpc")}}}, nil
		},
	} {
		instances, err := instancesInVPC(context.Background(), inventory, "test-vpc")
		require.Error(t, err)
		require.Nil(t, instances)
	}
}

func TestManagedNodeInventoryRequiresExactProviderIdentity(t *testing.T) {
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "managed", Labels: map[string]string{karpv1.NodePoolLabelKey: "pool"}}, Spec: corev1.NodeSpec{ProviderID: "ibm:///us-south/instance"}}
	require.NoError(t, verifyManagedNodeInstances([]corev1.Node{node}, "us-south", map[string]string{"instance": "cloud-instance"}))
	require.Error(t, verifyManagedNodeInstances([]corev1.Node{node}, "us-south", map[string]string{"different-instance": "managed"}))
	require.Error(t, verifyManagedNodeInstances([]corev1.Node{node}, "eu-de", map[string]string{"instance": "managed"}))
	require.Error(t, verifyManagedNodeInstances(nil, "us-south", map[string]string{"instance": "managed"}))
}

func TestInstancePollingFailsOnPersistentAPIErrorAndTimeout(t *testing.T) {
	for _, apiFails := range []bool{true, false} {
		t.Run(fmt.Sprint(apiFails), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			remaining, err := pollInstancesGone(ctx, []string{"provisioned"}, time.Millisecond, func(context.Context) (map[string]string, error) {
				if apiFails {
					return nil, fmt.Errorf("API unavailable")
				}
				return map[string]string{"provisioned": "worker"}, nil
			}, nil)
			require.Error(t, err)
			require.True(t, errors.Is(err, context.DeadlineExceeded))
			require.Equal(t, []string{"provisioned"}, remaining)
			if apiFails {
				require.ErrorContains(t, err, "API unavailable")
			}
		})
	}
}

func TestInstancePollingTracksIdentityNotCount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	requests := 0
	remaining, err := pollInstancesGone(ctx, []string{"provisioned"}, time.Millisecond, func(context.Context) (map[string]string, error) {
		requests++
		switch requests {
		case 1:
			return nil, fmt.Errorf("transient API error")
		case 2:
			// Another tenant's instance disappearing must not satisfy the wait while ours remains.
			return map[string]string{"provisioned": "worker"}, nil
		default:
			return map[string]string{"unrelated": "worker", "new-peer": "worker"}, nil
		}
	}, nil)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.Equal(t, 3, requests)
}
