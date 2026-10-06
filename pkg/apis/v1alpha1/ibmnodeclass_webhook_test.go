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

package v1alpha1

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func validWebhookNodeClass() *IBMNodeClass {
	return &IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
		Spec: IBMNodeClassSpec{
			InstanceProfile:   "bx2-2x8",
			Region:            "us-south",
			Zone:              "us-south-1",
			VPC:               "r010-2b1c3cdc-a678-4eda-86af-731130de1c0a",
			Image:             "r010-dd3c20fa-71d3-4dc0-913f-2f097bf3e500",
			Subnet:            "02c7-ac2802cf-54bb-4508-aad7-eba7e8c2034c",
			SecurityGroups:    []string{"r010-36f045e2-86a1-4af8-917e-b17a41f8abe3"},
			SSHKeys:           []string{"r010-28168374-32db-4fd4-b1e7-12bd4c30e1db"},
			ResourceGroup:     "0123456789abcdef0123456789abcdef",
			APIServerEndpoint: "https://test.example.com:6443",
			BootstrapMode:     stringPtr("cloud-init"),
		},
	}
}

func TestIBMNodeClass_ValidateCreate(t *testing.T) {
	tests := []struct {
		name         string
		configure    func(*IBMNodeClass)
		errContains  []string
		wantWarnings []string
	}{
		{
			name: "customer configuration - missing api server endpoint",
			configure: func(nc *IBMNodeClass) {
				nc.Spec.Region, nc.Spec.Zone = "br-sao", "br-sao-2"
				nc.Spec.Image = "ibm-ubuntu-22-04-5-minimal-amd64-6"
				nc.Spec.SecurityGroups = []string{"sg-k8s-workers"}
				nc.Spec.APIServerEndpoint = ""
				nc.Spec.BootstrapMode = nil
			},
			errContains: []string{
				"apiServerEndpoint is required",
				"security group 'sg-k8s-workers' is not a valid IBM Cloud resource ID",
			},
			wantWarnings: []string{
				"bootstrapMode not specified",
				"image 'ibm-ubuntu-22-04-5-minimal-amd64-6' appears to be a name",
			},
		},
		{name: "valid configuration with IDs"},
		{
			name: "invalid security group format - too short",
			configure: func(nc *IBMNodeClass) {
				nc.Spec.SecurityGroups = []string{"r010-short"}
				nc.Spec.Image = "ubuntu-20-04"
			},
			errContains:  []string{"security group 'r010-short' is not a valid IBM Cloud resource ID"},
			wantWarnings: []string{"image 'ubuntu-20-04' appears to be a name"},
		},
		{
			name: "invalid api server endpoint format",
			configure: func(nc *IBMNodeClass) {
				nc.Spec.APIServerEndpoint = "10.0.0.1:6443"
			},
			errContains: []string{"apiServerEndpoint '10.0.0.1:6443' is not a valid URL"},
		},
		{
			name: "invalid subnet format",
			configure: func(nc *IBMNodeClass) {
				nc.Spec.Subnet = "my-subnet"
			},
			errContains: []string{"subnet 'my-subnet' is not a valid IBM Cloud subnet ID"},
		},
		{
			name: "invalid bootstrap mode",
			configure: func(nc *IBMNodeClass) {
				nc.Spec.BootstrapMode = stringPtr("invalid-mode")
			},
			errContains: []string{"invalid bootstrapMode 'invalid-mode'"},
		},
		{
			name: "missing VPC",
			configure: func(nc *IBMNodeClass) {
				nc.Spec.VPC = ""
			},
			errContains: []string{"vpc is required"},
		},
		{
			name: "no security groups - should warn",
			configure: func(nc *IBMNodeClass) {
				nc.Spec.SecurityGroups = nil
			},
			wantWarnings: []string{"no security groups specified"},
		},
		{
			name: "invalid SSH key format",
			configure: func(nc *IBMNodeClass) {
				nc.Spec.SSHKeys = []string{"my-ssh-key"}
			},
			errContains: []string{"SSH key 'my-ssh-key' is not a valid IBM Cloud resource ID"},
		},
		{
			name: "valid configuration with different region number lengths",
			configure: func(nc *IBMNodeClass) {
				nc.Spec.Region, nc.Spec.Zone = "br-sao", "br-sao-2"
				nc.Spec.VPC = "r042-4225852b-4846-4a4a-88c4-9966471337c6"
				nc.Spec.Image = "r006-dd3c20fa-71d3-4dc0-913f-2f097bf3e500"
				nc.Spec.SecurityGroups = []string{"r50-36f045e2-86a1-4af8-917e-b17a41f8abe3"}
			},
		},
		{
			name: "http endpoint should be allowed",
			configure: func(nc *IBMNodeClass) {
				nc.Spec.APIServerEndpoint = "http://10.0.0.1:6443"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nc := validWebhookNodeClass()
			if tt.configure != nil {
				tt.configure(nc)
			}
			warnings, err := nc.ValidateCreate(context.Background(), nc)
			if len(tt.errContains) != 0 {
				require.Error(t, err)
				for _, expected := range tt.errContains {
					assert.Contains(t, err.Error(), expected)
				}
			} else {
				assert.NoError(t, err)
			}
			assert.Len(t, warnings, len(tt.wantWarnings))
			for _, expected := range tt.wantWarnings {
				assert.Contains(t, strings.Join(warnings, "\n"), expected)
			}
		})
	}
}

func TestIBMNodeClass_ValidateUpdate(t *testing.T) {
	ctx := context.Background()
	nc := validWebhookNodeClass()
	old := nc.DeepCopy()
	nc.Spec.APIServerEndpoint = ""
	nc.Spec.SecurityGroups = []string{"sg-k8s-workers"}
	nc.Spec.Image = "ibm-ubuntu-22-04-5-minimal-amd64-5"
	warnings, err := nc.ValidateUpdate(ctx, old, nc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiServerEndpoint is required")
	assert.Contains(t, err.Error(), "security group 'sg-k8s-workers' is not a valid IBM Cloud resource ID")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "image 'ibm-ubuntu-22-04-5-minimal-amd64-5' appears to be a name")
}

func TestIBMNodeClass_ValidateDelete(t *testing.T) {
	nc := &IBMNodeClass{}
	warnings, err := nc.ValidateDelete(context.Background(), nc)
	assert.NoError(t, err)
	assert.Empty(t, warnings)
}

func stringPtr(s string) *string {
	return &s
}
