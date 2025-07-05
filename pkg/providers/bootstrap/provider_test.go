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
	"encoding/base64"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
)

func TestBootstrapProvider_GetUserData_CloudInit(t *testing.T) {
	ctx := context.Background()
	
	// Create fake Kubernetes client with cluster resources
	fakeClient := createFakeKubernetesClient()
	
	// Create mock IBM client
	mockClient := &ibm.Client{} // This would be a proper mock in production
	
	// Create bootstrap provider
	provider := NewProvider(mockClient, fakeClient)
	
	// Create test NodeClass with cloud-init mode
	cloudInitMode := "cloud-init"
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:        "us-south",
			Zone:          "us-south-1",
			VPC:           "r006-test-vpc",
			Image:         "ubuntu-20-04",
			BootstrapMode: &cloudInitMode,
			UserData:      "echo 'custom user data'",
		},
	}
	
	// Create test NodeClaim
	nodeClaim := types.NamespacedName{
		Name:      "test-node",
		Namespace: "default",
	}
	
	// Generate user data
	userData, err := provider.GetUserData(ctx, nodeClass, nodeClaim)
	require.NoError(t, err)
	require.NotEmpty(t, userData)
	
	// Decode base64 user data
	decodedBytes, err := base64.StdEncoding.DecodeString(userData)
	require.NoError(t, err)
	
	decodedUserData := string(decodedBytes)
	
	// Verify bootstrap script contains expected elements
	assert.Contains(t, decodedUserData, "#!/bin/bash")
	assert.Contains(t, decodedUserData, "IBM Cloud Kubernetes Bootstrap Script")
	assert.Contains(t, decodedUserData, "install_container_runtime")
	assert.Contains(t, decodedUserData, "install_kubernetes")
	assert.Contains(t, decodedUserData, "configure_kubelet")
	assert.Contains(t, decodedUserData, "join_cluster")
	assert.Contains(t, decodedUserData, "echo 'custom user data'") // Custom user data included
	
	t.Logf("Generated cloud-init script length: %d bytes", len(decodedUserData))
}

func TestBootstrapProvider_GetUserData_IKSMode(t *testing.T) {
	ctx := context.Background()
	
	// Set environment variable for IKS cluster
	originalClusterID := os.Getenv("IKS_CLUSTER_ID")
	defer func() {
		_ = os.Setenv("IKS_CLUSTER_ID", originalClusterID)
	}()
	_ = os.Setenv("IKS_CLUSTER_ID", "test-cluster-id")
	
	// Create fake Kubernetes client
	fakeClient := createFakeKubernetesClient()
	
	// Create mock IBM client
	mockClient := &ibm.Client{}
	
	// Create bootstrap provider
	provider := NewProvider(mockClient, fakeClient)
	
	// Create test NodeClass with IKS API mode
	iksMode := "iks-api"
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass-iks",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:          "us-south",
			Zone:            "us-south-1",
			VPC:             "r006-test-vpc",
			Image:           "ubuntu-20-04",
			BootstrapMode:   &iksMode,
			IKSClusterID:    "test-cluster-id",
			UserData:        "echo 'IKS custom data'",
		},
	}
	
	// Create test NodeClaim
	nodeClaim := types.NamespacedName{
		Name:      "test-iks-node",
		Namespace: "default",
	}
	
	// Generate user data
	userData, err := provider.GetUserData(ctx, nodeClass, nodeClaim)
	require.NoError(t, err)
	
	// For IKS mode, user data should be empty since IKS handles node provisioning
	// through worker pool resize API, not bootstrap scripts
	assert.Empty(t, userData, "IKS mode should return empty user data")
	
	t.Logf("IKS mode correctly returned empty user data")
}

func TestBootstrapProvider_GetUserData_AutoMode(t *testing.T) {
	ctx := context.Background()
	
	// Create fake Kubernetes client
	fakeClient := createFakeKubernetesClient()
	
	// Create mock IBM client
	mockClient := &ibm.Client{}
	
	// Create bootstrap provider
	provider := NewProvider(mockClient, fakeClient)
	
	// Test auto mode without IKS cluster ID (should use cloud-init)
	autoMode := "auto"
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass-auto",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:        "us-south",
			Zone:          "us-south-1",
			VPC:           "r006-test-vpc",
			Image:         "ubuntu-20-04",
			BootstrapMode: &autoMode,
		},
	}
	
	// Create test NodeClaim
	nodeClaim := types.NamespacedName{
		Name:      "test-auto-node",
		Namespace: "default",
	}
	
	// Generate user data
	userData, err := provider.GetUserData(ctx, nodeClass, nodeClaim)
	require.NoError(t, err)
	require.NotEmpty(t, userData)
	
	// Decode base64 user data
	decodedBytes, err := base64.StdEncoding.DecodeString(userData)
	require.NoError(t, err)
	
	decodedUserData := string(decodedBytes)
	
	// Should default to cloud-init since no IKS cluster ID
	assert.Contains(t, decodedUserData, "IBM Cloud Kubernetes Bootstrap Script")
	assert.Contains(t, decodedUserData, "install_container_runtime")
	
	t.Logf("Auto mode defaulted to cloud-init")
}

func TestBootstrapProvider_DetectContainerRuntime(t *testing.T) {
	ctx := context.Background()
	
	// Create fake Kubernetes client with node that has containerd runtime
	fakeClient := createFakeKubernetesClientWithRuntime("containerd://1.6.12")
	
	// Create bootstrap provider
	provider := NewProvider(&ibm.Client{}, fakeClient)
	
	// Test runtime detection
	runtime := provider.detectContainerRuntime(ctx)
	assert.Equal(t, "containerd", runtime)
}

func TestBootstrapProvider_BuildKubeletConfig(t *testing.T) {
	// Create mock cluster config
	clusterConfig := &ClusterConfig{
		DNSClusterIP: "10.96.0.10",
		ClusterCIDR:  "10.244.0.0/16",
		CNIPlugin:    "calico",
		IPFamily:     "ipv4",
	}
	
	// Create bootstrap provider
	provider := NewProvider(&ibm.Client{}, nil)
	
	// Build kubelet config
	kubeletConfig := provider.buildKubeletConfig(clusterConfig)
	
	require.NotNil(t, kubeletConfig)
	assert.Equal(t, []string{"10.96.0.10"}, kubeletConfig.ClusterDNS)
	assert.Equal(t, "external", kubeletConfig.ExtraArgs["cloud-provider"])
	assert.Contains(t, kubeletConfig.ExtraArgs["provider-id"], "ibm://")
	assert.Equal(t, "cni", kubeletConfig.ExtraArgs["network-plugin"])
}

func TestBootstrapProvider_DetermineBootstrapMode(t *testing.T) {
	provider := NewProvider(&ibm.Client{}, nil)
	
	testCases := []struct {
		name         string
		nodeClass    *v1alpha1.IBMNodeClass
		clusterInfo  *ClusterInfo
		envVar       string
		expectedMode BootstrapMode
	}{
		{
			name: "explicit cloud-init mode",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					BootstrapMode: func() *string { s := "cloud-init"; return &s }(),
				},
			},
			clusterInfo:  &ClusterInfo{},
			expectedMode: BootstrapModeCloudInit,
		},
		{
			name: "explicit iks-api mode",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					BootstrapMode: func() *string { s := "iks-api"; return &s }(),
				},
			},
			clusterInfo:  &ClusterInfo{},
			expectedMode: BootstrapModeIKSAPI,
		},
		{
			name: "environment variable override",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{},
			},
			clusterInfo:  &ClusterInfo{},
			envVar:       "cloud-init",
			expectedMode: BootstrapModeCloudInit,
		},
		{
			name: "default to auto",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{},
			},
			clusterInfo:  &ClusterInfo{},
			expectedMode: BootstrapModeAuto,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set environment variable if specified
			if tc.envVar != "" {
				originalEnv := os.Getenv("BOOTSTRAP_MODE")
				defer func() {
					_ = os.Setenv("BOOTSTRAP_MODE", originalEnv)
				}()
				_ = os.Setenv("BOOTSTRAP_MODE", tc.envVar)
			}
			
			mode := provider.determineBootstrapMode(tc.nodeClass, tc.clusterInfo)
			assert.Equal(t, tc.expectedMode, mode)
		})
	}
}

// Helper function to create a fake Kubernetes client with test resources
func createFakeKubernetesClient() *fake.Clientset {
	// Create fake services
	kubernetesService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubernetes",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.1",
			Ports: []corev1.ServicePort{
				{Port: 443},
			},
		},
	}
	
	dnsService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-dns",
			Namespace: "kube-system",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.10",
		},
	}
	
	// Create fake node
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Spec: corev1.NodeSpec{
			PodCIDR: "10.244.0.0/24",
		},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				ContainerRuntimeVersion: "containerd://1.6.12",
			},
		},
	}
	
	// Create fake cluster-info configmap
	clusterInfoConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-info",
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"kubeconfig": `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJkekNDQVIyZ0F3SUJBZ0lCQURBS0JnZ3Foa2pPUFFRREFqQWpNU0V3SHdZRFZRUUREQmhyTTNNdGMyVnkKZG1WeUxXTmhRREUyTXpjME1UWXhNVEF3SGhjTk1qTXdOekEwTWpFeU5ERXdXaGNOTXpNd056QXhNakV5TkRFdwpXakFqTVNFd0h3WURWUVFEREJockszTXRjMlZ5ZG1WeUxXTmhRREUyTXpjME1UWXhNVEF3V1RBVEJnY3Foa2pPClBRSUJCZ2dxaGtqT1BRTUJCd05DQUFUWWk4UFpUZGdxaG1CTDA5Q2tTNzBLUjFvK2FUV0ZRVUhQRGVmd05uaFYKWHJ3SXNjZUNIcXFxYW5FQWlGaVFMRFNScjNOVVNRV0l3eWQ3Sm1vc0RqUG5vMEl3UURBT0JnTlZIUThCQWY4RQpCQU1DQXFRd0R3WURWUjBUQVFIL0JBVXdBd0VCL3pBZEJnTlZIUTRFRmdRVWZyVDFYKzFFTDB0dGg4eXQ2c0dvCjhyOWJ1RHN3Q2dZSUtvWkl6ajBFQXdJRFNBQXdSUUloQUpIZWMvMUF6bDhCTXVOTVNjZXB3ZVJwUG5pWFFvYnAKRUtjUlUvMENJM2VhQWlFQXc1V1FlSDJsQm5LZEVZUUx0MFlHS0JOYlpqOHFabTg1U3NiMnREZHJlNHM9Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
    server: https://kubernetes.default.svc.cluster.local:443
  name: kubernetes
contexts:
- context:
    cluster: kubernetes
    user: kubernetes-admin
  name: kubernetes-admin@kubernetes
current-context: kubernetes-admin@kubernetes
kind: Config
preferences: {}
users:
- name: kubernetes-admin
  user:
    client-certificate-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJkekNDQVIyZ0F3SUJBZ0lCQURBS0JnZ3Foa2pPUFFRREFqQWpNU0V3SHdZRFZRUUREQmhyTTNNdGMyVnkKZG1WeUxXTmhRREUyTXpjME1UWXhNVEF3SGhjTk1qTXdOekEwTWpFeU5ERXdXaGNOTXpNd056QXhNakV5TkRFdwpXakFqTVNFd0h3WURWUVFEREJockszTXRjMlZ5ZG1WeUxXTmhRREUyTXpjME1UWXhNVEF3V1RBVEJnY3Foa2pPClBRSUJCZ2dxaGtqT1BRTUJCd05DQUFUWWk4UFpUZGdxaG1CTDA5Q2tTNzBLUjFvK2FUV0ZRVUhQRGVmd05uaFYKWHJ3SXNjZUNIcXFxYW5FQWlGaVFMRFNScjNOVVNRV0l3eWQ3Sm1vc0RqUG5vMEl3UURBT0JnTlZIUThCQWY4RQpCQU1DQXFRd0R3WURWUjBUQVFIL0JBVXdBd0VCL3pBZEJnTlZIUTRFRmdRVWZyVDFYKzFFTDB0dGg4eXQ2c0dvCjhyOWJ1RHN3Q2dZSUtvWkl6ajBFQXdJRFNBQXdSUUloQUpIZWMvMUF6bDhCTXVOTVNjZXB3ZVJwUG5pWFFvYnAKRUtjUlUvMENJM2VhQWlFQXc1V1FlSDJsQm5LZEVZUUx0MFlHS0JOYlpqOHFabTg1U3NiMnREZHJlNHM9Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
    client-key-data: LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0tCk1IY0NBUUVFSU45dzlON1BBMUhsK2gzOGl1MkpSNjdnSnBSWmJYNEliQ25NMUIrT3NxeFZvQW9HQ0NxR1NNNDkKQXdFSG9VUURRZ0FFMkl2RDJVM1lLb1pnUzlQUXBFdTlDa2RhUG1rMWhVRkJ6dzNuOERaNFZWNjhDTEhIZ2g2cQpxbXB4QUloWWtDdzBrYTl6VkVrRmlNTW5leVpxTEE0ejV3PT0KLS0tLS1FTkQgRUMgUFJJVkFURSBLRVktLS0tLQo=`,
		},
	}
	
	return fake.NewSimpleClientset(kubernetesService, dnsService, node, clusterInfoConfigMap)
}

// Helper function to create a fake Kubernetes client with specific container runtime
func createFakeKubernetesClientWithRuntime(runtimeVersion string) *fake.Clientset {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				ContainerRuntimeVersion: runtimeVersion,
			},
		},
	}
	
	return fake.NewSimpleClientset(node)
}

func TestClusterConfigDiscovery(t *testing.T) {
	ctx := context.Background()
	
	// Create fake client with test resources
	fakeClient := createFakeKubernetesClient()
	
	// Discover cluster configuration
	config, err := DiscoverClusterConfig(ctx, fakeClient)
	require.NoError(t, err)
	require.NotNil(t, config)
	
	assert.Equal(t, "10.96.0.10", config.DNSClusterIP)
	assert.Equal(t, "10.244.0.0/24", config.ClusterCIDR)
	assert.Equal(t, "ipv4", config.IPFamily)
	
	t.Logf("Discovered cluster config: DNS=%s, CIDR=%s, CNI=%s, IPFamily=%s",
		config.DNSClusterIP, config.ClusterCIDR, config.CNIPlugin, config.IPFamily)
}

func TestBootstrapProvider_IKSModeDetection_Fix(t *testing.T) {
	ctx := context.Background()
	
	// Create fake Kubernetes client with cluster resources
	fakeClient := createFakeKubernetesClient()
	
	// Create mock IBM client
	mockClient := &ibm.Client{}
	
	// Create bootstrap provider
	provider := NewProvider(mockClient, fakeClient)
	
	tests := []struct {
		name               string
		nodeClassMode      *string
		nodeClassClusterID string
		envBootstrapMode   string
		envIKSClusterID    string
		expectedMode       BootstrapMode
		expectIKSManaged   bool
		expectedClusterID  string
	}{
		{
			name:               "NodeClass explicit IKS mode with cluster ID",
			nodeClassMode:      stringPtr("iks-api"),
			nodeClassClusterID: "d1jnl8od0fjpn1553n3g",
			envBootstrapMode:   "",
			envIKSClusterID:    "",
			expectedMode:       BootstrapModeIKSAPI,
			expectIKSManaged:   true,
			expectedClusterID:  "d1jnl8od0fjpn1553n3g",
		},
		{
			name:               "Environment IKS mode with cluster ID",
			nodeClassMode:      nil,
			nodeClassClusterID: "",
			envBootstrapMode:   "iks-api",
			envIKSClusterID:    "d1jnl8od0fjpn1553n3g",
			expectedMode:       BootstrapModeIKSAPI,
			expectIKSManaged:   true,
			expectedClusterID:  "d1jnl8od0fjpn1553n3g",
		},
		{
			name:               "Auto mode with NodeClass cluster ID",
			nodeClassMode:      nil,
			nodeClassClusterID: "d1jnl8od0fjpn1553n3g",
			envBootstrapMode:   "",
			envIKSClusterID:    "",
			expectedMode:       BootstrapModeAuto,
			expectIKSManaged:   true,
			expectedClusterID:  "d1jnl8od0fjpn1553n3g",
		},
		{
			name:               "Auto mode with environment cluster ID",
			nodeClassMode:      nil,
			nodeClassClusterID: "",
			envBootstrapMode:   "",
			envIKSClusterID:    "d1jnl8od0fjpn1553n3g",
			expectedMode:       BootstrapModeAuto,
			expectIKSManaged:   true,
			expectedClusterID:  "d1jnl8od0fjpn1553n3g",
		},
		{
			name:               "NodeClass overrides environment",
			nodeClassMode:      nil,
			nodeClassClusterID: "nodeclass-cluster-id",
			envBootstrapMode:   "",
			envIKSClusterID:    "env-cluster-id",
			expectedMode:       BootstrapModeAuto,
			expectIKSManaged:   true,
			expectedClusterID:  "nodeclass-cluster-id",
		},
		{
			name:               "No IKS configuration",
			nodeClassMode:      nil,
			nodeClassClusterID: "",
			envBootstrapMode:   "",
			envIKSClusterID:    "",
			expectedMode:       BootstrapModeAuto,
			expectIKSManaged:   false,
			expectedClusterID:  "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean environment
			_ = os.Unsetenv("BOOTSTRAP_MODE")
			_ = os.Unsetenv("IKS_CLUSTER_ID")
			
			// Set environment variables
			if tt.envBootstrapMode != "" {
				_ = os.Setenv("BOOTSTRAP_MODE", tt.envBootstrapMode)
			}
			if tt.envIKSClusterID != "" {
				_ = os.Setenv("IKS_CLUSTER_ID", tt.envIKSClusterID)
			}
			
			// Create NodeClass
			nodeClass := &v1alpha1.IBMNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: v1alpha1.IBMNodeClassSpec{
					Region: "us-south",
					VPC:    "test-vpc",
					Image:  "test-image",
				},
			}
			
			if tt.nodeClassMode != nil {
				nodeClass.Spec.BootstrapMode = tt.nodeClassMode
			}
			if tt.nodeClassClusterID != "" {
				nodeClass.Spec.IKSClusterID = tt.nodeClassClusterID
			}
			
			// Test the fix: create cluster info and determine bootstrap mode
			clusterInfo, err := provider.getClusterInfo(ctx)
			require.NoError(t, err)
			
			// Test the fixed bootstrap mode detection logic
			actualMode := provider.determineBootstrapMode(nodeClass, clusterInfo)
			
			// Verify results
			assert.Equal(t, tt.expectedMode, actualMode)
			assert.Equal(t, tt.expectIKSManaged, clusterInfo.IsIKSManaged)
			assert.Equal(t, tt.expectedClusterID, clusterInfo.IKSClusterID)
			
			// Clean up environment
			_ = os.Unsetenv("BOOTSTRAP_MODE")
			_ = os.Unsetenv("IKS_CLUSTER_ID")
		})
	}
}

func stringPtr(s string) *string {
	return &s
}

func TestIBMBootstrapProvider_getKubeconfigFromIKS(t *testing.T) {
	tests := []struct {
		name          string
		client        *ibm.Client
		expectedError string
	}{
		{
			name:          "nil client",
			client:        nil,
			expectedError: "IBM client not initialized",
		},
		{
			name: "client with nil IKS client components",
			client: &ibm.Client{},
			expectedError: "client not properly initialized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &IBMBootstrapProvider{
				client: tt.client,
			}

			ctx := context.Background()
			_, err := provider.getKubeconfigFromIKS(ctx, "test-cluster-id")

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestIBMBootstrapProvider_getIKSClusterID(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		expectedID  string
	}{
		{
			name:        "cluster ID from environment",
			envValue:    "cluster-from-env",
			expectedID:  "cluster-from-env",
		},
		{
			name:        "no cluster ID set",
			envValue:    "",
			expectedID:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up environment
			_ = os.Unsetenv("IKS_CLUSTER_ID")
			
			if tt.envValue != "" {
				_ = os.Setenv("IKS_CLUSTER_ID", tt.envValue)
			}

			provider := &IBMBootstrapProvider{}
			clusterID := provider.getIKSClusterID()

			assert.Equal(t, tt.expectedID, clusterID)

			// Clean up
			_ = os.Unsetenv("IKS_CLUSTER_ID")
		})
	}
}

func TestIBMBootstrapProvider_getClusterInfoWithNodeClass(t *testing.T) {
	tests := []struct {
		name              string
		nodeClass         *v1alpha1.IBMNodeClass
		envClusterID      string
		expectIKSAttempt  bool
		expectFallback    bool
	}{
		{
			name: "IKS mode with NodeClass cluster ID",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{
					IKSClusterID: "cluster-from-nodeclass",
				},
			},
			expectIKSAttempt: true,
			expectFallback:   true, // Since mock client will fail
		},
		{
			name: "IKS mode with environment cluster ID",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{},
			},
			envClusterID:     "cluster-from-env",
			expectIKSAttempt: true,
			expectFallback:   true,
		},
		{
			name: "no IKS configuration",
			nodeClass: &v1alpha1.IBMNodeClass{
				Spec: v1alpha1.IBMNodeClassSpec{},
			},
			expectIKSAttempt: false,
			expectFallback:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up environment
			_ = os.Unsetenv("IKS_CLUSTER_ID")
			
			if tt.envClusterID != "" {
				_ = os.Setenv("IKS_CLUSTER_ID", tt.envClusterID)
			}

			// Create fake Kubernetes client
			fakeClient := createFakeKubernetesClient()
			
			// Create mock IBM client (will fail IKS calls gracefully)
			mockClient := &ibm.Client{}
			
			provider := NewProvider(mockClient, fakeClient)

			ctx := context.Background()
			clusterInfo, err := provider.getClusterInfoWithNodeClass(ctx, tt.nodeClass)

			// Should always succeed due to fallback
			require.NoError(t, err)
			require.NotNil(t, clusterInfo)

			// Verify basic cluster info structure
			assert.NotEmpty(t, clusterInfo.Endpoint)
			assert.NotEmpty(t, clusterInfo.ClusterName)

			// Clean up
			_ = os.Unsetenv("IKS_CLUSTER_ID")
		})
	}
}

func TestIBMBootstrapProvider_parseKubeconfig(t *testing.T) {
	tests := []struct {
		name          string
		kubeconfig    string
		expectedURL   string
		expectCAData  bool
		expectedError string
	}{
		{
			name: "valid kubeconfig",
			kubeconfig: `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://c111.us-south.containers.cloud.ibm.com:30409
    certificate-authority-data: dGVzdC1jYS1kYXRh
  name: test-cluster`,
			expectedURL:  "https://c111.us-south.containers.cloud.ibm.com:30409",
			expectCAData: true,
		},
		{
			name: "kubeconfig without CA data",
			kubeconfig: `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://c111.us-south.containers.cloud.ibm.com:30409
  name: test-cluster`,
			expectedURL:  "https://c111.us-south.containers.cloud.ibm.com:30409",
			expectCAData: false,
		},
		{
			name: "kubeconfig without server",
			kubeconfig: `apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: dGVzdC1jYS1kYXRh
  name: test-cluster`,
			expectedError: "endpoint not found in kubeconfig",
		},
		{
			name: "invalid base64 CA data",
			kubeconfig: `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://c111.us-south.containers.cloud.ibm.com:30409
    certificate-authority-data: invalid-base64!@#$
  name: test-cluster`,
			expectedError: "decoding CA data:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &IBMBootstrapProvider{}
			
			endpoint, caData, err := provider.parseKubeconfig(tt.kubeconfig)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedURL, endpoint)
				
				if tt.expectCAData {
					assert.NotEmpty(t, caData)
					// Verify it's valid base64 decoded data
					assert.Equal(t, "test-ca-data", string(caData))
				} else {
					assert.Empty(t, caData)
				}
			}
		})
	}
}