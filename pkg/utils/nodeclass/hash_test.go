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

package nodeclass

import (
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOperationalPolicyDoesNotChangeProvisioningHash(t *testing.T) {
	original := &v1alpha1.IBMNodeClass{Spec: v1alpha1.IBMNodeClassSpec{Region: "us-south", Image: "image", IKSDynamicPools: &v1alpha1.IKSDynamicPoolConfig{Enabled: true}}}
	expected, err := ProvisioningHash(original)
	require.NoError(t, err)
	changed := original.DeepCopy()
	changed.Spec.LoadBalancerIntegration = &v1alpha1.LoadBalancerIntegration{Enabled: true}
	changed.Spec.IKSDynamicPools.CleanupPolicy = &v1alpha1.IKSPoolCleanupPolicy{EmptyPoolTTL: "1h"}
	actual, err := ProvisioningHash(changed)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	changed.Spec.Image = "other"
	actual, err = ProvisioningHash(changed)
	require.NoError(t, err)
	require.NotEqual(t, expected, actual)
}
