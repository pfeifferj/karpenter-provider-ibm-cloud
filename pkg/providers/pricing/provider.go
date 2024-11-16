package pricing

import (
	"context"
)

// Provider defines the interface for retrieving IBM Cloud pricing information
type Provider interface {
	// GetPrice returns the hourly price for the specified instance type in the given zone
	GetPrice(ctx context.Context, instanceType string, zone string) (float64, error)

	// GetPrices returns a map of instance type to price for all instance types in the given zone
	GetPrices(ctx context.Context, zone string) (map[string]float64, error)

	// Refresh updates the cached pricing information
	Refresh(ctx context.Context) error
}

// Price represents pricing information for an IBM Cloud instance type
type Price struct {
	// InstanceType is the instance type name
	InstanceType string

	// Zone is the availability zone
	Zone string

	// HourlyPrice is the cost per hour for the instance type
	HourlyPrice float64

	// Currency is the currency of the price (e.g., USD)
	Currency string
}
