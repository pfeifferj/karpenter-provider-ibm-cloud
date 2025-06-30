package pricing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPricingProviderStructure(t *testing.T) {
	// Test basic pricing provider structure
	// This test ensures the package compiles correctly
	t.Log("Pricing provider package compiles successfully")
}

func TestNewIBMPricingProvider(t *testing.T) {
	// Test creating a new pricing provider with nil client
	// In real usage, this would have a valid IBM client
	provider := NewIBMPricingProvider(nil)
	
	require.NotNil(t, provider)
	assert.Equal(t, 12*time.Hour, provider.ttl)
	assert.NotNil(t, provider.pricingMap)
}

func TestGetFallbackPricing(t *testing.T) {
	provider := NewIBMPricingProvider(nil)
	
	tests := []struct {
		instanceType string
		expectedPrice float64
	}{
		{"bx2-2x8", 0.097},
		{"bx2-4x16", 0.194},
		{"cx2-2x4", 0.087},
		{"unknown-type", 0.10}, // fallback price
	}
	
	for _, tt := range tests {
		t.Run(tt.instanceType, func(t *testing.T) {
			price := provider.getFallbackPricing(tt.instanceType)
			assert.Equal(t, tt.expectedPrice, price)
		})
	}
}

func TestGetFallbackPrices(t *testing.T) {
	provider := NewIBMPricingProvider(nil)
	
	prices := provider.getFallbackPrices()
	
	require.NotNil(t, prices)
	assert.Greater(t, len(prices), 0)
	
	// Check that some expected instance types are present
	expectedTypes := []string{"bx2-2x8", "bx2-4x16", "cx2-2x4", "mx2-2x16"}
	for _, instanceType := range expectedTypes {
		price, exists := prices[instanceType]
		assert.True(t, exists, "Expected instance type %s to be present", instanceType)
		assert.Greater(t, price, 0.0, "Expected positive price for %s", instanceType)
	}
}

func TestRefresh(t *testing.T) {
	provider := NewIBMPricingProvider(nil)
	ctx := context.Background()
	
	// Test refresh functionality
	err := provider.Refresh(ctx)
	assert.NoError(t, err)
	
	// Check that pricing map was populated
	assert.Greater(t, len(provider.pricingMap), 0)
	assert.False(t, provider.lastUpdate.IsZero())
}

func TestGetPriceWithFallback(t *testing.T) {
	provider := NewIBMPricingProvider(nil)
	ctx := context.Background()
	
	// Test getting price for a known instance type
	price, err := provider.GetPrice(ctx, "bx2-2x8", "us-south-1")
	assert.NoError(t, err)
	assert.Greater(t, price, 0.0)
	
	// Test getting price for unknown instance type (should fallback)
	price, err = provider.GetPrice(ctx, "unknown-type", "us-south-1")
	assert.NoError(t, err)
	assert.Equal(t, 0.10, price) // fallback price
}

func TestGetPricesWithFallback(t *testing.T) {
	provider := NewIBMPricingProvider(nil)
	ctx := context.Background()
	
	// Test getting all prices for a zone
	prices, err := provider.GetPrices(ctx, "us-south-1")
	assert.NoError(t, err)
	assert.Greater(t, len(prices), 0)
	
	// Verify that we get reasonable prices
	for instanceType, price := range prices {
		assert.Greater(t, price, 0.0, "Expected positive price for %s", instanceType)
		t.Logf("Instance type %s: $%.3f/hour", instanceType, price)
	}
}

// Note: Tests that require real IBM Cloud clients are skipped
// until proper mock interfaces are implemented
func TestGetPriceWithRealClient(t *testing.T) {
	t.Skip("Skipping test that requires real IBM Cloud client")
}