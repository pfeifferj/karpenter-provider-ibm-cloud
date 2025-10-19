/*
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

package image

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/go-logr/logr"
	"github.com/go-openapi/strfmt"
	"github.com/stretchr/testify/assert"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
)

// MockVPCSDKClient implements the vpcClientInterface for testing
type MockVPCSDKClient struct {
	getImageFunc   func(ctx context.Context, options *vpcv1.GetImageOptions) (*vpcv1.Image, *core.DetailedResponse, error)
	listImagesFunc func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error)
}

func (m *MockVPCSDKClient) GetImageWithContext(ctx context.Context, options *vpcv1.GetImageOptions) (*vpcv1.Image, *core.DetailedResponse, error) {
	if m.getImageFunc != nil {
		return m.getImageFunc(ctx, options)
	}
	return nil, &core.DetailedResponse{}, fmt.Errorf("image not found")
}

func (m *MockVPCSDKClient) ListImagesWithContext(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error) {
	if m.listImagesFunc != nil {
		return m.listImagesFunc(ctx, options)
	}
	return &vpcv1.ImageCollection{Images: []vpcv1.Image{}}, &core.DetailedResponse{}, nil
}

// Implement all required interface methods with no-op implementations
func (m *MockVPCSDKClient) CreateInstanceWithContext(context.Context, *vpcv1.CreateInstanceOptions) (*vpcv1.Instance, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) DeleteInstanceWithContext(context.Context, *vpcv1.DeleteInstanceOptions) (*core.DetailedResponse, error) {
	return &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) GetInstanceWithContext(context.Context, *vpcv1.GetInstanceOptions) (*vpcv1.Instance, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) ListInstancesWithContext(context.Context, *vpcv1.ListInstancesOptions) (*vpcv1.InstanceCollection, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) UpdateInstanceWithContext(context.Context, *vpcv1.UpdateInstanceOptions) (*vpcv1.Instance, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) ListSubnetsWithContext(context.Context, *vpcv1.ListSubnetsOptions) (*vpcv1.SubnetCollection, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) GetSubnetWithContext(context.Context, *vpcv1.GetSubnetOptions) (*vpcv1.Subnet, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) GetVPCWithContext(context.Context, *vpcv1.GetVPCOptions) (*vpcv1.VPC, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) ListInstanceProfilesWithContext(context.Context, *vpcv1.ListInstanceProfilesOptions) (*vpcv1.InstanceProfileCollection, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) GetInstanceProfileWithContext(context.Context, *vpcv1.GetInstanceProfileOptions) (*vpcv1.InstanceProfile, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) ListSecurityGroupsWithContext(context.Context, *vpcv1.ListSecurityGroupsOptions) (*vpcv1.SecurityGroupCollection, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

// Volume methods
func (m *MockVPCSDKClient) ListVolumesWithContext(context.Context, *vpcv1.ListVolumesOptions) (*vpcv1.VolumeCollection, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) DeleteVolumeWithContext(context.Context, *vpcv1.DeleteVolumeOptions) (*core.DetailedResponse, error) {
	return &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

// Virtual Network Interface methods
func (m *MockVPCSDKClient) ListVirtualNetworkInterfacesWithContext(context.Context, *vpcv1.ListVirtualNetworkInterfacesOptions) (*vpcv1.VirtualNetworkInterfaceCollection, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) DeleteVirtualNetworkInterfacesWithContext(context.Context, *vpcv1.DeleteVirtualNetworkInterfacesOptions) (*vpcv1.VirtualNetworkInterface, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

// Load Balancer methods
func (m *MockVPCSDKClient) GetLoadBalancerWithContext(context.Context, *vpcv1.GetLoadBalancerOptions) (*vpcv1.LoadBalancer, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) ListLoadBalancerPoolsWithContext(context.Context, *vpcv1.ListLoadBalancerPoolsOptions) (*vpcv1.LoadBalancerPoolCollection, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) GetLoadBalancerPoolWithContext(context.Context, *vpcv1.GetLoadBalancerPoolOptions) (*vpcv1.LoadBalancerPool, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) CreateLoadBalancerPoolMemberWithContext(context.Context, *vpcv1.CreateLoadBalancerPoolMemberOptions) (*vpcv1.LoadBalancerPoolMember, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) DeleteLoadBalancerPoolMemberWithContext(context.Context, *vpcv1.DeleteLoadBalancerPoolMemberOptions) (*core.DetailedResponse, error) {
	return &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) GetLoadBalancerPoolMemberWithContext(context.Context, *vpcv1.GetLoadBalancerPoolMemberOptions) (*vpcv1.LoadBalancerPoolMember, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) ListLoadBalancerPoolMembersWithContext(context.Context, *vpcv1.ListLoadBalancerPoolMembersOptions) (*vpcv1.LoadBalancerPoolMemberCollection, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) UpdateLoadBalancerPoolMemberWithContext(context.Context, *vpcv1.UpdateLoadBalancerPoolMemberOptions) (*vpcv1.LoadBalancerPoolMember, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) UpdateLoadBalancerPoolWithContext(context.Context, *vpcv1.UpdateLoadBalancerPoolOptions) (*vpcv1.LoadBalancerPool, *core.DetailedResponse, error) {
	return nil, &core.DetailedResponse{}, fmt.Errorf("not implemented")
}

func (m *MockVPCSDKClient) ListRegionZonesWithContext(context.Context, *vpcv1.ListRegionZonesOptions) (*vpcv1.ZoneCollection, *core.DetailedResponse, error) {
	// Return default zones for testing
	zone1 := "us-south-1"
	zone2 := "us-south-2"
	zone3 := "us-south-3"
	return &vpcv1.ZoneCollection{
		Zones: []vpcv1.Zone{
			{Name: &zone1},
			{Name: &zone2},
			{Name: &zone3},
		},
	}, &core.DetailedResponse{}, nil
}

func (m *MockVPCSDKClient) ListRegions(*vpcv1.ListRegionsOptions) (*vpcv1.RegionCollection, *core.DetailedResponse, error) {
	// Return default regions for testing
	region1 := "us-south"
	region2 := "us-east"
	region3 := "eu-de"
	status := "available"
	return &vpcv1.RegionCollection{
		Regions: []vpcv1.Region{
			{Name: &region1, Status: &status},
			{Name: &region2, Status: &status},
			{Name: &region3, Status: &status},
		},
	}, &core.DetailedResponse{}, nil
}

func (m *MockVPCSDKClient) GetSecurityGroupWithContext(context.Context, *vpcv1.GetSecurityGroupOptions) (*vpcv1.SecurityGroup, *core.DetailedResponse, error) {
	return &vpcv1.SecurityGroup{}, &core.DetailedResponse{}, nil
}

func (m *MockVPCSDKClient) GetKeyWithContext(context.Context, *vpcv1.GetKeyOptions) (*vpcv1.Key, *core.DetailedResponse, error) {
	return &vpcv1.Key{}, &core.DetailedResponse{}, nil
}

func TestNewResolver(t *testing.T) {
	// Create a mock SDK client that implements the interface
	mockSDKClient := &MockVPCSDKClient{}
	vpcClient := ibm.NewVPCClientWithMock(mockSDKClient)
	region := "us-south"
	logger := logr.Discard()

	resolver := NewResolver(vpcClient, region, logger)

	assert.NotNil(t, resolver)
	assert.Equal(t, region, resolver.region)
	assert.Equal(t, vpcClient, resolver.vpcClient)
	assert.Equal(t, logger, resolver.logger)
}

func TestResolveImage_WithImageID(t *testing.T) {
	tests := []struct {
		name        string
		imageID     string
		mockFunc    func(ctx context.Context, options *vpcv1.GetImageOptions) (*vpcv1.Image, *core.DetailedResponse, error)
		expected    string
		expectError bool
	}{
		{
			name:    "valid image ID",
			imageID: "r006-12345678-1234-1234-1234-123456789012",
			mockFunc: func(ctx context.Context, options *vpcv1.GetImageOptions) (*vpcv1.Image, *core.DetailedResponse, error) {
				imageID := *options.ID
				return &vpcv1.Image{
					ID:   &imageID,
					Name: stringPtr("ubuntu-20-04"),
				}, &core.DetailedResponse{}, nil
			},
			expected:    "r006-12345678-1234-1234-1234-123456789012",
			expectError: false,
		},
		{
			name:    "image ID not found",
			imageID: "r006-12345678-1234-1234-1234-123456789012",
			mockFunc: func(ctx context.Context, options *vpcv1.GetImageOptions) (*vpcv1.Image, *core.DetailedResponse, error) {
				return nil, &core.DetailedResponse{}, fmt.Errorf("image not found")
			},
			expected:    "",
			expectError: true,
		},
		{
			name:        "empty image identifier",
			imageID:     "",
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSDKClient := &MockVPCSDKClient{
				getImageFunc: tt.mockFunc,
			}
			vpcClient := ibm.NewVPCClientWithMock(mockSDKClient)
			resolver := NewResolver(vpcClient, "us-south", logr.Discard())

			result, err := resolver.ResolveImage(context.Background(), tt.imageID)

			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, tt.expected, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestResolveImage_WithImageName(t *testing.T) {
	tests := []struct {
		name        string
		imageName   string
		mockFunc    func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error)
		expected    string
		expectError bool
	}{
		{
			name:      "exact name match",
			imageName: "ubuntu-20-04",
			mockFunc: func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error) {
				now := time.Now()
				return &vpcv1.ImageCollection{
					Images: []vpcv1.Image{
						{
							ID:        stringPtr("r006-12345678-1234-1234-1234-123456789012"),
							Name:      stringPtr("ubuntu-20-04"),
							CreatedAt: createStrfmtDateTime(now),
						},
					},
				}, &core.DetailedResponse{}, nil
			},
			expected:    "r006-12345678-1234-1234-1234-123456789012",
			expectError: false,
		},
		{
			name:      "pattern match with multiple images",
			imageName: "ubuntu",
			mockFunc: func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error) {
				now := time.Now()
				return &vpcv1.ImageCollection{
					Images: []vpcv1.Image{
						{
							ID:        stringPtr("r006-12345678-1234-1234-1234-123456789012"),
							Name:      stringPtr("ubuntu-18-04"),
							CreatedAt: createStrfmtDateTime(now.Add(-time.Hour)),
						},
						{
							ID:        stringPtr("r006-87654321-4321-4321-4321-210987654321"),
							Name:      stringPtr("ubuntu-20-04"),
							CreatedAt: createStrfmtDateTime(now), // Most recent
						},
					},
				}, &core.DetailedResponse{}, nil
			},
			expected:    "r006-87654321-4321-4321-4321-210987654321", // Most recent
			expectError: false,
		},
		{
			name:      "no matching images",
			imageName: "nonexistent",
			mockFunc: func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error) {
				return &vpcv1.ImageCollection{Images: []vpcv1.Image{}}, &core.DetailedResponse{}, nil
			},
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSDKClient := &MockVPCSDKClient{
				listImagesFunc: tt.mockFunc,
			}
			vpcClient := ibm.NewVPCClientWithMock(mockSDKClient)
			resolver := NewResolver(vpcClient, "us-south", logr.Discard())

			result, err := resolver.ResolveImage(context.Background(), tt.imageName)

			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, tt.expected, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestListAvailableImages(t *testing.T) {
	tests := []struct {
		name        string
		nameFilter  string
		mockFunc    func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error)
		expectedLen int
		expectError bool
	}{
		{
			name:       "successful listing",
			nameFilter: "ubuntu",
			mockFunc: func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error) {
				now := time.Now()
				return &vpcv1.ImageCollection{
					Images: []vpcv1.Image{
						{
							ID:        stringPtr("r006-12345678-1234-1234-1234-123456789012"),
							Name:      stringPtr("ubuntu-20-04"),
							CreatedAt: createStrfmtDateTime(now),
							Status:    stringPtr("available"),
							OperatingSystem: &vpcv1.OperatingSystem{
								Name: stringPtr("ubuntu-20-04-amd64"),
							},
						},
						{
							ID:        stringPtr("r006-87654321-4321-4321-4321-210987654321"),
							Name:      stringPtr("ubuntu-18-04"),
							CreatedAt: createStrfmtDateTime(now.Add(-time.Hour)),
							Status:    stringPtr("available"),
							OperatingSystem: &vpcv1.OperatingSystem{
								Name: stringPtr("ubuntu-18-04-amd64"),
							},
						},
					},
				}, &core.DetailedResponse{}, nil
			},
			expectedLen: 2,
			expectError: false,
		},
		{
			name:       "API error",
			nameFilter: "ubuntu",
			mockFunc: func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error) {
				return nil, &core.DetailedResponse{}, fmt.Errorf("API error")
			},
			expectedLen: 0,
			expectError: true,
		},
		{
			name:       "empty results",
			nameFilter: "nonexistent",
			mockFunc: func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error) {
				return &vpcv1.ImageCollection{Images: []vpcv1.Image{}}, &core.DetailedResponse{}, nil
			},
			expectedLen: 0,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSDKClient := &MockVPCSDKClient{
				listImagesFunc: tt.mockFunc,
			}
			vpcClient := ibm.NewVPCClientWithMock(mockSDKClient)
			resolver := NewResolver(vpcClient, "us-south", logr.Discard())

			result, err := resolver.ListAvailableImages(context.Background(), tt.nameFilter)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Len(t, result, tt.expectedLen)
			}
		})
	}
}

func TestIsImageID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid image ID",
			input:    "r006-12345678-1234-1234-1234-123456789abc",
			expected: true,
		},
		{
			name:     "valid image ID with different region",
			input:    "r014-87654321-4321-4321-4321-cba987654321",
			expected: true,
		},
		{
			name:     "image name",
			input:    "ubuntu-20-04-minimal",
			expected: false,
		},
		{
			name:     "short string",
			input:    "r006",
			expected: false,
		},
		{
			name:     "doesn't start with r",
			input:    "a006-12345678-1234-1234-1234-123456789abc",
			expected: false,
		},
		{
			name:     "invalid region format",
			input:    "rabc-12345678-1234-1234-1234-123456789abc",
			expected: false,
		},
		{
			name:     "too few UUID parts",
			input:    "r006-12345678",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isImageID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesImageName(t *testing.T) {
	tests := []struct {
		name      string
		imageName string
		pattern   string
		expected  bool
	}{
		{
			name:      "exact match",
			imageName: "ubuntu-20-04",
			pattern:   "ubuntu-20-04",
			expected:  true,
		},
		{
			name:      "contains match",
			imageName: "ibm-ubuntu-20-04-minimal-amd64-2",
			pattern:   "ubuntu",
			expected:  true,
		},
		{
			name:      "case insensitive",
			imageName: "IBM-Ubuntu-20-04",
			pattern:   "ubuntu",
			expected:  true,
		},
		{
			name:      "wildcard prefix",
			imageName: "ibm-ubuntu-20-04-minimal",
			pattern:   "*ubuntu*",
			expected:  true,
		},
		{
			name:      "wildcard suffix",
			imageName: "ubuntu-20-04-minimal",
			pattern:   "ubuntu*",
			expected:  true,
		},
		{
			name:      "wildcard prefix only",
			imageName: "ibm-ubuntu-20-04",
			pattern:   "*04",
			expected:  true,
		},
		{
			name:      "no match",
			imageName: "centos-7-9",
			pattern:   "ubuntu",
			expected:  false,
		},
		{
			name:      "wildcard no match",
			imageName: "centos-7-9",
			pattern:   "*ubuntu*",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesImageName(tt.imageName, tt.pattern)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestImageSorting(t *testing.T) {
	now := time.Now()

	mockSDKClient := &MockVPCSDKClient{
		listImagesFunc: func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error) {
			return &vpcv1.ImageCollection{
				Images: []vpcv1.Image{
					{
						ID:        stringPtr("r006-old"),
						Name:      stringPtr("ubuntu-18-04"),
						CreatedAt: createStrfmtDateTime(now.Add(-2 * time.Hour)),
					},
					{
						ID:        stringPtr("r006-newest"),
						Name:      stringPtr("ubuntu-20-04"),
						CreatedAt: createStrfmtDateTime(now),
					},
					{
						ID:        stringPtr("r006-middle"),
						Name:      stringPtr("ubuntu-19-04"),
						CreatedAt: createStrfmtDateTime(now.Add(-time.Hour)),
					},
				},
			}, &core.DetailedResponse{}, nil
		},
	}
	vpcClient := ibm.NewVPCClientWithMock(mockSDKClient)
	resolver := NewResolver(vpcClient, "us-south", logr.Discard())

	// Test that the resolver returns the most recent image
	result, err := resolver.ResolveImage(context.Background(), "ubuntu")
	assert.NoError(t, err)
	assert.Equal(t, "r006-newest", result)
}

func TestEdgeCases(t *testing.T) {
	t.Run("empty image name in ResolveImage", func(t *testing.T) {
		mockSDKClient := &MockVPCSDKClient{}
		vpcClient := ibm.NewVPCClientWithMock(mockSDKClient)
		resolver := NewResolver(vpcClient, "us-south", logr.Discard())

		result, err := resolver.ResolveImage(context.Background(), "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
		assert.Empty(t, result)
	})

	t.Run("image with nil ID", func(t *testing.T) {
		mockSDKClient := &MockVPCSDKClient{
			listImagesFunc: func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error) {
				now := time.Now()
				return &vpcv1.ImageCollection{
					Images: []vpcv1.Image{
						{
							ID:        nil, // nil ID
							Name:      stringPtr("ubuntu-20-04"),
							CreatedAt: createStrfmtDateTime(now),
						},
					},
				}, &core.DetailedResponse{}, nil
			},
		}
		vpcClient := ibm.NewVPCClientWithMock(mockSDKClient)
		resolver := NewResolver(vpcClient, "us-south", logr.Discard())

		result, err := resolver.ResolveImage(context.Background(), "ubuntu")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "has no ID")
		assert.Empty(t, result)
	})

	t.Run("pattern matching edge cases", func(t *testing.T) {
		tests := []struct {
			pattern   string
			imageName string
			expected  bool
		}{
			{"*", "ubuntu-20-04", true}, // Single wildcard
			{"", "ubuntu-20-04", true},  // Empty pattern matches everything via contains check
		}

		for _, tt := range tests {
			result := matchesImageName(tt.imageName, tt.pattern)
			assert.Equal(t, tt.expected, result, "Pattern %s should match %s: %v", tt.pattern, tt.imageName, tt.expected)
		}
	})
}

// createStrfmtDateTime converts time.Time to *strfmt.DateTime for IBM VPC SDK compatibility
func createStrfmtDateTime(t time.Time) *strfmt.DateTime {
	dt := strfmt.DateTime(t)
	return &dt
}

// Test ResolveImageBySelector functionality
func TestResolver_ResolveImageBySelector(t *testing.T) {
	t.Run("successful image selection by selector", func(t *testing.T) {
		now := time.Now()
		mockSDKClient := &MockVPCSDKClient{
			listImagesFunc: func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error) {
				return &vpcv1.ImageCollection{
					Images: []vpcv1.Image{
						{
							ID:        stringPtr("r006-ubuntu-20-04-1"),
							Name:      stringPtr("ibm-ubuntu-20-04-minimal-amd64-1"),
							CreatedAt: createStrfmtDateTime(now.Add(-time.Hour)),
							Status:    stringPtr("available"),
						},
						{
							ID:        stringPtr("r006-ubuntu-20-04-2"),
							Name:      stringPtr("ibm-ubuntu-20-04-minimal-amd64-2"),
							CreatedAt: createStrfmtDateTime(now), // Most recent
							Status:    stringPtr("available"),
						},
						{
							ID:        stringPtr("r006-ubuntu-22-04-1"),
							Name:      stringPtr("ibm-ubuntu-22-04-minimal-amd64-1"),
							CreatedAt: createStrfmtDateTime(now.Add(-2 * time.Hour)),
							Status:    stringPtr("available"),
						},
					},
				}, &core.DetailedResponse{}, nil
			},
		}

		mockVPCClient := ibm.NewVPCClientWithMock(mockSDKClient)

		resolver := NewResolver(mockVPCClient, "us-south", logr.Discard())

		selector := &v1alpha1.ImageSelector{
			OS:           "ubuntu",
			MajorVersion: "20",
			MinorVersion: "04",
			Architecture: "amd64",
			Variant:      "minimal",
		}

		result, err := resolver.ResolveImageBySelector(context.Background(), selector)
		assert.NoError(t, err)
		assert.Equal(t, "r006-ubuntu-20-04-2", result) // Should select the most recent one
	})

	t.Run("image selection with latest minor version", func(t *testing.T) {
		now := time.Now()
		mockSDKClient := &MockVPCSDKClient{
			listImagesFunc: func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error) {
				return &vpcv1.ImageCollection{
					Images: []vpcv1.Image{
						{
							ID:        stringPtr("r006-ubuntu-20-04-1"),
							Name:      stringPtr("ibm-ubuntu-20-04-minimal-amd64-1"),
							CreatedAt: createStrfmtDateTime(now.Add(-time.Hour)),
							Status:    stringPtr("available"),
						},
						{
							ID:        stringPtr("r006-ubuntu-20-10-1"),
							Name:      stringPtr("ibm-ubuntu-20-10-minimal-amd64-1"),
							CreatedAt: createStrfmtDateTime(now), // Most recent and higher minor version
							Status:    stringPtr("available"),
						},
					},
				}, &core.DetailedResponse{}, nil
			},
		}

		mockVPCClient := ibm.NewVPCClientWithMock(mockSDKClient)

		resolver := NewResolver(mockVPCClient, "us-south", logr.Discard())

		selector := &v1alpha1.ImageSelector{
			OS:           "ubuntu",
			MajorVersion: "20",
			// MinorVersion not specified - should pick latest
			Architecture: "amd64",
			Variant:      "minimal",
		}

		result, err := resolver.ResolveImageBySelector(context.Background(), selector)
		assert.NoError(t, err)
		assert.Equal(t, "r006-ubuntu-20-10-1", result) // Should select the latest minor version
	})

	t.Run("no matching images", func(t *testing.T) {
		mockSDKClient := &MockVPCSDKClient{
			listImagesFunc: func(ctx context.Context, options *vpcv1.ListImagesOptions) (*vpcv1.ImageCollection, *core.DetailedResponse, error) {
				return &vpcv1.ImageCollection{
					Images: []vpcv1.Image{
						{
							ID:        stringPtr("r006-centos-8-1"),
							Name:      stringPtr("ibm-centos-8-01-minimal-amd64-1"),
							CreatedAt: createStrfmtDateTime(time.Now()),
							Status:    stringPtr("available"),
						},
					},
				}, &core.DetailedResponse{}, nil
			},
		}

		mockVPCClient := ibm.NewVPCClientWithMock(mockSDKClient)

		resolver := NewResolver(mockVPCClient, "us-south", logr.Discard())

		selector := &v1alpha1.ImageSelector{
			OS:           "ubuntu",
			MajorVersion: "20",
			MinorVersion: "04",
			Architecture: "amd64",
			Variant:      "minimal",
		}

		_, err := resolver.ResolveImageBySelector(context.Background(), selector)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no images found matching selector")
	})

	t.Run("nil selector", func(t *testing.T) {
		mockVPCClient := &ibm.VPCClient{}
		resolver := NewResolver(mockVPCClient, "us-south", logr.Discard())

		_, err := resolver.ResolveImageBySelector(context.Background(), nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "image selector cannot be nil")
	})
}

// Test parseImageName functionality
func TestResolver_parseImageName(t *testing.T) {
	resolver := &Resolver{}

	testCases := []struct {
		name        string
		imageName   string
		expected    map[string]string
		shouldBeNil bool
	}{
		{
			name:      "newer IBM format with patch version",
			imageName: "ibm-ubuntu-22-04-5-minimal-amd64-7",
			expected: map[string]string{
				"os":           "ubuntu",
				"majorVersion": "22",
				"minorVersion": "04",
				"patchVersion": "5",
				"variant":      "minimal",
				"architecture": "amd64",
				"build":        "7",
			},
		},
		{
			name:      "standard IBM format",
			imageName: "ibm-ubuntu-22-04-minimal-amd64-1",
			expected: map[string]string{
				"os":           "ubuntu",
				"majorVersion": "22",
				"minorVersion": "04",
				"variant":      "minimal",
				"architecture": "amd64",
				"build":        "1",
			},
		},
		{
			name:      "alternative IBM format",
			imageName: "ibm-rhel-8-6-amd64-2",
			expected: map[string]string{
				"os":           "rhel",
				"majorVersion": "8",
				"minorVersion": "6",
				"variant":      "minimal",
				"architecture": "amd64",
				"build":        "2",
			},
		},
		{
			name:      "simple format",
			imageName: "ubuntu-20-04",
			expected: map[string]string{
				"os":           "ubuntu",
				"majorVersion": "20",
				"minorVersion": "04",
				"variant":      "minimal",
				"architecture": "amd64",
				"build":        "1",
			},
		},
		{
			name:        "unparseable format",
			imageName:   "invalid-image-name",
			shouldBeNil: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := resolver.parseImageName(tc.imageName)
			if tc.shouldBeNil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}
