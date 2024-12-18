package ibm

import (
	"context"
	"errors"
	"testing"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"
)

type mockVPCClient struct {
	createInstanceResponse *vpcv1.Instance
	getInstanceResponse    *vpcv1.Instance
	listInstancesResponse  *vpcv1.InstanceCollection
	err                    error
}

func (m *mockVPCClient) CreateInstanceWithContext(_ context.Context, _ *vpcv1.CreateInstanceOptions) (*vpcv1.Instance, *core.DetailedResponse, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.createInstanceResponse, &core.DetailedResponse{}, nil
}

func (m *mockVPCClient) DeleteInstanceWithContext(_ context.Context, _ *vpcv1.DeleteInstanceOptions) (*core.DetailedResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &core.DetailedResponse{}, nil
}

func (m *mockVPCClient) GetInstanceWithContext(_ context.Context, _ *vpcv1.GetInstanceOptions) (*vpcv1.Instance, *core.DetailedResponse, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.getInstanceResponse, &core.DetailedResponse{}, nil
}

func (m *mockVPCClient) ListInstancesWithContext(_ context.Context, _ *vpcv1.ListInstancesOptions) (*vpcv1.InstanceCollection, *core.DetailedResponse, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.listInstancesResponse, &core.DetailedResponse{}, nil
}

func (m *mockVPCClient) UpdateInstanceWithContext(_ context.Context, _ *vpcv1.UpdateInstanceOptions) (*vpcv1.Instance, *core.DetailedResponse, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	return &vpcv1.Instance{}, &core.DetailedResponse{}, nil
}

func TestNewVPCClient(t *testing.T) {
	baseURL := "https://test.vpc.url"
	authType := "iam"
	apiKey := "test-key"
	region := "us-south"

	client := NewVPCClient(baseURL, authType, apiKey, region)

	if client == nil {
		t.Fatal("expected non-nil client")
		return
	}

	if client.baseURL != baseURL {
		t.Errorf("expected base URL %s, got %s", baseURL, client.baseURL)
	}
	if client.authType != authType {
		t.Errorf("expected auth type %s, got %s", authType, client.authType)
	}
	if client.apiKey != apiKey {
		t.Errorf("expected API key %s, got %s", apiKey, client.apiKey)
	}
	if client.region != region {
		t.Errorf("expected region %s, got %s", region, client.region)
	}
}

func TestCreateInstance(t *testing.T) {
	ctx := context.Background()
	instanceID := "test-instance"
	instanceName := "test-name"

	mockInstance := &vpcv1.Instance{
		ID:   &instanceID,
		Name: &instanceName,
	}

	tests := []struct {
		name         string
		mockVPC      vpcClientInterface
		prototype    *vpcv1.InstancePrototype
		wantErr      bool
		wantInstance *vpcv1.Instance
		uninitClient bool
	}{
		{
			name: "successful instance creation",
			mockVPC: &mockVPCClient{
				createInstanceResponse: mockInstance,
			},
			prototype:    &vpcv1.InstancePrototype{},
			wantInstance: mockInstance,
		},
		{
			name: "API error",
			mockVPC: &mockVPCClient{
				err: errors.New("API error"),
			},
			prototype: &vpcv1.InstancePrototype{},
			wantErr:   true,
		},
		{
			name:         "uninitialized client",
			uninitClient: true,
			prototype:    &vpcv1.InstancePrototype{},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &VPCClient{}
			if !tt.uninitClient {
				client.client = tt.mockVPC
			}

			instance, err := client.CreateInstance(ctx, tt.prototype)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				if instance != nil {
					t.Error("expected nil instance")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if instance != tt.wantInstance {
					t.Errorf("got instance %v, want %v", instance, tt.wantInstance)
				}
			}
		})
	}
}

func TestDeleteInstance(t *testing.T) {
	ctx := context.Background()
	instanceID := "test-instance"

	tests := []struct {
		name         string
		mockVPC      vpcClientInterface
		instanceID   string
		wantErr      bool
		uninitClient bool
	}{
		{
			name:       "successful instance deletion",
			mockVPC:    &mockVPCClient{},
			instanceID: instanceID,
		},
		{
			name: "API error",
			mockVPC: &mockVPCClient{
				err: errors.New("API error"),
			},
			instanceID: instanceID,
			wantErr:    true,
		},
		{
			name:         "uninitialized client",
			uninitClient: true,
			instanceID:   instanceID,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &VPCClient{}
			if !tt.uninitClient {
				client.client = tt.mockVPC
			}

			err := client.DeleteInstance(ctx, tt.instanceID)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestGetInstance(t *testing.T) {
	ctx := context.Background()
	instanceID := "test-instance"
	instanceName := "test-name"

	mockInstance := &vpcv1.Instance{
		ID:   &instanceID,
		Name: &instanceName,
	}

	tests := []struct {
		name         string
		mockVPC      vpcClientInterface
		instanceID   string
		wantErr      bool
		wantInstance *vpcv1.Instance
		uninitClient bool
	}{
		{
			name: "successful instance retrieval",
			mockVPC: &mockVPCClient{
				getInstanceResponse: mockInstance,
			},
			instanceID:   instanceID,
			wantInstance: mockInstance,
		},
		{
			name: "API error",
			mockVPC: &mockVPCClient{
				err: errors.New("API error"),
			},
			instanceID: instanceID,
			wantErr:    true,
		},
		{
			name:         "uninitialized client",
			uninitClient: true,
			instanceID:   instanceID,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &VPCClient{}
			if !tt.uninitClient {
				client.client = tt.mockVPC
			}

			instance, err := client.GetInstance(ctx, tt.instanceID)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				if instance != nil {
					t.Error("expected nil instance")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if instance != tt.wantInstance {
					t.Errorf("got instance %v, want %v", instance, tt.wantInstance)
				}
			}
		})
	}
}

func TestListInstances(t *testing.T) {
	ctx := context.Background()
	instanceID := "test-instance"
	instanceName := "test-name"

	mockInstances := []vpcv1.Instance{
		{
			ID:   &instanceID,
			Name: &instanceName,
		},
	}

	tests := []struct {
		name          string
		mockVPC       vpcClientInterface
		wantErr       bool
		wantInstances []vpcv1.Instance
		uninitClient  bool
	}{
		{
			name: "successful instances listing",
			mockVPC: &mockVPCClient{
				listInstancesResponse: &vpcv1.InstanceCollection{
					Instances: mockInstances,
				},
			},
			wantInstances: mockInstances,
		},
		{
			name: "API error",
			mockVPC: &mockVPCClient{
				err: errors.New("API error"),
			},
			wantErr: true,
		},
		{
			name:         "uninitialized client",
			uninitClient: true,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &VPCClient{}
			if !tt.uninitClient {
				client.client = tt.mockVPC
			}

			instances, err := client.ListInstances(ctx)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				if instances != nil {
					t.Error("expected nil instances")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(instances) != len(tt.wantInstances) {
					t.Errorf("got %d instances, want %d", len(instances), len(tt.wantInstances))
				}
			}
		})
	}
}

func TestUpdateInstanceTags(t *testing.T) {
	ctx := context.Background()
	instanceID := "test-instance"
	tags := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	tests := []struct {
		name         string
		mockVPC      vpcClientInterface
		instanceID   string
		tags         map[string]string
		wantErr      bool
		uninitClient bool
	}{
		{
			name:       "successful tags update",
			mockVPC:    &mockVPCClient{},
			instanceID: instanceID,
			tags:       tags,
		},
		{
			name: "API error",
			mockVPC: &mockVPCClient{
				err: errors.New("API error"),
			},
			instanceID: instanceID,
			tags:       tags,
			wantErr:    true,
		},
		{
			name:         "uninitialized client",
			uninitClient: true,
			instanceID:   instanceID,
			tags:         tags,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &VPCClient{}
			if !tt.uninitClient {
				client.client = tt.mockVPC
			}

			err := client.UpdateInstanceTags(ctx, tt.instanceID, tt.tags)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
