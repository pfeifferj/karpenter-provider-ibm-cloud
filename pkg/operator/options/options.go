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
package options

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	coreoptions "sigs.k8s.io/karpenter/pkg/operator/options"
	"sigs.k8s.io/karpenter/pkg/utils/env"
)

func init() {
	coreoptions.Injectables = append(coreoptions.Injectables, &Options{})
}

// Key is the context key for Options
type optionsKey struct{}

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

// AddFlags adds command-line flags for the options
func (o *Options) AddFlags(fs *coreoptions.FlagSet) {
	fs.BoolVarWithEnv(&o.Interruption, "interruption", "INTERRUPTION", true, "Enable interruption controller")
	fs.StringVar(&o.APIKey, "api-key", env.WithDefaultString("IBMCLOUD_API_KEY", ""), "IBM Cloud API key")
	fs.StringVar(&o.Region, "region", env.WithDefaultString("IBMCLOUD_REGION", ""), "IBM Cloud region")
	fs.StringVar(&o.Zone, "zone", env.WithDefaultString("IBMCLOUD_ZONE", ""), "IBM Cloud availability zone")
	fs.StringVar(&o.ResourceGroupID, "resource-group-id", env.WithDefaultString("IBMCLOUD_RESOURCE_GROUP_ID", ""), "IBM Cloud resource group ID")
}

// Parse parses command-line flags
func (o *Options) Parse(fs *coreoptions.FlagSet, args ...string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		return fmt.Errorf("parsing flags, %w", err)
	}
	if err := o.Validate(); err != nil {
		return fmt.Errorf("validating options, %w", err)
	}
	return nil
}

// ToContext adds the options to the context
func (o *Options) ToContext(ctx context.Context) context.Context {
	return ToContext(ctx, o)
}

// FromContext retrieves Options from the context
func FromContext(ctx context.Context) *Options {
	if v := ctx.Value(optionsKey{}); v != nil {
		return v.(*Options)
	}
	// Return zero value instead of nil to prevent panics
	return &Options{}
}

// ToContext returns a new context with the given Options
func ToContext(ctx context.Context, options *Options) context.Context {
	return context.WithValue(ctx, optionsKey{}, options)
}

// WithOptions returns a new context with the given Options (for backwards compatibility)
func WithOptions(ctx context.Context, options Options) context.Context {
	return ToContext(ctx, &options)
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
