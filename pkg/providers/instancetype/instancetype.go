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

package instancetype

import (
	"context"
	"fmt"

	"github.com/IBM/platform-services-go-sdk/globalcatalogv1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
)

type IBMInstanceTypeProvider struct {
	client *ibm.Client
}

func NewProvider() (Provider, error) {
	client, err := ibm.NewClient()
	if err != nil {
		return nil, fmt.Errorf("creating IBM Cloud client: %w", err)
	}
	return &IBMInstanceTypeProvider{
		client: client,
	}, nil
}

func (p *IBMInstanceTypeProvider) Get(ctx context.Context, name string) (*cloudprovider.InstanceType, error) {
	catalogClient, err := p.client.GetGlobalCatalogClient()
	if err != nil {
		return nil, fmt.Errorf("getting Global Catalog client: %w", err)
	}

	// Get instance profile details from Global Catalog
	entry, err := catalogClient.GetInstanceType(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("getting instance type from catalog: %w", err)
	}

	// Convert catalog entry to instance type
	instanceType, err := convertCatalogEntryToInstanceType(entry)
	if err != nil {
		return nil, fmt.Errorf("converting catalog entry: %w", err)
	}

	return instanceType, nil
}

func (p *IBMInstanceTypeProvider) List(ctx context.Context) ([]*cloudprovider.InstanceType, error) {
	catalogClient, err := p.client.GetGlobalCatalogClient()
	if err != nil {
		return nil, fmt.Errorf("getting Global Catalog client: %w", err)
	}

	// List instance profiles from Global Catalog
	entries, err := catalogClient.ListInstanceTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing instance types from catalog: %w", err)
	}

	// Convert catalog entries to instance types
	var instanceTypes []*cloudprovider.InstanceType
	for _, entry := range entries {
		instanceType, err := convertCatalogEntryToInstanceType(&entry)
		if err != nil {
			return nil, fmt.Errorf("converting catalog entry: %w", err)
		}
		instanceTypes = append(instanceTypes, instanceType)
	}

	return instanceTypes, nil
}

func (p *IBMInstanceTypeProvider) Create(ctx context.Context, instanceType *cloudprovider.InstanceType) error {
	// Instance types are predefined in IBM Cloud, so this is a no-op
	return nil
}

func (p *IBMInstanceTypeProvider) Delete(ctx context.Context, instanceType *cloudprovider.InstanceType) error {
	// Instance types are predefined in IBM Cloud, so this is a no-op
	return nil
}

func convertCatalogEntryToInstanceType(entry *globalcatalogv1.CatalogEntry) (*cloudprovider.InstanceType, error) {
	if entry == nil {
		return nil, fmt.Errorf("catalog entry is nil")
	}

	// Extract instance type details from catalog entry metadata
	var cpus int64 = 2    // Default values, replace with actual parsing
	var memory int64 = 8   // Default values, replace with actual parsing
	var gpus int64 = 0    // Default values, replace with actual parsing

	// TODO: Parse actual values from entry.Metadata or appropriate field
	// The exact field to use depends on IBM Cloud's catalog structure

	return &cloudprovider.InstanceType{
		Name: *entry.Name,
		Capacity: corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewQuantity(cpus, resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewQuantity(memory*1024*1024*1024, resource.BinarySI),
			"gpu":                 *resource.NewQuantity(gpus, resource.DecimalSI),
		},
		Requirements: scheduling.NewRequirements(
			scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, *entry.Name),
		),
	}, nil
}

// Helper functions to parse CPU, memory, and GPU values from catalog entry metadata
func parseCPU(s string) int64 {
	// Implementation would parse CPU value from string
	// For example: "2" -> 2
	return 0 // Placeholder
}

func parseMemory(s string) int64 {
	// Implementation would parse memory value from string
	// For example: "8GB" -> 8589934592 (bytes)
	return 0 // Placeholder
}

func parseGPU(s string) int64 {
	// Implementation would parse GPU value from string
	// For example: "1" -> 1
	return 0 // Placeholder
}
