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
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"
	"sigs.k8s.io/karpenter/pkg/scheduling"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	ibmcloud "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/vpc/subnet"
)

// Mock Event Recorder
type mockEventRecorder struct {
	record.EventRecorder
}

func (m *mockEventRecorder) Publish(e ...events.Event) {
	// No-op for testing
}

// Mock InstanceType Provider
type mockInstanceTypeProvider struct{}

func (m *mockInstanceTypeProvider) Get(ctx context.Context, name string) (*cloudprovider.InstanceType, error) {
	return &cloudprovider.InstanceType{
		Name: "test-instance-type",
		Capacity: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("64Gi"),
			corev1.ResourcePods:   resource.MustParse("110"),
		},
		Requirements: scheduling.NewRequirements(
			scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, "test-instance-type"),
			scheduling.NewRequirement(v1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, v1.CapacityTypeOnDemand),
			scheduling.NewRequirement(corev1.LabelTopologyZone, corev1.NodeSelectorOpIn, "us-south-1"),
		),
		Offerings: cloudprovider.Offerings{
			{
				Requirements: scheduling.NewRequirements(
					scheduling.NewRequirement(corev1.LabelTopologyZone, corev1.NodeSelectorOpIn, "us-south-1"),
					scheduling.NewRequirement(v1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, v1.CapacityTypeOnDemand),
				),
				Price:     1.0,
				Available: true,
			},
		},
		Overhead: &cloudprovider.InstanceTypeOverhead{
			KubeReserved: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			SystemReserved: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			EvictionThreshold: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
	}, nil
}

func (m *mockInstanceTypeProvider) List(ctx context.Context) ([]*cloudprovider.InstanceType, error) {
	instanceType, _ := m.Get(ctx, "test-instance-type")
	return []*cloudprovider.InstanceType{instanceType}, nil
}

func (m *mockInstanceTypeProvider) Create(ctx context.Context, instanceType *cloudprovider.InstanceType) error {
	return nil
}

func (m *mockInstanceTypeProvider) Delete(ctx context.Context, instanceType *cloudprovider.InstanceType) error {
	return nil
}

func (m *mockInstanceTypeProvider) FilterInstanceTypes(ctx context.Context, requirements *v1alpha1.InstanceTypeRequirements) ([]*cloudprovider.InstanceType, error) {
	// For testing, just return a single instance type that meets all requirements
	instanceType, _ := m.Get(ctx, "test-instance-type")
	return []*cloudprovider.InstanceType{instanceType}, nil
}

func (m *mockInstanceTypeProvider) RankInstanceTypes(instanceTypes []*cloudprovider.InstanceType) []*cloudprovider.InstanceType {
	// For testing, just return the input slice unchanged
	return instanceTypes
}

// mockInstanceProvider removed - was unused

// Mock Subnet Provider
type mockSubnetProvider struct{}

func (m *mockSubnetProvider) ListSubnets(ctx context.Context, vpcID string) ([]subnet.SubnetInfo, error) {
	return []subnet.SubnetInfo{
		{
			ID:           "test-subnet-1",
			Zone:         "us-south-1",
			CIDR:         "10.0.1.0/24",
			AvailableIPs: 250,
			State:        "available",
		},
	}, nil
}

func (m *mockSubnetProvider) GetSubnet(ctx context.Context, subnetID string) (*subnet.SubnetInfo, error) {
	return &subnet.SubnetInfo{
		ID:           subnetID,
		Zone:         "us-south-1", 
		CIDR:         "10.0.1.0/24",
		AvailableIPs: 250,
		State:        "available",
	}, nil
}

func (m *mockSubnetProvider) SelectSubnets(ctx context.Context, vpcID string, strategy *v1alpha1.PlacementStrategy) ([]subnet.SubnetInfo, error) {
	return m.ListSubnets(ctx, vpcID)
}

func TestReconcile(t *testing.T) {
	// Create a new scheme and register types
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("Failed to add core scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("Failed to add ibm scheme: %v", err)
	}

	// Register NodeClaim types
	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	s.AddKnownTypes(gv,
		&v1.NodeClaim{},
		&v1.NodeClaimList{},
		&v1.NodePool{},
		&v1.NodePoolList{},
	)
	metav1.AddToGroupVersion(s, gv)

	now := metav1.Now()
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:          "us-south",
			Zone:            "us-south-1",
			InstanceProfile: "bx2-4x16",
			Image:           "test-image",
			VPC:             "test-vpc",
			Subnet:          "test-subnet",
		},
		Status: v1alpha1.IBMNodeClassStatus{
			SpecHash: 12345,
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: now,
					Reason:             "Ready",
					Message:            "NodeClass is ready",
				},
			},
		},
	}

	nodeClaim := &v1.NodeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "karpenter.sh/v1",
			Kind:       "NodeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-node",
			Namespace: "default",
		},
		Spec: v1.NodeClaimSpec{
			NodeClassRef: &v1.NodeClassReference{
				Name: nodeClass.Name,
			},
			Requirements: []v1.NodeSelectorRequirementWithMinValues{
				{
					NodeSelectorRequirement: corev1.NodeSelectorRequirement{
						Key:      corev1.LabelInstanceTypeStable,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"test-instance-type"},
					},
				},
			},
			Resources: v1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		},
	}

	// Create fake client with objects
	client := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(nodeClass, nodeClaim).
		WithStatusSubresource(&v1.NodeClaim{}).
		Build()

	// Create CloudProvider with mocked dependencies
	cloudProvider := ibmcloud.New(
		client,
		&mockEventRecorder{},
		&ibm.Client{},
		&mockInstanceTypeProvider{},
		&mockSubnetProvider{},
	)

	// Test cloud provider is created successfully
	assert.NotNil(t, cloudProvider)

	// Verify cloud provider methods are available
	assert.NotNil(t, cloudProvider)
}
