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

package poolcleanup

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/httpclient"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/providers/iks/workerpool"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/utils/ownership"
)

type cleanupCloud struct {
	ibm.IKSClientInterface
	pool        *ibm.WorkerPool
	workers     []*ibm.IKSWorkerDetails
	getError    error
	deleteError error
	deleteCalls []string
	getCalls    int
	onGet       func(int)
}

func (c *cleanupCloud) GetAccountID() string { return "0123456789abcdef0123456789abcdef" }
func (c *cleanupCloud) GetRegion() string    { return "us-south" }

func (c *cleanupCloud) ListWorkerPools(context.Context, string) ([]*ibm.WorkerPool, error) {
	if c.pool == nil {
		return nil, nil
	}
	return []*ibm.WorkerPool{c.pool}, nil
}
func (c *cleanupCloud) GetWorkerPool(context.Context, string, string) (*ibm.WorkerPool, error) {
	c.getCalls++
	if c.onGet != nil {
		c.onGet(c.getCalls)
	}
	if c.getError != nil {
		return nil, c.getError
	}
	if c.pool == nil {
		return nil, &httpclient.IBMCloudError{StatusCode: 404}
	}
	return c.pool, nil
}
func (c *cleanupCloud) ListWorkers(context.Context, string) ([]*ibm.IKSWorkerDetails, error) {
	return c.workers, nil
}
func (c *cleanupCloud) DeleteWorkerPool(_ context.Context, _ string, poolID string) error {
	c.deleteCalls = append(c.deleteCalls, poolID)
	if c.deleteError != nil {
		return c.deleteError
	}
	c.pool = nil
	return nil
}

func cleanupFixture(t *testing.T) (*Controller, client.Client, *cleanupCloud, *v1alpha1.IBMNodeClass) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	scheme.AddKnownTypes(schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}, &v1.NodeClaim{}, &v1.NodeClaimList{})
	nodeClass := &v1alpha1.IBMNodeClass{ObjectMeta: metav1.ObjectMeta{Name: "class", UID: "class-uid"}, Spec: v1alpha1.IBMNodeClassSpec{Region: "us-south", IKSClusterID: "cluster", IKSDynamicPools: &v1alpha1.IKSDynamicPoolConfig{Enabled: true, CleanupPolicy: &v1alpha1.IKSPoolCleanupPolicy{EmptyPoolTTL: "0s"}}}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(nodeClass, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: "cluster-uid"}}).Build()
	cloud := &cleanupCloud{pool: &ibm.WorkerPool{ID: "pool-id", Name: "owned-pool", Labels: map[string]string{ownership.ManagedTag: "true", ownership.ProviderTag: "iks", ownership.ClusterUIDTag: "cluster-uid", ownership.NodeClassUIDTag: "class-uid"}}}
	controller := NewController(kubeClient, nil, kubeClient)
	controller.SetIKSClient(cloud)
	controller.poolTracking["cluster/pool-id"] = time.Now().Add(-time.Hour)
	return controller, kubeClient, cloud, nodeClass
}

func TestCleanupDeletesOnlyOwnedEmptyPoolAndConfirmsAbsence(t *testing.T) {
	controller, kubeClient, cloud, nodeClass := cleanupFixture(t)
	require.NoError(t, controller.cleanupEmptyPools(context.Background(), cloud, "cluster", nodeClass))
	require.Equal(t, []string{"pool-id"}, cloud.deleteCalls)
	reservation := &corev1.ConfigMap{}
	key := workerpool.ReservationKey("cluster", "owned-pool", "karpenter")
	require.NoError(t, kubeClient.Get(context.Background(), key, reservation))
	require.Equal(t, "deleting", reservation.Data["phase"])
	cloud.getError = &httpclient.IBMCloudError{StatusCode: 429}
	require.Error(t, controller.releaseDeletedReservations(context.Background(), cloud))
	require.NoError(t, kubeClient.Get(context.Background(), key, reservation))
	cloud.getError = nil
	require.NoError(t, controller.releaseDeletedReservations(context.Background(), cloud))
	require.True(t, client.IgnoreNotFound(kubeClient.Get(context.Background(), key, reservation)) == nil)
}

func TestCleanupRejectsForeignClassUIDAndClaimPools(t *testing.T) {
	for _, key := range []string{ownership.NodeClassUIDTag, ownership.ClusterUIDTag, ownership.ClaimUIDTag} {
		t.Run(key, func(t *testing.T) {
			controller, _, cloud, nodeClass := cleanupFixture(t)
			cloud.pool.Labels[key] = "different-owner"
			require.NoError(t, controller.cleanupEmptyPools(context.Background(), cloud, "cluster", nodeClass))
			require.Empty(t, cloud.deleteCalls)
		})
	}
}

func TestCleanupReservationBlocksDeletion(t *testing.T) {
	controller, kubeClient, cloud, nodeClass := cleanupFixture(t)
	key := workerpool.ReservationKey("cluster", cloud.pool.Name, "karpenter")
	require.NoError(t, kubeClient.Create(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}, Data: map[string]string{"phase": "creating"}}))
	require.NoError(t, controller.cleanupEmptyPools(context.Background(), cloud, "cluster", nodeClass))
	require.Empty(t, cloud.deleteCalls)
}

func TestCleanupRechecksWorkersAndPoolBeforeDeletion(t *testing.T) {
	t.Run("registered worker", func(t *testing.T) {
		controller, _, cloud, nodeClass := cleanupFixture(t)
		cloud.workers = []*ibm.IKSWorkerDetails{{ID: "worker", PoolID: "pool-id"}}
		require.NoError(t, controller.cleanupEmptyPools(context.Background(), cloud, "cluster", nodeClass))
		require.Empty(t, cloud.deleteCalls)
	})
	t.Run("size changed after reservation", func(t *testing.T) {
		controller, _, cloud, nodeClass := cleanupFixture(t)
		cloud.onGet = func(call int) {
			if call == 2 {
				cloud.pool.SizePerZone = 1
			}
		}
		require.NoError(t, controller.cleanupEmptyPools(context.Background(), cloud, "cluster", nodeClass))
		require.Empty(t, cloud.deleteCalls)
	})
}

func TestCleanupRetriesTransientDeletionFailures(t *testing.T) {
	controller, _, cloud, nodeClass := cleanupFixture(t)
	cloud.deleteError = fmt.Errorf("temporary outage")
	for range 5 {
		require.Error(t, controller.cleanupEmptyPools(context.Background(), cloud, "cluster", nodeClass))
	}
	cloud.deleteError = nil
	require.NoError(t, controller.cleanupEmptyPools(context.Background(), cloud, "cluster", nodeClass))
	require.Len(t, cloud.deleteCalls, 6)
}

func TestCleanupUsesFreshClassPolicy(t *testing.T) {
	controller, kubeClient, cloud, oldClass := cleanupFixture(t)
	fresh := oldClass.DeepCopy()
	fresh.Spec.IKSDynamicPools.Enabled = false
	require.NoError(t, kubeClient.Update(context.Background(), fresh))
	require.NoError(t, controller.cleanupEmptyPools(context.Background(), cloud, "cluster", oldClass))
	require.Empty(t, cloud.deleteCalls)
}

func TestCleanupPolicyDefaults(t *testing.T) {
	controller := NewController(nil, nil)
	nodeClass := &v1alpha1.IBMNodeClass{}
	require.True(t, controller.isCleanupEnabled(nodeClass))
	require.Equal(t, DefaultEmptyPoolTTL, controller.getEmptyPoolTTL(nodeClass))
	disabled := false
	nodeClass.Spec.IKSDynamicPools = &v1alpha1.IKSDynamicPoolConfig{Enabled: true, CleanupPolicy: &v1alpha1.IKSPoolCleanupPolicy{DeleteOnEmpty: &disabled, EmptyPoolTTL: "7m"}}
	require.False(t, controller.isCleanupEnabled(nodeClass))
	require.Equal(t, 7*time.Minute, controller.getEmptyPoolTTL(nodeClass))
}
