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

package bootstrap

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

func TestVPCBootstrapProvider_APIServerEndpoint_Override(t *testing.T) {
	ctx := context.Background()
	
	// Create a fake Kubernetes client with bootstrap token
	k8sClient := fake.NewSimpleClientset()
	
	// Create a bootstrap token secret
	bootstrapSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bootstrap-token-abcdef",
			Namespace: "kube-system",
		},
		Type: "bootstrap.kubernetes.io/token",
		Data: map[string][]byte{
			"token-id":     []byte("abcdef"),
			"token-secret": []byte("0123456789abcdef"),
			"usage-bootstrap-authentication": []byte("true"),
			"usage-bootstrap-signing":         []byte("true"),
		},
	}
	_, err := k8sClient.CoreV1().Secrets("kube-system").Create(ctx, bootstrapSecret, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create a CA certificate secret for testing
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-token",
			Namespace: "kube-system",
		},
		Type: corev1.SecretTypeServiceAccountToken,
		Data: map[string][]byte{
			"ca.crt": []byte("-----BEGIN CERTIFICATE-----\nMIIBKjCB4wIBATANBgkqhkiG9w0BAQsFADA...\n-----END CERTIFICATE-----"),
			"token":  []byte("test-token"),
		},
	}
	_, err = k8sClient.CoreV1().Secrets("kube-system").Create(ctx, caSecret, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create provider
	provider := NewVPCBootstrapProvider(nil, k8sClient, nil)

	tests := []struct {
		name                    string
		apiServerEndpoint       string
		expectedInUserData     string
		expectError             bool
		description             string
	}{
		{
			name:                "custom internal API endpoint",
			apiServerEndpoint:   "https://10.243.65.4:6443",
			expectedInUserData:  "10.243.65.4:6443", // https:// should be stripped for kubeadm
			expectError:         false,
			description:         "Should use internal control plane IP when specified",
		},
		{
			name:                "custom external API endpoint",
			apiServerEndpoint:   "https://158.176.3.211:6443",
			expectedInUserData:  "158.176.3.211:6443", // https:// should be stripped for kubeadm
			expectError:         false,
			description:         "Should use external endpoint when specified",
		},
		{
			name:                "empty endpoint - auto discovery",
			apiServerEndpoint:   "",
			expectedInUserData:  "", // Will fail because no kubeadm-config exists
			expectError:         true,
			description:         "Should attempt auto-discovery when no endpoint specified",
		},
		{
			name:                "malformed endpoint",
			apiServerEndpoint:   "invalid-endpoint",
			expectedInUserData:  "",
			expectError:         false, // Provider doesn't validate format, only CRD does
			description:         "Provider accepts any endpoint, validation is at CRD level",
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
					APIServerEndpoint: tt.apiServerEndpoint,
				},
			}

			nodeClaim := types.NamespacedName{
				Name:      "test-nodeclaim",
				Namespace: "default",
			}

			userData, err := provider.GetUserData(ctx, nodeClass, nodeClaim)

			if tt.expectError {
				assert.Error(t, err, tt.description)
				return
			}

			require.NoError(t, err, tt.description)
			assert.NotEmpty(t, userData, "User data should not be empty")

			if tt.expectedInUserData != "" {
				// Check that the expected endpoint appears in the user data
				assert.Contains(t, userData, tt.expectedInUserData, 
					"Expected endpoint %s to be in user data", tt.expectedInUserData)
				
				// Verify direct kubelet uses the cluster endpoint variable in bootstrap kubeconfig
				assert.Contains(t, userData, "server: ${CLUSTER_ENDPOINT}", 
					"bootstrap kubeconfig should use CLUSTER_ENDPOINT variable")
				
				// Verify the script uses direct kubelet approach (not kubeadm)
				assert.Contains(t, userData, "bootstrap-kubeconfig", 
					"script should create bootstrap kubeconfig for direct kubelet")
				
				// Verify kubeadm is NOT used
				assert.NotContains(t, userData, "kubeadm join", 
					"script should not use kubeadm join")
			}
		})
	}
}

func TestVPCBootstrapProvider_APIServerEndpoint_Issue18_Fix(t *testing.T) {
	// This test specifically addresses Issue #18 from OPERATOR_ISSUES.md
	// "Bootstrap Provider Uses Wrong API Server Endpoint"
	
	ctx := context.Background()
	k8sClient := fake.NewSimpleClientset()
	
	// Create bootstrap token
	bootstrapSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bootstrap-token-abcdef",
			Namespace: "kube-system",
		},
		Type: "bootstrap.kubernetes.io/token",
		Data: map[string][]byte{
			"token-id":     []byte("abcdef"),
			"token-secret": []byte("0123456789abcdef"),
			"usage-bootstrap-authentication": []byte("true"),
			"usage-bootstrap-signing":         []byte("true"),
		},
	}
	_, err := k8sClient.CoreV1().Secrets("kube-system").Create(ctx, bootstrapSecret, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create a CA certificate secret for testing
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-token",
			Namespace: "kube-system",
		},
		Type: corev1.SecretTypeServiceAccountToken,
		Data: map[string][]byte{
			"ca.crt": []byte("-----BEGIN CERTIFICATE-----\nMIIBKjCB4wIBATANBgkqhkiG9w0BAQsFADA...\n-----END CERTIFICATE-----"),
			"token":  []byte("test-token"),
		},
	}
	_, err = k8sClient.CoreV1().Secrets("kube-system").Create(ctx, caSecret, metav1.CreateOptions{})
	require.NoError(t, err)

	provider := NewVPCBootstrapProvider(nil, k8sClient, nil)

	// Test case from Issue #18: Internal control plane should be used instead of external
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:            "eu-de",
			Zone:              "eu-de-2",
			VPC:               "r010-2b1c3cdc-a678-4eda-86af-731130de1c0a",
			Image:             "ibm-ubuntu-24-04-2-minimal-amd64-4",
			Subnet:            "02c7-ac2802cf-54bb-4508-aad7-eba7e8c2034c",
			SecurityGroups:    []string{"r010-b79186e9-78d9-4062-a4b2-291529460eff"},
			// Fix: Use internal control plane IP instead of external
			APIServerEndpoint: "https://10.243.65.4:6443",
		},
	}

	nodeClaim := types.NamespacedName{
		Name:      "test-nodeclaim",
		Namespace: "default",
	}

	userData, err := provider.GetUserData(ctx, nodeClass, nodeClaim)
	require.NoError(t, err)

	// Verify the fix addresses the specific issues mentioned in Issue #18
	
	// 1. Should use internal control plane IP (10.243.65.4:6443)
	assert.Contains(t, userData, "10.243.65.4:6443", 
		"Should use internal control plane IP")
	
	// 2. Should NOT use external API server (158.176.3.211:6443)
	assert.NotContains(t, userData, "158.176.3.211:6443", 
		"Should not use external API server endpoint")
	
	// 3. Should NOT use internal Kubernetes service IP (10.96.0.1:443)
	assert.NotContains(t, userData, "10.96.0.1:443", 
		"Should not use internal Kubernetes service IP")
	
	// 4. bootstrap kubeconfig should use the correct server endpoint with https://
	assert.Contains(t, userData, "server: ${CLUSTER_ENDPOINT}", 
		"bootstrap kubeconfig should use CLUSTER_ENDPOINT variable")
	
	// 5. Should use direct kubelet approach (not kubeadm)
	assert.Contains(t, userData, "bootstrap-kubeconfig", 
		"script should create bootstrap kubeconfig for direct kubelet")
	
	// 6. Should not use kubeadm join
	assert.NotContains(t, userData, "kubeadm join", 
		"script should not use kubeadm join")
}

func TestVPCBootstrapProvider_APIServerEndpoint_ValidationPattern(t *testing.T) {
	// Test the validation pattern from the CRD
	pattern := `^https://[a-zA-Z0-9.-]+:[0-9]+$`
	
	tests := []struct {
		endpoint string
		valid    bool
	}{
		{"https://10.243.65.4:6443", true},
		{"https://158.176.3.211:6443", true},
		{"https://api.example.com:6443", true},
		{"https://master-node-1.cluster.local:6443", true},
		{"http://10.243.65.4:6443", false},   // http not https
		{"10.243.65.4:6443", false},          // missing protocol
		{"https://10.243.65.4", false},       // missing port
		{"", true},                           // empty is valid (optional)
	}
	
	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			if tt.endpoint == "" {
				// Empty string is valid (optional field)
				return
			}
			
			// Test pattern matching
			matched := matchesPattern(tt.endpoint, pattern)
			assert.Equal(t, tt.valid, matched, 
				"Endpoint %s should match pattern: %t", tt.endpoint, tt.valid)
		})
	}
}

// Helper function to test regex pattern matching
func matchesPattern(s, pattern string) bool {
	if len(s) == 0 {
		return true // Empty is valid
	}
	
	// Use actual regex matching
	matched, err := regexp.MatchString(pattern, s)
	if err != nil {
		return false
	}
	return matched
}

