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
package pricing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/IBM/platform-services-go-sdk/globalcatalogv1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cache"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/logging"
)

// IBMPricingProvider implements the Provider interface for IBM Cloud pricing
type IBMPricingProvider struct {
	client     *ibm.Client
	pricingMap map[string]map[string]float64 // instanceType -> zone -> price
	lastUpdate time.Time
	mutex      sync.RWMutex
	ttl        time.Duration
	priceCache *cache.Cache
	logger     *logging.Logger
}

// NewIBMPricingProvider creates a new IBM Cloud pricing provider
func NewIBMPricingProvider(client *ibm.Client) *IBMPricingProvider {
	return &IBMPricingProvider{
		client:     client,
		pricingMap: make(map[string]map[string]float64),
		ttl:        12 * time.Hour,            // Cache pricing for 12 hours
		priceCache: cache.New(12 * time.Hour), // Use new cache infrastructure
		logger:     logging.PricingLogger(),
	}
}

// GetPrice returns the hourly price for the specified instance type in the given zone
func (p *IBMPricingProvider) GetPrice(ctx context.Context, instanceType string, zone string) (float64, error) {
	cacheKey := fmt.Sprintf("price:%s:%s", instanceType, zone)

	// Try to get from cache first
	if cached, exists := p.priceCache.Get(cacheKey); exists {
		return cached.(float64), nil
	}

	p.mutex.RLock()

	// Check if cache needs refresh
	if time.Since(p.lastUpdate) > p.ttl {
		p.mutex.RUnlock()
		if err := p.Refresh(ctx); err != nil {
			// Log error but continue with cached data if available
			p.logger.Warn("Failed to refresh pricing data", "error", err)
		}
		p.mutex.RLock()
	}

	if zoneMap, exists := p.pricingMap[instanceType]; exists {
		if price, exists := zoneMap[zone]; exists {
			p.mutex.RUnlock()
			// Cache the result
			p.priceCache.Set(cacheKey, price)
			return price, nil
		}
	}
	p.mutex.RUnlock()

	// No pricing data available - return error instead of fallback
	return 0, fmt.Errorf("no pricing data available for instance type %s in zone %s", instanceType, zone)
}

// GetPrices returns a map of instance type to price for all instance types in the given zone
func (p *IBMPricingProvider) GetPrices(ctx context.Context, zone string) (map[string]float64, error) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	// Check if cache needs refresh
	if time.Since(p.lastUpdate) > p.ttl {
		p.mutex.RUnlock()
		if err := p.Refresh(ctx); err != nil {
			// Log error but continue with cached data if available
			p.logger.Warn("Failed to refresh pricing data", "error", err)
		}
		p.mutex.RLock()
	}

	prices := make(map[string]float64)
	for instanceType, zoneMap := range p.pricingMap {
		if price, exists := zoneMap[zone]; exists {
			prices[instanceType] = price
		}
		// Skip instance types without pricing data for this zone
	}

	// Return empty map if no pricing data available
	if len(prices) == 0 {
		return nil, fmt.Errorf("no pricing data available for zone %s", zone)
	}

	return prices, nil
}

// Refresh updates the cached pricing information using IBM Cloud API
func (p *IBMPricingProvider) Refresh(ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// If no client available, return error
	if p.client == nil {
		return fmt.Errorf("IBM client not available for pricing API calls")
	}

	// Try to fetch pricing data from IBM Cloud API
	newPricingMap, err := p.fetchPricingData(ctx)
	if err != nil {
		// API failed - don't update lastUpdate to allow retry
		return fmt.Errorf("failed to fetch pricing data from IBM Cloud API: %w", err)
	}

	// Successfully fetched from API - update cache
	p.pricingMap = newPricingMap
	p.lastUpdate = time.Now()
	return nil
}

// fetchPricingData fetches pricing from IBM Cloud Global Catalog API
func (p *IBMPricingProvider) fetchPricingData(ctx context.Context) (map[string]map[string]float64, error) {
	if p.client == nil {
		return nil, fmt.Errorf("IBM client not initialized")
	}

	// Get the catalog client
	catalogClient, err := p.client.GetGlobalCatalogClient()
	if err != nil {
		return nil, fmt.Errorf("getting catalog client: %w", err)
	}

	// Fetch all instance types from catalog
	instanceTypes, err := catalogClient.ListInstanceTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing instance types: %w", err)
	}

	pricingMap := make(map[string]map[string]float64)

	// Define IBM Cloud regions and their zones
	regionZones := map[string][]string{
		"us-south": {"us-south-1", "us-south-2", "us-south-3"},
		"us-east":  {"us-east-1", "us-east-2", "us-east-3"},
		"eu-gb":    {"eu-gb-1", "eu-gb-2", "eu-gb-3"},
		"eu-de":    {"eu-de-1", "eu-de-2", "eu-de-3"},
		"jp-tok":   {"jp-tok-1", "jp-tok-2", "jp-tok-3"},
		"au-syd":   {"au-syd-1", "au-syd-2", "au-syd-3"},
	}

	// Process each instance type
	for _, entry := range instanceTypes {
		if entry.Name == nil {
			continue
		}

		instanceTypeName := *entry.Name

		// Fetch pricing for this instance type
		price, err := p.fetchInstancePricing(ctx, catalogClient, entry)
		if err != nil {
			// Skip this instance type if pricing unavailable
			fmt.Printf("Warning: Skipping instance type %s due to pricing error: %v\n", instanceTypeName, err)
			continue
		}

		// Initialize map for this instance type
		pricingMap[instanceTypeName] = make(map[string]float64)

		// IBM Cloud pricing is typically uniform across zones in a region
		// Set the same price for all zones across all regions
		for _, zones := range regionZones {
			for _, zone := range zones {
				pricingMap[instanceTypeName][zone] = price
			}
		}
	}

	return pricingMap, nil
}

// fetchInstancePricing fetches pricing for a specific instance type from IBM Cloud API
func (p *IBMPricingProvider) fetchInstancePricing(ctx context.Context, catalogClient *ibm.GlobalCatalogClient, entry globalcatalogv1.CatalogEntry) (float64, error) {
	if entry.ID == nil {
		return 0, fmt.Errorf("catalog entry ID is nil")
	}

	// Use IBM Cloud GetPricing API to fetch pricing data
	catalogEntryID := *entry.ID

	// Access the underlying global catalog client to use GetPricing
	// This requires extending our catalog client interface
	price, err := p.fetchPricingFromAPI(ctx, catalogEntryID)
	if err != nil {
		return 0, fmt.Errorf("fetching pricing for %s: %w", *entry.Name, err)
	}

	return price, nil
}

// fetchPricingFromAPI calls IBM Cloud GetPricing API for the catalog entry
func (p *IBMPricingProvider) fetchPricingFromAPI(ctx context.Context, catalogEntryID string) (float64, error) {
	// Get Global Catalog client which handles authentication internally
	catalogClient, err := p.client.GetGlobalCatalogClient()
	if err != nil {
		return 0, fmt.Errorf("getting catalog client: %w", err)
	}

	// Call GetPricing API through our catalog client
	pricingData, err := catalogClient.GetPricing(ctx, catalogEntryID)
	if err != nil {
		return 0, fmt.Errorf("calling GetPricing API: %w", err)
	}

	// Extract pricing from response - based on tools/gen_instance_types.go pattern
	if pricingData.Metrics != nil {
		for _, metric := range pricingData.Metrics {
			if metric.Amounts != nil {
				for _, amount := range metric.Amounts {
					if amount.Country != nil && *amount.Country == "USA" {
						if amount.Prices != nil {
							for _, priceObj := range amount.Prices {
								if priceObj.Price != nil {
									return *priceObj.Price, nil
								}
							}
						}
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("no pricing data found in API response")
}
