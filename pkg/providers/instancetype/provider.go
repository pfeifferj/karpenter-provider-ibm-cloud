package instancetype

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"knative.dev/pkg/logging"
	"sigs.k8s.io/karpenter/pkg/apis/v1beta1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"

	"github.com/pfeifferj/karpenter-ibm-cloud/pkg/apis/v1alpha1"
)

type Provider interface {
	UpdateInstanceTypes(ctx context.Context) error
	UpdateInstanceTypeOfferings(ctx context.Context) error
	GetInstanceTypes(ctx context.Context) ([]*InstanceType, error)
	List(ctx context.Context, kubeletConfig *v1beta1.KubeletConfiguration, nodeClass *v1alpha1.IBMNodeClass) ([]*cloudprovider.InstanceType, error)
	LivenessProbe(req *http.Request) error
}

type InstanceType struct {
	Name             string
	Architecture     string
	OperatingSystems []string
	Resources        v1beta1.ResourceList
	Offerings        []Offering
}

type Offering struct {
	CapacityType string
	Zone         string
	Price        float64
}

type provider struct {
	sync.RWMutex
	instanceTypes []*InstanceType
	vpcClient     *vpcv1.VpcV1
}

func NewProvider() (Provider, error) {
	apiKey := os.Getenv("IBMCLOUD_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("IBMCLOUD_API_KEY environment variable is required")
	}

	authenticator := &core.IamAuthenticator{
		ApiKey: apiKey,
	}

	vpcService, err := vpcv1.NewVpcV1(&vpcv1.VpcV1Options{
		Authenticator: authenticator,
	})
	if err != nil {
		return nil, fmt.Errorf("creating VPC client: %w", err)
	}

	return &provider{
		vpcClient: vpcService,
	}, nil
}

func (p *provider) UpdateInstanceTypes(ctx context.Context) error {
	profiles, _, err := p.vpcClient.ListInstanceProfiles(&vpcv1.ListInstanceProfilesOptions{})
	if err != nil {
		return fmt.Errorf("listing instance profiles: %w", err)
	}

	instanceTypes := make([]*InstanceType, 0, len(profiles.Profiles))
	for _, profile := range profiles.Profiles {
		memory := resource.MustParse(fmt.Sprintf("%dGi", *profile.Memory.Value))
		vcpu := resource.MustParse(fmt.Sprintf("%d", *profile.VCPUCount.Value))

		resources := v1beta1.ResourceList{
			v1beta1.ResourceCPU:    vcpu,
			v1beta1.ResourceMemory: memory,
		}

		// Add GPU resources if available
		if profile.GPUs != nil && len(profile.GPUs) > 0 {
			gpuCount := int64(0)
			for _, gpu := range profile.GPUs {
				gpuCount += *gpu.Count.Value
			}
			resources[v1beta1.ResourceNvidiaGPU] = resource.MustParse(fmt.Sprintf("%d", gpuCount))
		}

		instanceTypes = append(instanceTypes, &InstanceType{
			Name:         *profile.Name,
			Architecture: *profile.Architecture,
			// IBM Cloud VPC instances support both Linux and Windows
			OperatingSystems: []string{"linux", "windows"},
			Resources:        resources,
			Offerings:        []Offering{}, // Will be populated in UpdateInstanceTypeOfferings
		})
	}

	p.Lock()
	defer p.Unlock()
	p.instanceTypes = instanceTypes
	return nil
}

func (p *provider) UpdateInstanceTypeOfferings(ctx context.Context) error {
	p.Lock()
	defer p.Unlock()

	for _, instanceType := range p.instanceTypes {
		// Get zone availability for each instance type
		zones, _, err := p.vpcClient.ListRegionZones(&vpcv1.ListRegionZonesOptions{})
		if err != nil {
			logging.FromContext(ctx).Errorf("listing zones for instance type %s: %v", instanceType.Name, err)
			continue
		}

		offerings := make([]Offering, 0, len(zones.Zones))
		for _, zone := range zones.Zones {
			// Check if instance type is available in zone
			available, _, err := p.vpcClient.GetInstanceProfileZone(&vpcv1.GetInstanceProfileZoneOptions{
				InstanceProfileName: &instanceType.Name,
				ZoneName:           zone.Name,
			})
			if err != nil {
				logging.FromContext(ctx).Errorf("checking zone availability for instance type %s in zone %s: %v", instanceType.Name, *zone.Name, err)
				continue
			}

			if available.Available != nil && *available.Available {
				offerings = append(offerings, Offering{
					CapacityType: "on-demand", // IBM Cloud currently only supports on-demand instances
					Zone:         *zone.Name,
					Price:        0.0, // Price information would need to be fetched from a pricing API
				})
			}
		}

		instanceType.Offerings = offerings
	}

	return nil
}

func (p *provider) GetInstanceTypes(ctx context.Context) ([]*InstanceType, error) {
	p.RLock()
	defer p.RUnlock()
	return lo.ToSlicePtr(lo.FromPtr(p.instanceTypes...)), nil
}

func (p *provider) List(ctx context.Context, kubeletConfig *v1beta1.KubeletConfiguration, nodeClass *v1alpha1.IBMNodeClass) ([]*cloudprovider.InstanceType, error) {
	p.RLock()
	defer p.RUnlock()

	var result []*cloudprovider.InstanceType
	for _, it := range p.instanceTypes {
		offerings := make([]cloudprovider.Offering, 0, len(it.Offerings))
		for _, o := range it.Offerings {
			// Filter offerings based on nodeClass zones if specified
			if len(nodeClass.Spec.Zones) > 0 && !lo.Contains(nodeClass.Spec.Zones, o.Zone) {
				continue
			}

			offerings = append(offerings, cloudprovider.Offering{
				CapacityType: o.CapacityType,
				Zone:         o.Zone,
				Price:        o.Price,
				Available:    true,
			})
		}

		// Skip if no valid offerings for this instance type
		if len(offerings) == 0 {
			continue
		}

		requirements := scheduling.NewRequirements(
			scheduling.NewRequirement(corev1.LabelArchStable, corev1.NodeSelectorOpIn, it.Architecture),
			scheduling.NewRequirement(corev1.LabelOSStable, corev1.NodeSelectorOpIn, it.OperatingSystems...),
		)

		result = append(result, &cloudprovider.InstanceType{
			Name:         it.Name,
			Requirements: requirements,
			Capacity:     it.Resources,
			Offerings:    cloudprovider.NewOfferings(offerings...),
		})
	}

	return result, nil
}

func (p *provider) LivenessProbe(req *http.Request) error {
	p.RLock()
	defer p.RUnlock()
	if len(p.instanceTypes) == 0 {
		return fmt.Errorf("no instance types loaded")
	}
	return nil
}
