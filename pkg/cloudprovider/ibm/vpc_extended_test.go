package ibm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/IBM/vpc-go-sdk/vpcv1"
)

func TestListSubnets(t *testing.T) {
	ctx := context.Background()
	vpcID := "test-vpc"
	subnetID := "test-subnet"
	subnetName := "test-subnet-name"
	zoneName := "us-south-1"

	mockSubnets := []vpcv1.Subnet{
		{
			ID:   &subnetID,
			Name: &subnetName,
			Zone: &vpcv1.ZoneReference{
				Name: &zoneName,
			},
		},
	}

	tests := []struct {
		name         string
		mockVPC      vpcClientInterface
		vpcID        string
		wantErr      bool
		wantSubnets  int
		uninitClient bool
	}{
		{
			name: "successful subnet listing",
			mockVPC: &mockVPCClient{
				listSubnetsResponse: &vpcv1.SubnetCollection{
					Subnets: mockSubnets,
				},
			},
			vpcID:       vpcID,
			wantSubnets: 1,
		},
		{
			name: "API error",
			mockVPC: &mockVPCClient{
				err: errors.New("API error"),
			},
			vpcID:   vpcID,
			wantErr: true,
		},
		{
			name:         "uninitialized client",
			uninitClient: true,
			vpcID:        vpcID,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &VPCClient{}
			if !tt.uninitClient {
				client.client = tt.mockVPC
			}

			subnets, err := client.ListSubnets(ctx, tt.vpcID)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				if subnets != nil {
					t.Error("expected nil subnets")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(subnets.Subnets) != tt.wantSubnets {
					t.Errorf("got %d subnets, want %d", len(subnets.Subnets), tt.wantSubnets)
				}
			}
		})
	}
}

func TestGetSubnet(t *testing.T) {
	ctx := context.Background()
	subnetID := "test-subnet"
	subnetName := "test-subnet-name"
	zoneName := "us-south-1"

	mockSubnet := &vpcv1.Subnet{
		ID:   &subnetID,
		Name: &subnetName,
		Zone: &vpcv1.ZoneReference{
			Name: &zoneName,
		},
	}

	tests := []struct {
		name         string
		mockVPC      vpcClientInterface
		subnetID     string
		wantErr      bool
		wantSubnet   *vpcv1.Subnet
		uninitClient bool
	}{
		{
			name: "successful subnet retrieval",
			mockVPC: &mockVPCClient{
				getSubnetResponse: mockSubnet,
			},
			subnetID:   subnetID,
			wantSubnet: mockSubnet,
		},
		{
			name: "API error",
			mockVPC: &mockVPCClient{
				err: errors.New("API error"),
			},
			subnetID: subnetID,
			wantErr:  true,
		},
		{
			name:         "uninitialized client",
			uninitClient: true,
			subnetID:     subnetID,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &VPCClient{}
			if !tt.uninitClient {
				client.client = tt.mockVPC
			}

			subnet, err := client.GetSubnet(ctx, tt.subnetID)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				if subnet != nil {
					t.Error("expected nil subnet")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if subnet != tt.wantSubnet {
					t.Errorf("got subnet %v, want %v", subnet, tt.wantSubnet)
				}
			}
		})
	}
}

// Integration-style test for VPC client with retries and error scenarios
func TestVPCClientErrorHandling(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		setupClient func() *VPCClient
		operation   func(*VPCClient) error
		wantErr     bool
		errContains string
	}{
		{
			name: "uninitialized client - create instance",
			setupClient: func() *VPCClient {
				return &VPCClient{}
			},
			operation: func(c *VPCClient) error {
				_, err := c.CreateInstance(ctx, &vpcv1.InstancePrototype{})
				return err
			},
			wantErr:     true,
			errContains: "VPC client not initialized",
		},
		{
			name: "uninitialized client - list subnets",
			setupClient: func() *VPCClient {
				return &VPCClient{}
			},
			operation: func(c *VPCClient) error {
				_, err := c.ListSubnets(ctx, "test-vpc")
				return err
			},
			wantErr:     true,
			errContains: "VPC client not initialized",
		},
		{
			name: "timeout scenario",
			setupClient: func() *VPCClient {
				return &VPCClient{
					client: &mockVPCClient{
						err: errors.New("context deadline exceeded"),
					},
				}
			},
			operation: func(c *VPCClient) error {
				_, err := c.ListInstances(ctx)
				return err
			},
			wantErr:     true,
			errContains: "context deadline exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setupClient()
			err := tt.operation(client)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}