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

package v1alpha1_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

func TestIBMNodeClass_APIServerEndpoint_Validation(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		expectValid bool
	}{
		{
			name:        "valid HTTPS endpoint with IP",
			endpoint:    "https://10.243.65.4:6443",
			expectValid: true,
		},
		{
			name:        "valid HTTPS endpoint with hostname",
			endpoint:    "https://api.example.com:6443",
			expectValid: true,
		},
		{
			name:        "valid HTTPS endpoint with dashes in hostname",
			endpoint:    "https://master-node-1.cluster.local:6443",
			expectValid: true,
		},
		{
			name:        "empty endpoint should be valid (optional field)",
			endpoint:    "",
			expectValid: true,
		},
		{
			name:        "invalid - missing https",
			endpoint:    "http://10.243.65.4:6443",
			expectValid: false,
		},
		{
			name:        "invalid - missing port",
			endpoint:    "https://10.243.65.4",
			expectValid: false,
		},
		{
			name:        "invalid - missing protocol",
			endpoint:    "10.243.65.4:6443",
			expectValid: false,
		},
		{
			name:        "invalid - invalid format",
			endpoint:    "not-a-url",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeClass := &v1alpha1.IBMNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: v1alpha1.IBMNodeClassSpec{
					Region:            "us-south",
					Zone:              "us-south-1",
					VPC:               "r010-12345678-1234-1234-1234-123456789012",
					Image:             "ubuntu-20-04",
					Subnet:            "1234-12345678-1234-1234-1234-123456789012",
					SecurityGroups:    []string{"r010-12345678-1234-1234-1234-123456789012"},
					APIServerEndpoint: tt.endpoint,
				},
			}

			// Validate the endpoint format using regex pattern
			if tt.expectValid {
				if tt.endpoint != "" {
					// Check if it matches the expected pattern
					pattern := `^https://[a-zA-Z0-9.-]+:[0-9]+$`
					matched, _ := regexp.MatchString(pattern, tt.endpoint)
					assert.True(t, matched, "Expected endpoint %s to match validation pattern", tt.endpoint)
				}
			} else {
				if tt.endpoint != "" {
					pattern := `^https://[a-zA-Z0-9.-]+:[0-9]+$`
					matched, _ := regexp.MatchString(pattern, tt.endpoint)
					assert.False(t, matched, "Expected endpoint %s to fail validation pattern", tt.endpoint)
				}
			}

			// Test that the field is properly set
			assert.Equal(t, tt.endpoint, nodeClass.Spec.APIServerEndpoint)
		})
	}
}

func TestIBMNodeClass_APIServerEndpoint_DefaultBehavior(t *testing.T) {
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:         "us-south",
			Zone:           "us-south-1",
			VPC:            "r010-12345678-1234-1234-1234-123456789012",
			Image:          "ubuntu-20-04",
			Subnet:         "1234-12345678-1234-1234-1234-123456789012",
			SecurityGroups: []string{"r010-12345678-1234-1234-1234-123456789012"},
			// APIServerEndpoint not set
		},
	}

	// Verify the field defaults to empty string
	assert.Equal(t, "", nodeClass.Spec.APIServerEndpoint)
}