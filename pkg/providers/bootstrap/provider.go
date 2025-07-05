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
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
)

// IBMBootstrapProvider provides bootstrap functionality for IBM Cloud
type IBMBootstrapProvider struct {
	client    *ibm.Client
	k8sClient kubernetes.Interface
}

// NewProvider creates a new bootstrap provider
func NewProvider(client *ibm.Client, k8sClient kubernetes.Interface) *IBMBootstrapProvider {
	return &IBMBootstrapProvider{
		client:    client,
		k8sClient: k8sClient,
	}
}

// GetUserData generates user data for node bootstrapping
func (p *IBMBootstrapProvider) GetUserData(ctx context.Context, nodeClass *v1alpha1.IBMNodeClass, nodeClaim types.NamespacedName) (string, error) {
	logger := log.FromContext(ctx)
	
	// Discover cluster configuration
	clusterConfig, err := DiscoverClusterConfig(ctx, p.k8sClient)
	if err != nil {
		return "", fmt.Errorf("discovering cluster config: %w", err)
	}

	// Get cluster information with NodeClass context
	clusterInfo, err := p.getClusterInfoWithNodeClass(ctx, nodeClass)
	if err != nil {
		return "", fmt.Errorf("getting cluster info: %w", err)
	}

	// Determine bootstrap mode
	bootstrapMode := p.determineBootstrapMode(nodeClass, clusterInfo)
	logger.Info("Using bootstrap mode", "mode", bootstrapMode)

	// Build bootstrap options
	options := Options{
		ClusterName:      clusterInfo.ClusterName,
		ClusterEndpoint:  clusterInfo.Endpoint,
		CABundle:         string(clusterInfo.CAData),
		ContainerRuntime: p.detectContainerRuntime(ctx),
		CNIPlugin:        clusterConfig.CNIPlugin,
		DNSClusterIP:     clusterConfig.DNSClusterIP,
		ClusterCIDR:      clusterConfig.ClusterCIDR,
		CustomUserData:   nodeClass.Spec.UserData,
		Region:           nodeClass.Spec.Region,
		Zone:             nodeClass.Spec.Zone,
		KubeletConfig:    p.buildKubeletConfig(clusterConfig),
	}

	// Generate bootstrap script based on mode
	switch bootstrapMode {
	case BootstrapModeCloudInit:
		return p.generateCloudInitScript(ctx, options)
	case BootstrapModeIKSAPI:
		return p.generateIKSAPIScript(ctx, options, clusterInfo)
	case BootstrapModeAuto:
		// Try IKS API first, fallback to cloud-init
		if clusterInfo.IsIKSManaged {
			return p.generateIKSAPIScript(ctx, options, clusterInfo)
		}
		return p.generateCloudInitScript(ctx, options)
	default:
		return "", fmt.Errorf("unsupported bootstrap mode: %s", bootstrapMode)
	}
}

// determineBootstrapMode determines the appropriate bootstrap mode
func (p *IBMBootstrapProvider) determineBootstrapMode(nodeClass *v1alpha1.IBMNodeClass, clusterInfo *ClusterInfo) BootstrapMode {
	// Check if bootstrap mode is specified in node class
	if nodeClass.Spec.BootstrapMode != nil {
		mode := BootstrapMode(*nodeClass.Spec.BootstrapMode)
		// Update cluster info for IKS mode detection
		if mode == BootstrapModeIKSAPI {
			p.updateClusterInfoForIKS(nodeClass, clusterInfo)
		}
		return mode
	}

	// Check environment variable
	if mode := os.Getenv("BOOTSTRAP_MODE"); mode != "" {
		bootstrapMode := BootstrapMode(mode)
		if bootstrapMode == BootstrapModeIKSAPI {
			p.updateClusterInfoForIKS(nodeClass, clusterInfo)
		}
		return bootstrapMode
	}

	// Auto mode - check for IKS configuration
	// Enhanced IKS detection: check both environment variable AND NodeClass
	hasIKSClusterID := os.Getenv("IKS_CLUSTER_ID") != "" || nodeClass.Spec.IKSClusterID != ""
	if hasIKSClusterID {
		p.updateClusterInfoForIKS(nodeClass, clusterInfo)
	}

	// Default to auto mode
	return BootstrapModeAuto
}

// getClusterInfo retrieves cluster information
func (p *IBMBootstrapProvider) getClusterInfo(ctx context.Context) (*ClusterInfo, error) {
	// Check if this is an IKS managed cluster and try to get kubeconfig from IKS API first
	if p.isIKSManaged() {
		clusterID := p.getIKSClusterID()
		if clusterID != "" {
			if kubeconfig, err := p.getKubeconfigFromIKS(ctx, clusterID); err == nil {
				// Successfully retrieved kubeconfig from IKS API
				endpoint, caData, err := p.parseKubeconfig(kubeconfig)
				if err != nil {
					return nil, fmt.Errorf("parsing kubeconfig from IKS API: %w", err)
				}
				return &ClusterInfo{
					Endpoint:     endpoint,
					CAData:       caData,
					ClusterName:  p.getClusterName(),
					IsIKSManaged: p.isIKSManaged(),
				}, nil
			}
			// If IKS API fails, continue with fallback methods
		}
	}

	// Get cluster endpoint from kube-system configmap or service
	config, err := p.k8sClient.CoreV1().ConfigMaps("kube-system").Get(ctx, "cluster-info", metav1.GetOptions{})
	if err != nil {
		// Fallback to kubernetes service
		kubeService, serviceErr := p.k8sClient.CoreV1().Services("default").Get(ctx, "kubernetes", metav1.GetOptions{})
		if serviceErr != nil {
			return nil, fmt.Errorf("getting cluster endpoint: %w", serviceErr)
		}
		
		endpoint := fmt.Sprintf("https://%s:%d", kubeService.Spec.ClusterIP, kubeService.Spec.Ports[0].Port)
		return &ClusterInfo{
			Endpoint:     endpoint,
			ClusterName:  p.getClusterName(),
			IsIKSManaged: p.isIKSManaged(),
		}, nil
	}

	// Parse cluster-info configmap
	kubeconfig, exists := config.Data["kubeconfig"]
	if !exists {
		return nil, fmt.Errorf("kubeconfig not found in cluster-info configmap")
	}

	// Extract endpoint and CA data from kubeconfig
	endpoint, caData, err := p.parseKubeconfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("parsing kubeconfig: %w", err)
	}

	return &ClusterInfo{
		Endpoint:     endpoint,
		CAData:       caData,
		ClusterName:  p.getClusterName(),
		IsIKSManaged: p.isIKSManaged(),
	}, nil
}

// detectContainerRuntime detects the container runtime being used
func (p *IBMBootstrapProvider) detectContainerRuntime(ctx context.Context) string {
	// Check if containerd is running
	nodes, err := p.k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil || len(nodes.Items) == 0 {
		return "containerd" // Default
	}

	node := nodes.Items[0]
	if node.Status.NodeInfo.ContainerRuntimeVersion != "" {
		runtime := strings.Split(node.Status.NodeInfo.ContainerRuntimeVersion, "://")[0]
		return runtime
	}

	return "containerd" // Default
}

// buildKubeletConfig builds kubelet configuration
func (p *IBMBootstrapProvider) buildKubeletConfig(clusterConfig *ClusterConfig) *KubeletConfig {
	config := &KubeletConfig{
		ClusterDNS: []string{clusterConfig.DNSClusterIP},
		ExtraArgs:  make(map[string]string),
	}

	// Add cloud provider configuration
	config.ExtraArgs["cloud-provider"] = "external"
	config.ExtraArgs["provider-id"] = "ibm://$(curl -s http://169.254.169.254/metadata/v1/instance/id)"

	// Add network configuration based on CNI
	switch clusterConfig.CNIPlugin {
	case "calico":
		config.ExtraArgs["network-plugin"] = "cni"
		config.ExtraArgs["cni-conf-dir"] = "/etc/cni/net.d"
		config.ExtraArgs["cni-bin-dir"] = "/opt/cni/bin"
	case "cilium":
		config.ExtraArgs["network-plugin"] = "cni"
		config.ExtraArgs["cni-conf-dir"] = "/etc/cni/net.d"
		config.ExtraArgs["cni-bin-dir"] = "/opt/cni/bin"
	}

	return config
}

// getClusterName gets the cluster name from environment or defaults
func (p *IBMBootstrapProvider) getClusterName() string {
	if name := os.Getenv("CLUSTER_NAME"); name != "" {
		return name
	}
	return "karpenter-cluster"
}

// isIKSManaged checks if this is an IKS-managed cluster
func (p *IBMBootstrapProvider) isIKSManaged() bool {
	return os.Getenv("IKS_CLUSTER_ID") != ""
}

// updateClusterInfoForIKS updates cluster info with IKS configuration from NodeClass or environment
func (p *IBMBootstrapProvider) updateClusterInfoForIKS(nodeClass *v1alpha1.IBMNodeClass, clusterInfo *ClusterInfo) {
	clusterInfo.IsIKSManaged = true
	
	// Set IKS cluster ID from NodeClass or environment variable
	if nodeClass.Spec.IKSClusterID != "" {
		clusterInfo.IKSClusterID = nodeClass.Spec.IKSClusterID
	} else if envClusterID := os.Getenv("IKS_CLUSTER_ID"); envClusterID != "" {
		clusterInfo.IKSClusterID = envClusterID
	}
	
	// Set worker pool ID if specified
	if nodeClass.Spec.IKSWorkerPoolID != "" {
		clusterInfo.IKSWorkerPoolID = nodeClass.Spec.IKSWorkerPoolID
	}
}

// parseKubeconfig parses kubeconfig to extract endpoint and CA data
func (p *IBMBootstrapProvider) parseKubeconfig(kubeconfig string) (string, []byte, error) {
	// This is a simplified parser - in production, you'd want to use a proper YAML parser
	lines := strings.Split(kubeconfig, "\n")
	var endpoint string
	var caData []byte
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "server:") {
			endpoint = strings.TrimSpace(strings.TrimPrefix(line, "server:"))
		}
		if strings.HasPrefix(line, "certificate-authority-data:") {
			caDataStr := strings.TrimSpace(strings.TrimPrefix(line, "certificate-authority-data:"))
			var err error
			caData, err = base64.StdEncoding.DecodeString(caDataStr)
			if err != nil {
				return "", nil, fmt.Errorf("decoding CA data: %w", err)
			}
		}
	}
	
	if endpoint == "" {
		return "", nil, fmt.Errorf("endpoint not found in kubeconfig")
	}
	
	return endpoint, caData, nil
}

// getKubeconfigFromIKS retrieves kubeconfig from IKS API
func (p *IBMBootstrapProvider) getKubeconfigFromIKS(ctx context.Context, clusterID string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("IBM client not initialized")
	}
	
	iksClient := p.client.GetIKSClient()
	if iksClient == nil {
		return "", fmt.Errorf("IKS client not available")
	}
	
	return iksClient.GetClusterConfig(ctx, clusterID)
}

// getIKSClusterID retrieves the IKS cluster ID from environment or context
func (p *IBMBootstrapProvider) getIKSClusterID() string {
	// Check environment variable first
	if clusterID := os.Getenv("IKS_CLUSTER_ID"); clusterID != "" {
		return clusterID
	}
	
	// Could also check from cluster-info ConfigMap data if available
	// For now, just return empty string if not found
	return ""
}

// getClusterInfoWithNodeClass retrieves cluster information with access to NodeClass
func (p *IBMBootstrapProvider) getClusterInfoWithNodeClass(ctx context.Context, nodeClass *v1alpha1.IBMNodeClass) (*ClusterInfo, error) {
	// Check if this is an IKS managed cluster and try to get kubeconfig from IKS API first
	isIKSManaged := p.isIKSManaged() || nodeClass.Spec.IKSClusterID != ""
	if isIKSManaged {
		clusterID := nodeClass.Spec.IKSClusterID
		if clusterID == "" {
			clusterID = os.Getenv("IKS_CLUSTER_ID")
		}
		
		if clusterID != "" {
			kubeconfig, err := p.getKubeconfigFromIKS(ctx, clusterID)
			if err == nil && kubeconfig != "" {
				// Successfully retrieved kubeconfig from IKS API
				endpoint, caData, parseErr := p.parseKubeconfig(kubeconfig)
				if parseErr != nil {
					return nil, fmt.Errorf("parsing kubeconfig from IKS API: %w", parseErr)
				}
				return &ClusterInfo{
					Endpoint:     endpoint,
					CAData:       caData,
					ClusterName:  p.getClusterName(),
					IsIKSManaged: true,
					IKSClusterID: clusterID,
				}, nil
			}
			// If IKS API fails, log the error and continue with fallback methods
			logger := log.FromContext(ctx)
			logger.Info("Failed to get kubeconfig from IKS API, falling back to ConfigMap", "error", err)
		}
	}

	// Fall back to the standard getClusterInfo method
	return p.getClusterInfo(ctx)
}