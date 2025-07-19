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

package test

import (
	"context"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	clock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/fake"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/instancetype"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/pricing"
	commonTypes "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/types"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/vpc/subnet"

	coretest "sigs.k8s.io/karpenter/pkg/test"
)

func init() {
	// Set up normalized labels for IBM Cloud zones
	karpv1.NormalizedLabels = lo.Assign(karpv1.NormalizedLabels, map[string]string{
		"topology.ibm.com/zone": corev1.LabelTopologyZone,
	})
	coretest.SetDefaultNodeClassType(&v1alpha1.IBMNodeClass{})
}

// Environment provides a test environment for IBM Cloud provider testing
type Environment struct {
	// Mock
	Clock         *clock.FakeClock
	EventRecorder *coretest.EventRecorder

	// Fake APIs
	VPCAPI *fake.VPCAPI
	IKSAPI *fake.IKSAPI

	// Caches
	InstanceTypeCache *cache.Cache
	SubnetCache       *cache.Cache
	ImageCache        *cache.Cache
	WorkerPoolCache   *cache.Cache

	// Providers
	InstanceTypesProvider instancetype.Provider
	VPCInstanceProvider   commonTypes.VPCInstanceProvider
	SubnetProvider        subnet.Provider
	WorkerPoolProvider    commonTypes.IKSWorkerPoolProvider
}

// NewEnvironment creates a new test environment with fake IBM Cloud APIs
func NewEnvironment(ctx context.Context, kubeClient client.Client) *Environment {
	clock := clock.NewFakeClock(time.Now())
	eventRecorder := coretest.NewEventRecorder()

	// Create fake APIs
	vpcAPI := fake.NewVPCAPI()
	iksAPI := fake.NewIKSAPI()

	// Create caches
	instanceTypeCache := cache.New(5*time.Minute, 10*time.Minute)
	subnetCache := cache.New(5*time.Minute, 10*time.Minute)
	imageCache := cache.New(30*time.Minute, time.Hour)
	workerPoolCache := cache.New(1*time.Minute, 5*time.Minute)

	// Create providers - use nil clients for test environment
	pricingProvider := pricing.NewIBMPricingProvider(nil)
	instanceTypesProvider := instancetype.NewProvider(nil, pricingProvider)

	// For VPC instance provider, we'll create a test wrapper that uses the fake API
	vpcInstanceProvider := NewTestVPCInstanceProvider(vpcAPI, kubeClient)

	// For subnet provider, create a test wrapper
	subnetProvider := NewTestSubnetProvider(vpcAPI)

	// For worker pool provider, create a test wrapper
	workerPoolProvider := NewTestWorkerPoolProvider(iksAPI)

	return &Environment{
		Clock:                 clock,
		EventRecorder:         eventRecorder,
		VPCAPI:                vpcAPI,
		IKSAPI:                iksAPI,
		InstanceTypeCache:     instanceTypeCache,
		SubnetCache:           subnetCache,
		ImageCache:            imageCache,
		WorkerPoolCache:       workerPoolCache,
		InstanceTypesProvider: instanceTypesProvider,
		VPCInstanceProvider:   vpcInstanceProvider,
		SubnetProvider:        subnetProvider,
		WorkerPoolProvider:    workerPoolProvider,
	}
}

// Reset resets the test environment state
func (env *Environment) Reset() {
	env.VPCAPI.Reset()
	env.IKSAPI.Reset()
	env.InstanceTypeCache.Flush()
	env.SubnetCache.Flush()
	env.ImageCache.Flush()
	env.WorkerPoolCache.Flush()
	env.EventRecorder.Reset()
}

// ExpectInstances sets up the fake VPC API to return specific instances
func (env *Environment) ExpectInstances(instances ...*fake.VPCInstance) {
	// Convert to VPC SDK format and add to fake API
	for _, inst := range instances {
		// Convert fake.VPCInstance to vpcv1.Instance format
		// For now, we'll just track that instances were provided
		_ = inst // TODO: Implement conversion when needed
	}
}

// ExpectWorkerPools sets up the fake IKS API to return specific worker pools
func (env *Environment) ExpectWorkerPools(pools ...*fake.WorkerPool) {
	for _, pool := range pools {
		env.IKSAPI.WorkerPools.Add(pool)
	}
}

// ExpectSubnets sets up the fake VPC API to return specific subnets
func (env *Environment) ExpectSubnets(subnets ...*fake.VPCSubnet) {
	// Convert to VPC SDK format and add to fake API
	for _, subnet := range subnets {
		// Convert fake.VPCSubnet to vpcv1.Subnet format
		// For now, we'll just track that subnets were provided
		_ = subnet // TODO: Implement conversion when needed
	}
}
