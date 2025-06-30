package ibm

import (
	"os"
	"testing"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name      string
		envVars   map[string]string
		wantError bool
	}{
		{
			name: "valid configuration",
			envVars: map[string]string{
				"VPC_API_KEY": "test-vpc-key",
				"IBM_API_KEY": "test-ibm-key",
				"IBM_REGION":  "us-south",
			},
			wantError: false,
		},
		{
			name: "valid configuration with custom VPC URL",
			envVars: map[string]string{
				"VPC_URL":     "https://custom.vpc.url/v1",
				"VPC_API_KEY": "test-vpc-key",
				"IBM_API_KEY": "test-ibm-key",
				"IBM_REGION":  "us-south",
			},
			wantError: false,
		},
		{
			name: "missing VPC API key",
			envVars: map[string]string{
				"IBM_API_KEY": "test-ibm-key",
				"IBM_REGION":  "us-south",
			},
			wantError: true,
		},
		{
			name: "missing IBM API key",
			envVars: map[string]string{
				"VPC_API_KEY": "test-vpc-key",
				"IBM_REGION":  "us-south",
			},
			wantError: true,
		},
		{
			name: "missing region",
			envVars: map[string]string{
				"VPC_API_KEY": "test-vpc-key",
				"IBM_API_KEY": "test-ibm-key",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment before each test
			os.Clearenv()

			// Set environment variables for this test
			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
			}

			// Run the test
			client, err := NewClient()

			// Check error expectation
			if tt.wantError && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Additional checks for successful cases
			if err == nil {
				if client.vpcURL == "" {
					t.Error("vpcURL should not be empty")
				}
				if client.vpcAuthType == "" {
					t.Error("vpcAuthType should not be empty")
				}
				if client.iamClient == nil {
					t.Error("iamClient should not be nil")
				}
				if client.GetRegion() != tt.envVars["IBM_REGION"] {
					t.Errorf("expected region %s, got %s", tt.envVars["IBM_REGION"], client.GetRegion())
				}
			}
		})
	}
}

func TestGetVPCClient(t *testing.T) {
	client := &Client{
		vpcURL:      "https://test.vpc.url/v1",
		vpcAuthType: "iam",
		vpcAPIKey:   "test-key",
		region:      "us-south",
	}

	vpcClient, err := client.GetVPCClient()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if vpcClient == nil {
		t.Error("expected VPC client but got nil")
	}
}

func TestGetGlobalCatalogClient(t *testing.T) {
	client := &Client{
		ibmAPIKey: "test-key",
		iamClient: NewIAMClient("test-key"),
	}

	catalogClient, err := client.GetGlobalCatalogClient()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if catalogClient == nil {
		t.Error("expected Global Catalog client but got nil")
	}
}
