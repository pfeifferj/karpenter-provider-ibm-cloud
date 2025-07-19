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
package status

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cache"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/vpc/subnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockSubnetProvider is a mock implementation of subnet.Provider
type MockSubnetProvider struct {
	mock.Mock
}

func (m *MockSubnetProvider) ListSubnets(ctx context.Context, vpcID string) ([]subnet.SubnetInfo, error) {
	args := m.Called(ctx, vpcID)
	return args.Get(0).([]subnet.SubnetInfo), args.Error(1)
}

func (m *MockSubnetProvider) GetSubnet(ctx context.Context, subnetID string) (*subnet.SubnetInfo, error) {
	args := m.Called(ctx, subnetID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*subnet.SubnetInfo), args.Error(1)
}

func (m *MockSubnetProvider) SelectSubnets(ctx context.Context, vpcID string, strategy *v1alpha1.PlacementStrategy) ([]subnet.SubnetInfo, error) {
	args := m.Called(ctx, vpcID, strategy)
	return args.Get(0).([]subnet.SubnetInfo), args.Error(1)
}

func TestValidateZoneSubnetCompatibility(t *testing.T) {
	tests := []struct {
		name          string
		zone          string
		subnetID      string
		setupMock     func(*MockSubnetProvider)
		setupCache    func(*cache.Cache)
		expectedError string
	}{
		{
			name:     "valid zone-subnet combination",
			zone:     "us-south-1",
			subnetID: "subnet-12345",
			setupMock: func(m *MockSubnetProvider) {
				m.On("GetSubnet", mock.Anything, "subnet-12345").Return(&subnet.SubnetInfo{
					ID:           "subnet-12345",
					Zone:         "us-south-1",
					State:        "available",
					AvailableIPs: 100,
				}, nil)
			},
			setupCache:    func(c *cache.Cache) {},
			expectedError: "",
		},
		{
			name:     "zone-subnet mismatch",
			zone:     "us-south-1",
			subnetID: "subnet-12345",
			setupMock: func(m *MockSubnetProvider) {
				m.On("GetSubnet", mock.Anything, "subnet-12345").Return(&subnet.SubnetInfo{
					ID:           "subnet-12345",
					Zone:         "us-south-2",
					State:        "available",
					AvailableIPs: 100,
				}, nil)
			},
			setupCache:    func(c *cache.Cache) {},
			expectedError: "subnet subnet-12345 is in zone us-south-2, but requested zone is us-south-1. Subnets cannot span multiple zones in IBM Cloud VPC",
		},
		{
			name:     "subnet not available",
			zone:     "us-south-1",
			subnetID: "subnet-12345",
			setupMock: func(m *MockSubnetProvider) {
				m.On("GetSubnet", mock.Anything, "subnet-12345").Return(&subnet.SubnetInfo{
					ID:           "subnet-12345",
					Zone:         "us-south-1",
					State:        "pending",
					AvailableIPs: 100,
				}, nil)
			},
			setupCache:    func(c *cache.Cache) {},
			expectedError: "subnet subnet-12345 in zone us-south-1 is not in available state (current state: pending)",
		},
		{
			name:     "subnet not found",
			zone:     "us-south-1",
			subnetID: "subnet-12345",
			setupMock: func(m *MockSubnetProvider) {
				m.On("GetSubnet", mock.Anything, "subnet-12345").Return(nil, fmt.Errorf("subnet not found"))
			},
			setupCache:    func(c *cache.Cache) {},
			expectedError: "failed to get subnet information: subnet not found",
		},
		{
			name:     "valid zone-subnet from cache",
			zone:     "us-south-1",
			subnetID: "subnet-12345",
			setupMock: func(m *MockSubnetProvider) {
				// Should not be called due to cache hit
			},
			setupCache: func(c *cache.Cache) {
				c.SetWithTTL("subnet-zone-subnet-12345", "us-south-1", 15*time.Minute)
			},
			expectedError: "",
		},
		{
			name:     "zone-subnet mismatch from cache",
			zone:     "us-south-1",
			subnetID: "subnet-12345",
			setupMock: func(m *MockSubnetProvider) {
				// Should not be called due to cache hit
			},
			setupCache: func(c *cache.Cache) {
				c.SetWithTTL("subnet-zone-subnet-12345", "us-south-2", 15*time.Minute)
			},
			expectedError: "subnet subnet-12345 is in zone us-south-2, but requested zone is us-south-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock subnet provider
			mockProvider := new(MockSubnetProvider)
			if tt.setupMock != nil {
				tt.setupMock(mockProvider)
			}

			// Create cache
			testCache := cache.New(15 * time.Minute)
			if tt.setupCache != nil {
				tt.setupCache(testCache)
			}

			// Create controller with mock
			controller := &Controller{
				subnetProvider: mockProvider,
				cache:          testCache,
			}

			// Test the validation
			ctx := context.Background()
			err := controller.validateZoneSubnetCompatibility(ctx, tt.zone, tt.subnetID)

			if tt.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			}

			// Verify mock expectations
			mockProvider.AssertExpectations(t)
		})
	}
}

func TestValidateZoneSubnetCompatibility_CacheBehavior(t *testing.T) {
	mockProvider := new(MockSubnetProvider)
	testCache := cache.New(15 * time.Minute)
	controller := &Controller{
		subnetProvider: mockProvider,
		cache:          testCache,
	}

	// Setup mock to be called only once
	mockProvider.On("GetSubnet", mock.Anything, "subnet-12345").Return(&subnet.SubnetInfo{
		ID:           "subnet-12345",
		Zone:         "us-south-1",
		State:        "available",
		AvailableIPs: 100,
	}, nil).Once()

	ctx := context.Background()

	// First call should hit the API
	err := controller.validateZoneSubnetCompatibility(ctx, "us-south-1", "subnet-12345")
	assert.NoError(t, err)

	// Second call should use cache
	err = controller.validateZoneSubnetCompatibility(ctx, "us-south-1", "subnet-12345")
	assert.NoError(t, err)

	// Third call with different zone should use cache and fail
	err = controller.validateZoneSubnetCompatibility(ctx, "us-south-2", "subnet-12345")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "subnet subnet-12345 is in zone us-south-1, but requested zone is us-south-2")

	// Verify API was called only once
	mockProvider.AssertExpectations(t)
}

func TestValidateBusinessLogic_ZoneSubnetCompatibility(t *testing.T) {
	tests := []struct {
		name          string
		zone          string
		subnetID      string
		setupMock     func(*MockSubnetProvider)
		expectedError string
	}{
		{
			name:          "no zone or subnet specified",
			zone:          "",
			subnetID:      "",
			setupMock:     func(m *MockSubnetProvider) {},
			expectedError: "",
		},
		{
			name:          "only zone specified",
			zone:          "us-south-1",
			subnetID:      "",
			setupMock:     func(m *MockSubnetProvider) {},
			expectedError: "",
		},
		{
			name:          "only subnet specified",
			zone:          "",
			subnetID:      "subnet-12345",
			setupMock:     func(m *MockSubnetProvider) {},
			expectedError: "",
		},
		{
			name:     "both zone and subnet specified - compatible",
			zone:     "us-south-1",
			subnetID: "subnet-12345",
			setupMock: func(m *MockSubnetProvider) {
				m.On("GetSubnet", mock.Anything, "subnet-12345").Return(&subnet.SubnetInfo{
					ID:           "subnet-12345",
					Zone:         "us-south-1",
					State:        "available",
					AvailableIPs: 100,
				}, nil)
			},
			expectedError: "",
		},
		{
			name:     "both zone and subnet specified - incompatible",
			zone:     "us-south-1",
			subnetID: "subnet-12345",
			setupMock: func(m *MockSubnetProvider) {
				m.On("GetSubnet", mock.Anything, "subnet-12345").Return(&subnet.SubnetInfo{
					ID:           "subnet-12345",
					Zone:         "us-south-2",
					State:        "available",
					AvailableIPs: 100,
				}, nil)
			},
			expectedError: "zone-subnet compatibility validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockProvider := new(MockSubnetProvider)
			if tt.setupMock != nil {
				tt.setupMock(mockProvider)
			}

			controller := &Controller{
				subnetProvider: mockProvider,
				cache:          cache.New(15 * time.Minute),
			}

			nodeClass := getValidNodeClass()
			nodeClass.Spec.Zone = tt.zone
			nodeClass.Spec.Subnet = tt.subnetID

			ctx := context.Background()
			err := controller.validateBusinessLogic(ctx, nodeClass)

			if tt.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			}

			mockProvider.AssertExpectations(t)
		})
	}
}