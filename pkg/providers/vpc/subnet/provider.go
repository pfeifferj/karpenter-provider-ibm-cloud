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
package subnet

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/IBM/vpc-go-sdk/vpcv1"
	v1alpha1 "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cache"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
)

// SubnetInfo contains information about a VPC subnet
type SubnetInfo struct {
	ID              string
	Zone            string
	CIDR            string
	AvailableIPs    int32
	Tags            map[string]string
	State           string
	TotalIPCount    int32
	UsedIPCount     int32
	ReservedIPCount int32
}

// Provider defines the interface for managing IBM Cloud VPC subnets
type Provider interface {
	// ListSubnets returns all subnets in the VPC
	ListSubnets(ctx context.Context, vpcID string) ([]SubnetInfo, error)

	// GetSubnet retrieves information about a specific subnet
	GetSubnet(ctx context.Context, subnetID string) (*SubnetInfo, error)

	// SelectSubnets returns subnets that meet the placement criteria
	SelectSubnets(ctx context.Context, vpcID string, strategy *v1alpha1.PlacementStrategy) ([]SubnetInfo, error)
}

type provider struct {
	client      *ibm.Client
	subnetCache *cache.Cache
}

// NewProvider creates a new subnet provider
func NewProvider(client *ibm.Client) Provider {
	return &provider{
		client:      client,
		subnetCache: cache.New(5 * time.Minute), // Cache subnets for 5 minutes
	}
}

// subnetScore represents a subnet's suitability score
type subnetScore struct {
	subnet SubnetInfo
	score  float64
}

// calculateSubnetScore computes a ranking score for a subnet
// Higher scores are better
func calculateSubnetScore(subnet SubnetInfo, criteria *v1alpha1.SubnetSelectionCriteria) float64 {
	var score float64

	// Factor 1: Available IP capacity (0-100 points)
	capacityRatio := float64(subnet.AvailableIPs) / float64(subnet.TotalIPCount)
	score += capacityRatio * 100

	// Factor 2: IP fragmentation penalty (0-50 points penalty)
	fragmentationRatio := float64(subnet.UsedIPCount+subnet.ReservedIPCount) / float64(subnet.TotalIPCount)
	score -= fragmentationRatio * 50

	return score
}

// SelectSubnets implements subnet selection based on placement strategy
func (p *provider) SelectSubnets(ctx context.Context, vpcID string, strategy *v1alpha1.PlacementStrategy) ([]SubnetInfo, error) {
	// Get all subnets in the VPC
	allSubnets, err := p.ListSubnets(ctx, vpcID)
	if err != nil {
		return nil, fmt.Errorf("failed to list subnets: %w", err)
	}

	var eligibleSubnets []SubnetInfo

	// Filter subnets based on criteria
	for _, subnet := range allSubnets {
		// Skip subnets in bad state
		if subnet.State != "available" {
			continue
		}

		// Check minimum available IPs
		if strategy.SubnetSelection != nil &&
			strategy.SubnetSelection.MinimumAvailableIPs > 0 &&
			subnet.AvailableIPs < strategy.SubnetSelection.MinimumAvailableIPs {
			continue
		}

		// Check required tags
		if strategy.SubnetSelection != nil && len(strategy.SubnetSelection.RequiredTags) > 0 {
			hasAllTags := true
			for k, v := range strategy.SubnetSelection.RequiredTags {
				if subnet.Tags[k] != v {
					hasAllTags = false
					break
				}
			}
			if !hasAllTags {
				continue
			}
		}

		eligibleSubnets = append(eligibleSubnets, subnet)
	}

	if len(eligibleSubnets) == 0 {
		return nil, fmt.Errorf("no eligible subnets found in VPC %s", vpcID)
	}

	// Score and rank subnets
	scores := make([]subnetScore, len(eligibleSubnets))
	for i, subnet := range eligibleSubnets {
		scores[i] = subnetScore{
			subnet: subnet,
			score:  calculateSubnetScore(subnet, strategy.SubnetSelection),
		}
	}

	// Sort by score (higher is better)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Apply zone distribution strategy
	var selectedSubnets []SubnetInfo
	seenZones := make(map[string]bool)

	switch strategy.ZoneBalance {
	case "Balanced":
		// Select one subnet per zone, prioritizing higher scores
		for _, score := range scores {
			if !seenZones[score.subnet.Zone] {
				selectedSubnets = append(selectedSubnets, score.subnet)
				seenZones[score.subnet.Zone] = true
			}
		}

	case "AvailabilityFirst":
		// Select all eligible subnets to maximize availability
		for _, score := range scores {
			selectedSubnets = append(selectedSubnets, score.subnet)
		}

	case "CostOptimized":
		// Select fewer subnets to minimize cross-zone traffic costs
		// Start with the highest scoring subnet in each zone
		targetZones := 2 // Minimum for HA
		for _, score := range scores {
			if len(selectedSubnets) >= targetZones {
				break
			}
			if !seenZones[score.subnet.Zone] {
				selectedSubnets = append(selectedSubnets, score.subnet)
				seenZones[score.subnet.Zone] = true
			}
		}
	}

	if len(selectedSubnets) == 0 {
		return nil, fmt.Errorf("no subnets selected after applying placement strategy")
	}

	return selectedSubnets, nil
}

// ListSubnets returns all subnets in the VPC
func (p *provider) ListSubnets(ctx context.Context, vpcID string) ([]SubnetInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("IBM client is not initialized")
	}

	cacheKey := fmt.Sprintf("vpc-subnets:%s", vpcID)

	// Try to get from cache first
	if cached, exists := p.subnetCache.Get(cacheKey); exists {
		return cached.([]SubnetInfo), nil
	}

	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return nil, fmt.Errorf("getting VPC client: %w", err)
	}

	// List all subnets in the VPC
	response, err := vpcClient.ListSubnets(ctx, vpcID)
	if err != nil {
		return nil, fmt.Errorf("listing subnets for VPC %s: %w", vpcID, err)
	}

	// Convert VPC subnet objects to our SubnetInfo format
	subnets := make([]SubnetInfo, 0, len(response.Subnets))
	for _, subnet := range response.Subnets {
		subnetInfo := convertVPCSubnetToSubnetInfo(subnet)
		subnets = append(subnets, subnetInfo)
	}

	// Cache the result
	p.subnetCache.Set(cacheKey, subnets)

	return subnets, nil
}

// GetSubnet retrieves information about a specific subnet
func (p *provider) GetSubnet(ctx context.Context, subnetID string) (*SubnetInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("IBM client is not initialized")
	}

	cacheKey := fmt.Sprintf("subnet:%s", subnetID)

	// Try to get from cache first
	if cached, exists := p.subnetCache.Get(cacheKey); exists {
		result := cached.(SubnetInfo)
		return &result, nil
	}

	vpcClient, err := p.client.GetVPCClient()
	if err != nil {
		return nil, fmt.Errorf("getting VPC client: %w", err)
	}

	// Get subnet details
	subnet, err := vpcClient.GetSubnet(ctx, subnetID)
	if err != nil {
		return nil, fmt.Errorf("getting subnet %s: %w", subnetID, err)
	}

	subnetInfo := convertVPCSubnetToSubnetInfo(*subnet)

	// Cache the result with shorter TTL for individual subnets (may change more frequently)
	p.subnetCache.SetWithTTL(cacheKey, subnetInfo, 2*time.Minute)

	return &subnetInfo, nil
}

// convertVPCSubnetToSubnetInfo converts a VPC SDK subnet to our SubnetInfo format
func convertVPCSubnetToSubnetInfo(vpcSubnet vpcv1.Subnet) SubnetInfo {
	subnetInfo := SubnetInfo{
		ID:    *vpcSubnet.ID,
		Zone:  *vpcSubnet.Zone.Name,
		CIDR:  *vpcSubnet.Ipv4CIDRBlock,
		State: *vpcSubnet.Status,
		Tags:  make(map[string]string),
	}

	// Calculate IP counts
	if vpcSubnet.TotalIpv4AddressCount != nil {
		subnetInfo.TotalIPCount = int32(*vpcSubnet.TotalIpv4AddressCount)
	}
	if vpcSubnet.AvailableIpv4AddressCount != nil {
		subnetInfo.AvailableIPs = int32(*vpcSubnet.AvailableIpv4AddressCount)
	}

	// Calculate used IPs
	subnetInfo.UsedIPCount = subnetInfo.TotalIPCount - subnetInfo.AvailableIPs

	// Extract user tags if available (Note: UserTags may not be available in all SDK versions)
	// TODO: Implement tag extraction when IBM Cloud SDK supports user tags on subnets
	// For now, we'll use an empty tags map

	return subnetInfo
}
