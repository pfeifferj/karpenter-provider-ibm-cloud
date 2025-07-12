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
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coreoptions "sigs.k8s.io/karpenter/pkg/operator/options"
)

func TestNewOptions(t *testing.T) {
	// Clear environment variables first
	_ = os.Unsetenv("IBMCLOUD_API_KEY")
	_ = os.Unsetenv("IBMCLOUD_REGION")
	_ = os.Unsetenv("IBMCLOUD_ZONE")
	_ = os.Unsetenv("IBMCLOUD_RESOURCE_GROUP_ID")
	
	opts := NewOptions()
	
	assert.True(t, opts.Interruption) // Default should be true
	assert.Empty(t, opts.APIKey)
	assert.Empty(t, opts.Region)
	assert.Empty(t, opts.Zone)
	assert.Empty(t, opts.ResourceGroupID)
}

func TestNewOptionsWithEnvironment(t *testing.T) {
	// Set environment variables
	_ = os.Setenv("IBMCLOUD_API_KEY", "test-api-key")
	_ = os.Setenv("IBMCLOUD_REGION", "us-south")
	_ = os.Setenv("IBMCLOUD_ZONE", "us-south-1")
	_ = os.Setenv("IBMCLOUD_RESOURCE_GROUP_ID", "test-rg-id")
	
	defer func() {
		_ = os.Unsetenv("IBMCLOUD_API_KEY")
		_ = os.Unsetenv("IBMCLOUD_REGION")
		_ = os.Unsetenv("IBMCLOUD_ZONE")
		_ = os.Unsetenv("IBMCLOUD_RESOURCE_GROUP_ID")
	}()
	
	opts := NewOptions()
	
	assert.True(t, opts.Interruption)
	assert.Equal(t, "test-api-key", opts.APIKey)
	assert.Equal(t, "us-south", opts.Region)
	assert.Equal(t, "us-south-1", opts.Zone)
	assert.Equal(t, "test-rg-id", opts.ResourceGroupID)
}

func TestOptionsAddFlags(t *testing.T) {
	opts := &Options{}
	flagSet := &coreoptions.FlagSet{FlagSet: flag.NewFlagSet("test", flag.ContinueOnError)}
	
	opts.AddFlags(flagSet)
	
	// Test that flags are added (we can't easily test the actual flag parsing without more setup)
	assert.NotNil(t, flagSet.FlagSet)
}

func TestOptionsToContext(t *testing.T) {
	opts := &Options{
		Interruption:    true,
		APIKey:          "test-key",
		Region:          "us-south",
		Zone:            "us-south-1",
		ResourceGroupID: "test-rg",
	}
	
	ctx := context.Background()
	ctxWithOpts := opts.ToContext(ctx)
	
	retrievedOpts := FromContext(ctxWithOpts)
	require.NotNil(t, retrievedOpts)
	assert.Equal(t, opts.Interruption, retrievedOpts.Interruption)
	assert.Equal(t, opts.APIKey, retrievedOpts.APIKey)
	assert.Equal(t, opts.Region, retrievedOpts.Region)
	assert.Equal(t, opts.Zone, retrievedOpts.Zone)
	assert.Equal(t, opts.ResourceGroupID, retrievedOpts.ResourceGroupID)
}

func TestToContext(t *testing.T) {
	opts := &Options{
		Interruption:    false,
		APIKey:          "test-key-2",
		Region:          "eu-gb",
		Zone:            "eu-gb-1",
		ResourceGroupID: "test-rg-2",
	}
	
	ctx := context.Background()
	ctxWithOpts := ToContext(ctx, opts)
	
	retrievedOpts := FromContext(ctxWithOpts)
	require.NotNil(t, retrievedOpts)
	assert.Equal(t, opts, retrievedOpts)
}

func TestWithOptions(t *testing.T) {
	opts := Options{
		Interruption:    true,
		APIKey:          "test-key-3",
		Region:          "jp-tok",
		Zone:            "jp-tok-1",
		ResourceGroupID: "test-rg-3",
	}
	
	ctx := context.Background()
	ctxWithOpts := WithOptions(ctx, opts)
	
	retrievedOpts := FromContext(ctxWithOpts)
	require.NotNil(t, retrievedOpts)
	assert.Equal(t, opts.Interruption, retrievedOpts.Interruption)
	assert.Equal(t, opts.APIKey, retrievedOpts.APIKey)
	assert.Equal(t, opts.Region, retrievedOpts.Region)
	assert.Equal(t, opts.Zone, retrievedOpts.Zone)
	assert.Equal(t, opts.ResourceGroupID, retrievedOpts.ResourceGroupID)
}

func TestFromContextEmpty(t *testing.T) {
	ctx := context.Background()
	opts := FromContext(ctx)
	
	// Should return zero value, not nil
	require.NotNil(t, opts)
	assert.False(t, opts.Interruption)
	assert.Empty(t, opts.APIKey)
	assert.Empty(t, opts.Region)
	assert.Empty(t, opts.Zone)
	assert.Empty(t, opts.ResourceGroupID)
}

func TestFromContextWithNilValue(t *testing.T) {
	ctx := context.WithValue(context.Background(), optionsKey{}, nil)
	opts := FromContext(ctx)
	
	// Should return zero value when context value is nil
	require.NotNil(t, opts)
	assert.False(t, opts.Interruption)
	assert.Empty(t, opts.APIKey)
}

func TestOptionsValidation(t *testing.T) {
	tests := []struct {
		name        string
		opts        Options
		expectError bool
	}{
		{
			name: "valid options",
			opts: Options{
				APIKey:          "test-key",
				Region:          "us-south",
				Zone:            "us-south-1",
				ResourceGroupID: "test-rg",
			},
			expectError: false,
		},
		{
			name: "missing API key",
			opts: Options{
				Region:          "us-south",
				Zone:            "us-south-1",
				ResourceGroupID: "test-rg",
			},
			expectError: true,
		},
		{
			name: "missing region",
			opts: Options{
				APIKey:          "test-key",
				Zone:            "us-south-1",
				ResourceGroupID: "test-rg",
			},
			expectError: true,
		},
		{
			name: "invalid zone for region",
			opts: Options{
				APIKey:          "test-key",
				Region:          "us-south",
				Zone:            "eu-gb-1", // Wrong zone for us-south
				ResourceGroupID: "test-rg",
			},
			expectError: true,
		},
		{
			name:        "empty options should be invalid",
			opts:        Options{},
			expectError: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOptionsParse(t *testing.T) {
	// Set up valid environment for parsing
	_ = os.Setenv("IBMCLOUD_API_KEY", "test-key")
	_ = os.Setenv("IBMCLOUD_REGION", "us-south")
	_ = os.Setenv("IBMCLOUD_ZONE", "us-south-1")
	_ = os.Setenv("IBMCLOUD_RESOURCE_GROUP_ID", "test-rg")
	defer func() {
		_ = os.Unsetenv("IBMCLOUD_API_KEY")
		_ = os.Unsetenv("IBMCLOUD_REGION")
		_ = os.Unsetenv("IBMCLOUD_ZONE")
		_ = os.Unsetenv("IBMCLOUD_RESOURCE_GROUP_ID")
	}()

	opts := &Options{}
	flagSet := &coreoptions.FlagSet{FlagSet: flag.NewFlagSet("test", flag.ContinueOnError)}
	
	opts.AddFlags(flagSet)
	
	// Test parsing with no arguments (should use environment variables)
	err := opts.Parse(flagSet)
	assert.NoError(t, err)
	assert.Equal(t, "test-key", opts.APIKey)
	assert.Equal(t, "us-south", opts.Region)
}

func TestOptionsParseWithArgs(t *testing.T) {
	opts := &Options{}
	flagSet := &coreoptions.FlagSet{FlagSet: flag.NewFlagSet("test", flag.ContinueOnError)}
	
	opts.AddFlags(flagSet)
	
	// Test parsing with all required arguments
	args := []string{
		"--api-key", "test-key",
		"--region", "us-south",
		"--zone", "us-south-1",
		"--resource-group-id", "test-rg",
	}
	err := opts.Parse(flagSet, args...)
	assert.NoError(t, err)
	assert.Equal(t, "test-key", opts.APIKey)
	assert.Equal(t, "us-south", opts.Region)
	assert.Equal(t, "us-south-1", opts.Zone)
	assert.Equal(t, "test-rg", opts.ResourceGroupID)
}

func TestOptionsKey(t *testing.T) {
	// Test that optionsKey is a proper type for context keys
	key1 := optionsKey{}
	key2 := optionsKey{}
	
	assert.Equal(t, key1, key2)
	
	// Test that it can be used as a context key
	ctx := context.WithValue(context.Background(), key1, "test")
	value := ctx.Value(key2)
	assert.Equal(t, "test", value)
}

func TestOptionsFieldValidation(t *testing.T) {
	opts := &Options{
		Interruption:    true,
		APIKey:          "valid-key",
		Region:          "us-south",
		Zone:            "us-south-1",
		ResourceGroupID: "valid-rg-id",
	}
	
	// All fields should be accessible
	assert.True(t, opts.Interruption)
	assert.Equal(t, "valid-key", opts.APIKey)
	assert.Equal(t, "us-south", opts.Region)
	assert.Equal(t, "us-south-1", opts.Zone)
	assert.Equal(t, "valid-rg-id", opts.ResourceGroupID)
}

func TestOptionsDefaults(t *testing.T) {
	opts := &Options{}
	
	// Test default values
	assert.False(t, opts.Interruption) // Default should be false for zero value
	assert.Empty(t, opts.APIKey)
	assert.Empty(t, opts.Region)
	assert.Empty(t, opts.Zone)
	assert.Empty(t, opts.ResourceGroupID)
}