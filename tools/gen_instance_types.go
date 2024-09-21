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
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/IBM/platform-services-go-sdk/globalcatalogv1"
	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

var regionZones = map[string][]string{
	"au-syd":   {"au-syd-1", "au-syd-2", "au-syd-3"},
	"jp-osa":   {"jp-osa-1", "jp-osa-2", "jp-osa-3"},
	"jp-tok":   {"jp-tok-1", "jp-tok-2", "jp-tok-3"},
	"eu-de":    {"eu-de-1", "eu-de-2", "eu-de-3"},
	"eu-es":    {"eu-es-1", "eu-es-2", "eu-es-3"},
	"eu-gb":    {"eu-gb-1", "eu-gb-2", "eu-gb-3"},
	"ca-tor":   {"ca-tor-1", "ca-tor-2", "ca-tor-3"},
	"us-south": {"us-south-1", "us-south-2", "us-south-3"},
	"us-east":  {"us-east-1", "us-east-2", "us-east-3"},
	"br-sao":   {"br-sao-1", "br-sao-2", "br-sao-3"},
}

// define IBMInstanceType struct
type IBMInstanceType struct {
	Name             string                       `json:"name"`
	Architecture     string                       `json:"architecture"`
	OperatingSystems []corev1.OSName              `json:"operatingSystems"`
	Resources        corev1.ResourceList          `json:"resources"`
	Offerings        []IBMOffering                `json:"offerings"`
}

// define an IBM-specific offering struct
type IBMOffering struct {
	Requirements []corev1.NodeSelectorRequirement `json:"requirements"`
	Offering     cloudprovider.Offering           `json:"offering"`
}

// fetch instance profile pricing from IBM Cloud's Global Catalog API
func fetchPricing(globalCatalog *globalcatalogv1.GlobalCatalogV1, profileName string) (float64, error) {
	pricingOptions := &globalcatalogv1.GetPricingOptions{
		ID: &profileName,
	}
	pricingData, _, err := globalCatalog.GetPricing(pricingOptions)
	if err != nil {
		return 0, fmt.Errorf("error fetching pricing data: %v", err)
	}

	// example extraction logic: adjust as per the exact response structure
	if len(pricingData.Metrics) > 0 && len(pricingData.Metrics[0].Amounts) > 0 {
		price := pricingData.Metrics[0].Amounts[0].Price
		return price, nil
	}

	return 0, fmt.Errorf("pricing data not found")
}

// construct instance types with pricing data
func constructIBMInstanceTypes(vpcService *vpcv1.VpcV1, globalCatalog *globalcatalogv1.GlobalCatalogV1) []IBMInstanceType {
	instanceProfiles, err := fetchInstanceProfiles(vpcService)
	if err != nil {
		log.Fatalf("failed to fetch instance profiles: %v", err)
	}

	var instanceTypes []IBMInstanceType
	for _, profile := range instanceProfiles {
		options := &vpcv1.GetInstanceProfileOptions{}
		options.SetName(*profile.Name)
		detailedProfile, _, err := vpcService.GetInstanceProfile(options)
		if err != nil {
			log.Printf("failed to retrieve details for profile %s: %v", *profile.Name, err)
			continue
		}

		price, err := fetchPricing(globalCatalog, *detailedProfile.Name)
		if err != nil {
			log.Printf("failed to retrieve pricing for profile %s: %v", *profile.Name, err)
			price = 0 // fallback to zero or some default value
		}

		instance := IBMInstanceType{
			Name:             *detailedProfile.Name,
			Architecture:     *detailedProfile.VcpuArchitecture.Value,
			OperatingSystems: []corev1.OSName{corev1.Linux},
			Resources: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%d", *detailedProfile.VcpuCount.Value)),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dGi", *detailedProfile.Memory.Value)),
			},
		}

		for _, zones := range regionZones {
			for _, zone := range zones {
				instance.Offerings = append(instance.Offerings, IBMOffering{
					Requirements: []corev1.NodeSelectorRequirement{
						{Key: v1.CapacityTypeLabelKey, Operator: corev1.NodeSelectorOpIn, Values: []string{v1.CapacityTypeOnDemand}},
						{Key: corev1.LabelTopologyZone, Operator: corev1.NodeSelectorOpIn, Values: []string{zone}},
					},
					Offering: cloudprovider.Offering{
						Price:     price,
						Available: true,
					},
				})
			}
		}
		instanceTypes = append(instanceTypes, instance)
	}
	return instanceTypes
}

func main() {
	// create a VPC client
	vpcService, err := vpcv1.NewVpcV1(&vpcv1.VpcV1Options{})
	if err != nil {
		log.Fatalf("failed to create VPC service client: %v", err)
	}

	// create a Global Catalog client using external configuration
	globalCatalog, err := globalcatalogv1.NewGlobalCatalogV1UsingExternalConfig(&globalcatalogv1.GlobalCatalogV1Options{})
	if err != nil {
		log.Fatalf("failed to create Global Catalog service client: %v", err)
	}

	// construct instance types using the VPC service and Global Catalog service
	instanceTypes := constructIBMInstanceTypes(vpcService, globalCatalog)
	output, err := json.MarshalIndent(instanceTypes, "", "    ")
	if err != nil {
		log.Fatalf("failed to marshal generated instance types to JSON: %v", err)
	}

	// write to the desired file
	outputFilePath := filepath.Join("cloudprovider", "instance_types.json")
	if err := os.WriteFile(outputFilePath, output, 0644); err != nil {
		log.Fatalf("failed to write output to file: %v", err)
	}
	fmt.Printf("Instance types have been generated and written to %s\n", outputFilePath)
}