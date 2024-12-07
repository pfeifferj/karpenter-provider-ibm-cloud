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
	"reflect"

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

	// Extract instance type details from catalog entry metadata using reflection
	vcpuCount := 0
	memoryValue := 0
	gpuCount := 0

	// Extract values from metadata using reflection
	if entry.Metadata != nil {
		metadataValue := reflect.ValueOf(entry.Metadata).Elem()

		// Extract CPU count
		if vcpuField := metadataValue.FieldByName("VcpuCount"); vcpuField.IsValid() {
			if vcpuInterface := vcpuField.Interface(); vcpuInterface != nil {
				vcpuValue := reflect.ValueOf(vcpuInterface).Elem().FieldByName("Count")
				if vcpuValue.IsValid() && vcpuValue.Kind() == reflect.Ptr && !vcpuValue.IsNil() {
					vcpuCount = int(vcpuValue.Elem().Int())
				}
			}
		}

		// Extract memory
		if memField := metadataValue.FieldByName("Memory"); memField.IsValid() {
			if memInterface := memField.Interface(); memInterface != nil {
				memValue := reflect.ValueOf(memInterface).Elem().FieldByName("Value")
				if memValue.IsValid() && memValue.Kind() == reflect.Ptr && !memValue.IsNil() {
					memoryValue = int(memValue.Elem().Int())
				}
			}
		}

		// Extract GPU count if available
		if gpuField := metadataValue.FieldByName("GpuCount"); gpuField.IsValid() {
			if gpuInterface := gpuField.Interface(); gpuInterface != nil {
				gpuValue := reflect.ValueOf(gpuInterface).Elem().FieldByName("Count")
				if gpuValue.IsValid() && gpuValue.Kind() == reflect.Ptr && !gpuValue.IsNil() {
					gpuCount = int(gpuValue.Elem().Int())
				}
			}
		}
	}

	return &cloudprovider.InstanceType{
		Name: *entry.Name,
		Capacity: corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewQuantity(int64(vcpuCount), resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewQuantity(int64(memoryValue)*1024*1024*1024, resource.BinarySI),
			"gpu":                 *resource.NewQuantity(int64(gpuCount), resource.DecimalSI),
		},
		Requirements: scheduling.NewRequirements(
			scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, *entry.Name),
		),
	}, nil
}
