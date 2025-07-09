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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	commonTypes "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/types"
)

// MockK8sClient provides a mock implementation for specific operations
type MockK8sClient struct {
	mock.Mock
	fake.Clientset
}

func (m *MockK8sClient) CoreV1() typedcorev1.CoreV1Interface {
	args := m.Called()
	return args.Get(0).(typedcorev1.CoreV1Interface)
}

// MockCoreV1 provides a mock implementation of CoreV1Interface
type MockCoreV1 struct {
	mock.Mock
}

func (m *MockCoreV1) ConfigMaps(namespace string) typedcorev1.ConfigMapInterface {
	args := m.Called(namespace)
	return args.Get(0).(typedcorev1.ConfigMapInterface)
}

func (m *MockCoreV1) Services(namespace string) typedcorev1.ServiceInterface {
	args := m.Called(namespace)
	return args.Get(0).(typedcorev1.ServiceInterface)
}

func (m *MockCoreV1) Nodes() typedcorev1.NodeInterface {
	args := m.Called()
	return args.Get(0).(typedcorev1.NodeInterface)
}

// Test helpers
func getTestNodeClass() *v1alpha1.IBMNodeClass {
	return &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:   "us-south",
			Zone:     "us-south-1",
			Image:    "test-image",
			VPC:      "test-vpc",
			UserData: "#!/bin/bash\necho 'custom user data'",
		},
	}
}

func getTestClusterInfoConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-info",
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"kubeconfig": `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: dGVzdC1jYS1kYXRh
    server: https://test-cluster.us-south.containers.cloud.ibm.com:31234
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
kind: Config
users:
- name: test-user
  user:
    token: test-token`,
		},
	}
}

func getTestKubernetesService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubernetes",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.1",
			Ports: []corev1.ServicePort{
				{
					Port: 443,
				},
			},
		},
	}
}

func getTestDNSService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-dns",
			Namespace: "kube-system",
			Labels: map[string]string{
				"k8s-app": "kube-dns",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.10",
			Ports: []corev1.ServicePort{
				{
					Port: 53,
					Protocol: corev1.ProtocolUDP,
				},
			},
		},
	}
}

func getTestNode() *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				ContainerRuntimeVersion: "containerd://1.6.8",
			},
		},
	}
}

func TestVPCBootstrapProvider_GetUserData(t *testing.T) {
	tests := []struct {
		name            string
		nodeClass       *v1alpha1.IBMNodeClass
		nodeClaim       types.NamespacedName
		setupMocks      func(*fake.Clientset)
		expectError     bool
		errorContains   string
		validateUserData func(*testing.T, string)
	}{
		{
			name:      "successful user data generation with cluster-info configmap",
			nodeClass: getTestNodeClass(),
			nodeClaim: types.NamespacedName{Name: "test-nodeclaim", Namespace: "default"},
			setupMocks: func(fakeClient *fake.Clientset) {
				// Add cluster-info configmap
				clusterInfo := getTestClusterInfoConfigMap()
				_, _ = fakeClient.CoreV1().ConfigMaps("kube-system").Create(context.Background(), clusterInfo, metav1.CreateOptions{})
				
				// Add DNS service for cluster discovery
				dnsService := getTestDNSService()
				_, _ = fakeClient.CoreV1().Services("kube-system").Create(context.Background(), dnsService, metav1.CreateOptions{})
				
				// Add kubernetes service for cluster CIDR discovery
				kubeService := getTestKubernetesService()
				_, _ = fakeClient.CoreV1().Services("default").Create(context.Background(), kubeService, metav1.CreateOptions{})
				
				// Add test node for container runtime detection
				testNode := getTestNode()
				_, _ = fakeClient.CoreV1().Nodes().Create(context.Background(), testNode, metav1.CreateOptions{})
			},
			expectError: false,
			validateUserData: func(t *testing.T, userData string) {
				assert.NotEmpty(t, userData)
				
				// User data is now plain text (no base64 encoding)
				assert.Contains(t, userData, "#!/bin/bash")
				// Should contain custom user data from NodeClass
				assert.Contains(t, userData, "custom user data")
			},
		},
		{
			name:      "fallback to kubernetes service when configmap not found",
			nodeClass: getTestNodeClass(),
			nodeClaim: types.NamespacedName{Name: "test-nodeclaim", Namespace: "default"},
			setupMocks: func(fakeClient *fake.Clientset) {
				// Add kubernetes service for fallback
				kubeService := getTestKubernetesService()
				_, _ = fakeClient.CoreV1().Services("default").Create(context.Background(), kubeService, metav1.CreateOptions{})
				
				// Add DNS service for cluster discovery
				dnsService := getTestDNSService()
				_, _ = fakeClient.CoreV1().Services("kube-system").Create(context.Background(), dnsService, metav1.CreateOptions{})
				
				// Add test node
				testNode := getTestNode()
				_, _ = fakeClient.CoreV1().Nodes().Create(context.Background(), testNode, metav1.CreateOptions{})
			},
			expectError: false,
			validateUserData: func(t *testing.T, userData string) {
				assert.NotEmpty(t, userData)
				
				// User data is now plain text (no base64 encoding)
				assert.Contains(t, userData, "#!/bin/bash")
			},
		},
		{
			name: "nodeclass without custom user data",
			nodeClass: func() *v1alpha1.IBMNodeClass {
				nc := getTestNodeClass()
				nc.Spec.UserData = "" // Remove custom user data
				return nc
			}(),
			nodeClaim: types.NamespacedName{Name: "test-nodeclaim", Namespace: "default"},
			setupMocks: func(fakeClient *fake.Clientset) {
				clusterInfo := getTestClusterInfoConfigMap()
				_, _ = fakeClient.CoreV1().ConfigMaps("kube-system").Create(context.Background(), clusterInfo, metav1.CreateOptions{})
				
				// Add DNS service for cluster discovery
				dnsService := getTestDNSService()
				_, _ = fakeClient.CoreV1().Services("kube-system").Create(context.Background(), dnsService, metav1.CreateOptions{})
				
				// Add kubernetes service for cluster CIDR discovery
				kubeService := getTestKubernetesService()
				_, _ = fakeClient.CoreV1().Services("default").Create(context.Background(), kubeService, metav1.CreateOptions{})
				
				testNode := getTestNode()
				_, _ = fakeClient.CoreV1().Nodes().Create(context.Background(), testNode, metav1.CreateOptions{})
			},
			expectError: false,
			validateUserData: func(t *testing.T, userData string) {
				assert.NotEmpty(t, userData)
				
				// User data is now plain text (no base64 encoding)
				assert.Contains(t, userData, "#!/bin/bash")
				// Should not contain the actual custom user data content (echo 'custom user data')
				assert.NotContains(t, userData, "echo 'custom user data'")
			},
		},
		{
			name:      "cluster info discovery failure",
			nodeClass: getTestNodeClass(),
			nodeClaim: types.NamespacedName{Name: "test-nodeclaim", Namespace: "default"},
			setupMocks: func(fakeClient *fake.Clientset) {
				// Don't add any cluster info sources - both configmap and service will fail
			},
			expectError:   true,
			errorContains: "getting internal API server endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create fake Kubernetes client
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			fakeClient := fake.NewSimpleClientset()

			// Setup mocks
			tt.setupMocks(fakeClient)

			// Create VPC bootstrap provider
			provider := NewVPCBootstrapProvider(nil, fakeClient)

			// Test GetUserData method
			result, err := provider.GetUserData(ctx, tt.nodeClass, tt.nodeClaim)

			// Validate results
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, result)
				if tt.validateUserData != nil {
					tt.validateUserData(t, result)
				}
			}
		})
	}
}

func TestVPCBootstrapProvider_getClusterInfo(t *testing.T) {
	tests := []struct {
		name           string
		setupMocks     func(*fake.Clientset)
		expectError    bool
		errorContains  string
		validateResult func(*testing.T, *commonTypes.ClusterInfo)
	}{
		{
			name: "successful cluster info from configmap",
			setupMocks: func(fakeClient *fake.Clientset) {
				clusterInfo := getTestClusterInfoConfigMap()
				_, _ = fakeClient.CoreV1().ConfigMaps("kube-system").Create(context.Background(), clusterInfo, metav1.CreateOptions{})
			},
			expectError: false,
			validateResult: func(t *testing.T, info *commonTypes.ClusterInfo) {
				assert.NotNil(t, info)
				assert.Equal(t, "https://test-cluster.us-south.containers.cloud.ibm.com:31234", info.Endpoint)
				assert.NotEmpty(t, info.CAData)
				assert.Equal(t, "karpenter-vpc-cluster", info.ClusterName)
				assert.False(t, info.IsIKSManaged)
			},
		},
		{
			name: "fallback to kubernetes service",
			setupMocks: func(fakeClient *fake.Clientset) {
				// Don't create configmap, but create service for fallback
				kubeService := getTestKubernetesService()
				_, _ = fakeClient.CoreV1().Services("default").Create(context.Background(), kubeService, metav1.CreateOptions{})
			},
			expectError: false,
			validateResult: func(t *testing.T, info *commonTypes.ClusterInfo) {
				assert.NotNil(t, info)
				assert.Equal(t, "https://10.96.0.1:443", info.Endpoint)
				assert.Equal(t, "karpenter-vpc-cluster", info.ClusterName)
				assert.False(t, info.IsIKSManaged)
			},
		},
		{
			name: "configmap exists but no kubeconfig",
			setupMocks: func(fakeClient *fake.Clientset) {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-info",
						Namespace: "kube-system",
					},
					Data: map[string]string{
						"other-data": "not-kubeconfig",
					},
				}
				_, _ = fakeClient.CoreV1().ConfigMaps("kube-system").Create(context.Background(), configMap, metav1.CreateOptions{})
			},
			expectError:   true,
			errorContains: "kubeconfig not found in cluster-info configmap",
		},
		{
			name: "no cluster info sources available",
			setupMocks: func(fakeClient *fake.Clientset) {
				// Don't create anything - both configmap and service will fail
			},
			expectError:   true,
			errorContains: "getting cluster endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create fake Kubernetes client
			fakeClient := fake.NewSimpleClientset()

			// Setup mocks
			tt.setupMocks(fakeClient)

			// Create VPC bootstrap provider
			provider := NewVPCBootstrapProvider(nil, fakeClient)

			// Test getClusterInfo method
			result, err := provider.getClusterInfo(ctx)

			// Validate results
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.validateResult != nil {
					tt.validateResult(t, result)
				}
			}
		})
	}
}

func TestVPCBootstrapProvider_detectContainerRuntime(t *testing.T) {
	tests := []struct {
		name           string
		setupMocks     func(*fake.Clientset)
		expectedRuntime string
	}{
		{
			name: "containerd runtime detected",
			setupMocks: func(fakeClient *fake.Clientset) {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
					Status: corev1.NodeStatus{
						NodeInfo: corev1.NodeSystemInfo{
							ContainerRuntimeVersion: "containerd://1.6.8",
						},
					},
				}
				_, _ = fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
			},
			expectedRuntime: "containerd",
		},
		{
			name: "cri-o runtime detected",
			setupMocks: func(fakeClient *fake.Clientset) {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
					Status: corev1.NodeStatus{
						NodeInfo: corev1.NodeSystemInfo{
							ContainerRuntimeVersion: "cri-o://1.25.0",
						},
					},
				}
				_, _ = fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
			},
			expectedRuntime: "cri-o",
		},
		{
			name: "no nodes available - default to containerd",
			setupMocks: func(fakeClient *fake.Clientset) {
				// Don't create any nodes
			},
			expectedRuntime: "containerd",
		},
		{
			name: "node with empty runtime version - default to containerd",
			setupMocks: func(fakeClient *fake.Clientset) {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
					Status: corev1.NodeStatus{
						NodeInfo: corev1.NodeSystemInfo{
							ContainerRuntimeVersion: "",
						},
					},
				}
				_, _ = fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
			},
			expectedRuntime: "containerd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create fake Kubernetes client
			fakeClient := fake.NewSimpleClientset()

			// Setup mocks
			tt.setupMocks(fakeClient)

			// Create VPC bootstrap provider
			provider := NewVPCBootstrapProvider(nil, fakeClient)

			// Test detectContainerRuntime method
			result := provider.detectContainerRuntime(ctx)

			// Validate result
			assert.Equal(t, tt.expectedRuntime, result)
		})
	}
}

func TestVPCBootstrapProvider_buildKubeletConfig(t *testing.T) {
	tests := []struct {
		name           string
		clusterConfig  *commonTypes.ClusterConfig
		validateConfig func(*testing.T, *commonTypes.KubeletConfig)
	}{
		{
			name: "calico CNI configuration",
			clusterConfig: &commonTypes.ClusterConfig{
				DNSClusterIP: "10.96.0.10",
				CNIPlugin:    "calico",
				ClusterCIDR:  "10.244.0.0/16",
			},
			validateConfig: func(t *testing.T, config *commonTypes.KubeletConfig) {
				assert.NotNil(t, config)
				assert.Equal(t, []string{"10.96.0.10"}, config.ClusterDNS)
				assert.Equal(t, "external", config.ExtraArgs["cloud-provider"])
				assert.Equal(t, "cni", config.ExtraArgs["network-plugin"])
				assert.Equal(t, "/etc/cni/net.d", config.ExtraArgs["cni-conf-dir"])
				assert.Equal(t, "/opt/cni/bin", config.ExtraArgs["cni-bin-dir"])
				assert.Contains(t, config.ExtraArgs["provider-id"], "ibm://")
			},
		},
		{
			name: "cilium CNI configuration",
			clusterConfig: &commonTypes.ClusterConfig{
				DNSClusterIP: "10.96.0.10",
				CNIPlugin:    "cilium",
				ClusterCIDR:  "10.244.0.0/16",
			},
			validateConfig: func(t *testing.T, config *commonTypes.KubeletConfig) {
				assert.NotNil(t, config)
				assert.Equal(t, []string{"10.96.0.10"}, config.ClusterDNS)
				assert.Equal(t, "external", config.ExtraArgs["cloud-provider"])
				assert.Equal(t, "cni", config.ExtraArgs["network-plugin"])
				assert.Equal(t, "/etc/cni/net.d", config.ExtraArgs["cni-conf-dir"])
				assert.Equal(t, "/opt/cni/bin", config.ExtraArgs["cni-bin-dir"])
			},
		},
		{
			name: "unknown CNI - basic configuration",
			clusterConfig: &commonTypes.ClusterConfig{
				DNSClusterIP: "10.96.0.10",
				CNIPlugin:    "unknown",
				ClusterCIDR:  "10.244.0.0/16",
			},
			validateConfig: func(t *testing.T, config *commonTypes.KubeletConfig) {
				assert.NotNil(t, config)
				assert.Equal(t, []string{"10.96.0.10"}, config.ClusterDNS)
				assert.Equal(t, "external", config.ExtraArgs["cloud-provider"])
				// Should not have CNI-specific configuration for unknown plugin
				assert.NotContains(t, config.ExtraArgs, "network-plugin")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create VPC bootstrap provider
			provider := NewVPCBootstrapProvider(nil, nil)

			// Test buildKubeletConfig method
			result := provider.buildKubeletConfig(tt.clusterConfig)

			// Validate result
			assert.NotNil(t, result)
			if tt.validateConfig != nil {
				tt.validateConfig(t, result)
			}
		})
	}
}

func TestVPCBootstrapProvider_parseKubeconfig(t *testing.T) {
	tests := []struct {
		name          string
		kubeconfig    string
		expectError   bool
		errorContains string
		expectedEndpoint string
		expectCAData  bool
	}{
		{
			name: "valid kubeconfig",
			kubeconfig: `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: dGVzdC1jYS1kYXRh
    server: https://test-cluster.us-south.containers.cloud.ibm.com:31234
  name: test-cluster`,
			expectError:      false,
			expectedEndpoint: "https://test-cluster.us-south.containers.cloud.ibm.com:31234",
			expectCAData:     true,
		},
		{
			name: "kubeconfig without CA data",
			kubeconfig: `apiVersion: v1
clusters:
- cluster:
    server: https://test-cluster.us-south.containers.cloud.ibm.com:31234
  name: test-cluster`,
			expectError:      false,
			expectedEndpoint: "https://test-cluster.us-south.containers.cloud.ibm.com:31234",
			expectCAData:     false,
		},
		{
			name: "kubeconfig without server",
			kubeconfig: `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: dGVzdC1jYS1kYXRh
  name: test-cluster`,
			expectError:   true,
			errorContains: "endpoint not found in kubeconfig",
		},
		{
			name: "invalid base64 CA data",
			kubeconfig: `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: invalid-base64!@#$
    server: https://test-cluster.us-south.containers.cloud.ibm.com:31234
  name: test-cluster`,
			expectError:   true,
			errorContains: "decoding CA data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create VPC bootstrap provider
			provider := NewVPCBootstrapProvider(nil, nil)

			// Test parseKubeconfig method
			endpoint, caData, err := provider.parseKubeconfig(tt.kubeconfig)

			// Validate results
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedEndpoint, endpoint)
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