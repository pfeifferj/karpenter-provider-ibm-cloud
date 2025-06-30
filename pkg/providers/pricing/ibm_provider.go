package pricing

import (
	"context"
	"sync"
	"time"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
)

// IBMPricingProvider implements the Provider interface for IBM Cloud pricing
type IBMPricingProvider struct {
	client     *ibm.Client
	pricingMap map[string]map[string]float64 // instanceType -> zone -> price
	lastUpdate time.Time
	mutex      sync.RWMutex
	ttl        time.Duration
}

// NewIBMPricingProvider creates a new IBM Cloud pricing provider
func NewIBMPricingProvider(client *ibm.Client) *IBMPricingProvider {
	return &IBMPricingProvider{
		client:     client,
		pricingMap: make(map[string]map[string]float64),
		ttl:        12 * time.Hour, // Cache pricing for 12 hours
	}
}

// GetPrice returns the hourly price for the specified instance type in the given zone
func (p *IBMPricingProvider) GetPrice(ctx context.Context, instanceType string, zone string) (float64, error) {
	p.mutex.RLock()
	
	// Check if cache needs refresh
	if time.Since(p.lastUpdate) > p.ttl {
		p.mutex.RUnlock()
		if err := p.Refresh(ctx); err != nil {
			// Return fallback pricing if refresh fails
			return p.getFallbackPricing(instanceType), nil
		}
		p.mutex.RLock()
	}

	if zoneMap, exists := p.pricingMap[instanceType]; exists {
		if price, exists := zoneMap[zone]; exists {
			p.mutex.RUnlock()
			return price, nil
		}
	}
	p.mutex.RUnlock()
	
	// Return fallback pricing if not found
	return p.getFallbackPricing(instanceType), nil
}

// GetPrices returns a map of instance type to price for all instance types in the given zone
func (p *IBMPricingProvider) GetPrices(ctx context.Context, zone string) (map[string]float64, error) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	// Check if cache needs refresh
	if time.Since(p.lastUpdate) > p.ttl {
		p.mutex.RUnlock()
		if err := p.Refresh(ctx); err != nil {
			// Continue with fallback pricing if refresh fails
		}
		p.mutex.RLock()
	}

	prices := make(map[string]float64)
	for instanceType, zoneMap := range p.pricingMap {
		if price, exists := zoneMap[zone]; exists {
			prices[instanceType] = price
		} else {
			// Use fallback pricing if specific zone not found
			prices[instanceType] = p.getFallbackPricing(instanceType)
		}
	}

	// If no cached prices, provide fallback prices
	if len(prices) == 0 {
		prices = p.getFallbackPrices()
	}

	return prices, nil
}

// Refresh updates the cached pricing information
func (p *IBMPricingProvider) Refresh(ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// For now, use static pricing data
	// TODO: Implement real IBM Cloud pricing integration when API is available
	p.pricingMap = map[string]map[string]float64{
		"bx2-2x8": {
			"us-south-1": 0.097,
			"us-south-2": 0.097,
			"us-south-3": 0.097,
		},
		"bx2-4x16": {
			"us-south-1": 0.194,
			"us-south-2": 0.194,
			"us-south-3": 0.194,
		},
		"bx2-8x32": {
			"us-south-1": 0.388,
			"us-south-2": 0.388,
			"us-south-3": 0.388,
		},
		"cx2-2x4": {
			"us-south-1": 0.087,
			"us-south-2": 0.087,
			"us-south-3": 0.087,
		},
		"cx2-4x8": {
			"us-south-1": 0.174,
			"us-south-2": 0.174,
			"us-south-3": 0.174,
		},
		"mx2-2x16": {
			"us-south-1": 0.155,
			"us-south-2": 0.155,
			"us-south-3": 0.155,
		},
	}
	
	p.lastUpdate = time.Now()
	return nil
}

// getFallbackPricing returns fallback pricing for unknown instance types
func (p *IBMPricingProvider) getFallbackPricing(instanceType string) float64 {
	fallbackPrices := map[string]float64{
		"bx2-2x8":   0.097,
		"bx2-4x16":  0.194,
		"bx2-8x32":  0.388,
		"bx2-16x64": 0.776,
		"cx2-2x4":   0.087,
		"cx2-4x8":   0.174,
		"cx2-8x16":  0.348,
		"mx2-2x16":  0.155,
		"mx2-4x32":  0.310,
	}
	
	if price, exists := fallbackPrices[instanceType]; exists {
		return price
	}
	
	// Default fallback for unknown instance types
	return 0.10 // $0.10/hour default
}

// getFallbackPrices returns a complete set of fallback prices
func (p *IBMPricingProvider) getFallbackPrices() map[string]float64 {
	return map[string]float64{
		"bx2-2x8":   0.097,
		"bx2-4x16":  0.194,
		"bx2-8x32":  0.388,
		"bx2-16x64": 0.776,
		"cx2-2x4":   0.087,
		"cx2-4x8":   0.174,
		"cx2-8x16":  0.348,
		"mx2-2x16":  0.155,
		"mx2-4x32":  0.310,
	}
}