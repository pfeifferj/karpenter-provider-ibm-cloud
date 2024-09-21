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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	kwok "sigs.k8s.io/karpenter/kwok/cloudprovider"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

var regionZones = map[string][]string{
	"au-syd":       {"au-syd-1", "au-syd-2", "au-syd-3"},
	"jp-osa":       {"jp-osa-1", "jp-osa-2", "jp-osa-3"},
	"jp-tok":       {"jp-tok-1", "jp-tok-2", "jp-tok-3"},
	"eu-de":        {"eu-de-1", "eu-de-2", "eu-de-3"},
	"eu-es":        {"eu-es-1", "eu-es-2", "eu-es-3"},
	"eu-gb":        {"eu-gb-1", "eu-gb-2", "eu-gb-3"},
	"ca-tor":       {"ca-tor-1", "ca-tor-2", "ca-tor-3"},
	"us-south":     {"us-south-1", "us-south-2", "us-south-3"},
	"us-east":      {"us-east-1", "us-east-2", "us-east-3"},
	"br-sao":       {"br-sao-1", "br-sao-2", "br-sao-3"},
}

// function to calculate price based on resources
func priceFromResources(resources corev1.ResourceList) float64 {
	price := 0.0
	for k, v := range resources {
		switch k {
		case corev1.ResourceCPU:
			price += 0.025 * v.AsApproximateFloat64()
		case corev1.ResourceMemory:
			price += 0.001 * v.AsApproximateFloat64() / (1e9) // assuming 1GiB = 1e9 bytes
		}
	}
	return price
}

func makeGenericInstanceTypeName(cpu, memFactor int, arch string, os corev1.OSName) string {
	size := fmt.Sprintf("%dx", cpu)
	var family string
	switch memFactor {
	case 2:
		family = "c" // cpu optimized
	case 4:
		family = "s" // standard
	case 8:
		family = "m" // memory optimized
	default:
		family = "e" // exotic
	}
	return fmt.Sprintf("%s-%s-%s-%s", family, size, arch, os)
}

// function to create instance types for IBM Cloud
func constructGenericInstanceTypes() []kwok.InstanceTypeOptions {
	var instanceTypesOptions []kwok.InstanceTypeOptions

	for _, cpu := range []int{1, 2, 4, 8, 16, 32, 48, 64, 96, 128, 192, 256} {
		for _, memFactor := range []int{2, 4, 8} {
			for _, os := range []corev1.OSName{corev1.Linux, corev1.Windows} {
				for _, arch := range []string{v1.ArchitectureAmd64, v1.ArchitectureArm64} {
					// construct instance type details, then construct offerings.
					name := makeGenericInstanceTypeName(cpu, memFactor, arch, os)
					mem := cpu * memFactor
					pods := lo.Clamp(cpu*16, 0, 1024)
					opts := kwok.InstanceTypeOptions{
						Name:             name,
						Architecture:     arch,
						OperatingSystems: []corev1.OSName{os},
						Resources: corev1.ResourceList{
							corev1.ResourceCPU:              resource.MustParse(fmt.Sprintf("%d", cpu)),
							corev1.ResourceMemory:           resource.MustParse(fmt.Sprintf("%dGi", mem)),
							corev1.ResourcePods:             resource.MustParse(fmt.Sprintf("%d", pods)),
							corev1.ResourceEphemeralStorage: resource.MustParse("20Gi"),
						},
					}

					opts.Offerings = []kwok.KWOKOffering{}
					for _, zones := range regionZones {
						for _, zone := range zones {
							price := priceFromResources(opts.Resources) // calculate price

							for _, ct := range []string{v1.CapacityTypeSpot, v1.CapacityTypeOnDemand} {
								opts.Offerings = append(opts.Offerings, kwok.KWOKOffering{
									Requirements: []corev1.NodeSelectorRequirement{
										{Key: v1.CapacityTypeLabelKey, Operator: corev1.NodeSelectorOpIn, Values: []string{ct}},
										{Key: corev1.LabelTopologyZone, Operator: corev1.NodeSelectorOpIn, Values: []string{zone}},
									},
									Offering: cloudprovider.Offering{
										Price:     lo.Ternary(ct == v1.CapacityTypeSpot, price*0.7, price),
										Available: true,
									},
								})
							}
						}
					}
					instanceTypesOptions = append(instanceTypesOptions, opts)
				}
			}
		}
	}
	return instanceTypesOptions
}

func main() {
	// construct instance types
	opts := constructGenericInstanceTypes()
	output, err := json.MarshalIndent(opts, "", "    ")
	if err != nil {
		fmt.Printf("could not marshal generated instance types to JSON: %v\n", err)
		os.Exit(1)
	}
    // Write to the desired file
    outputFilePath := filepath.Join("cloudprovider", "instance_types.json")
    if err := os.WriteFile(outputFilePath, output, 0644); err != nil {
        fmt.Printf("error writing output to file: %v\n", err)
        os.Exit(1)
    }
    fmt.Printf("instance types have been generated and written to %s\n", outputFilePath)

    // Read back the written file
    content, err := os.ReadFile(outputFilePath)
    if err != nil {
        fmt.Printf("error reading back written file: %v\n", err)
        os.Exit(1)
    }
    fmt.Printf("content size: %d bytes\n", len(content))
    fmt.Println("first 500 characters of content:")
    fmt.Println(string(content[:500]))
}
