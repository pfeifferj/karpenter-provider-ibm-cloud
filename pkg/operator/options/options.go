package options

import (
	"context"
	"os"
)

// Key is the context key for Options
type Key struct{}

// Options contains operator configuration
type Options struct {
	// Interruption enables the interruption controller
	Interruption bool

	// APIKey is the IBM Cloud API key used for authentication
	APIKey string
	// Region is the IBM Cloud region to operate in
	Region string
	// Zone is the availability zone within the region
	Zone string
	// ResourceGroupID is the ID of the resource group to use
	ResourceGroupID string
}

// FromContext retrieves Options from the context
func FromContext(ctx context.Context) Options {
	if v := ctx.Value(Key{}); v != nil {
		return v.(Options)
	}
	return Options{}
}

// WithOptions returns a new context with the given Options
func WithOptions(ctx context.Context, options Options) context.Context {
	return context.WithValue(ctx, Key{}, options)
}

// NewOptions creates a new Options instance with values from environment variables
func NewOptions() Options {
	return Options{
		Interruption:    true, // Enable interruption controller by default
		APIKey:          os.Getenv("IBMCLOUD_API_KEY"),
		Region:          os.Getenv("IBMCLOUD_REGION"),
		Zone:            os.Getenv("IBMCLOUD_ZONE"),
		ResourceGroupID: os.Getenv("IBMCLOUD_RESOURCE_GROUP_ID"),
	}
}
