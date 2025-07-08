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
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	commonTypes "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/types"
)

// VPCBootstrapProvider provides VPC-specific bootstrap functionality
type VPCBootstrapProvider struct {
	client    *ibm.Client
	k8sClient kubernetes.Interface
}

// NewVPCBootstrapProvider creates a new VPC bootstrap provider
func NewVPCBootstrapProvider(client *ibm.Client, k8sClient kubernetes.Interface) *VPCBootstrapProvider {
	return &VPCBootstrapProvider{
		client:    client,
		k8sClient: k8sClient,
	}
}

// GetUserData generates VPC-specific user data for node bootstrapping using cloud-init
func (p *VPCBootstrapProvider) GetUserData(ctx context.Context, nodeClass *v1alpha1.IBMNodeClass, nodeClaim types.NamespacedName) (string, error) {
	logger := log.FromContext(ctx)
	logger.Info("Generating VPC cloud-init user data for dynamic bootstrap")

	// Get internal API server endpoint (use internal cluster IP)
	clusterEndpoint, err := commonTypes.GetInternalAPIServerEndpoint(ctx, p.k8sClient)
	if err != nil {
		return "", fmt.Errorf("getting internal API server endpoint: %w", err)
	}

	// Generate or find bootstrap token (valid for 24 hours)
	bootstrapToken, err := commonTypes.FindOrCreateBootstrapToken(ctx, p.k8sClient, 24*time.Hour)
	if err != nil {
		return "", fmt.Errorf("generating bootstrap token: %w", err)
	}

	// Build bootstrap options for VPC mode
	options := commonTypes.Options{
		ClusterEndpoint:  clusterEndpoint,
		BootstrapToken:   bootstrapToken,
		CustomUserData:   nodeClass.Spec.UserData,
		Region:           nodeClass.Spec.Region,
		Zone:             nodeClass.Spec.Zone,
	}

	logger.Info("Generated bootstrap configuration", 
		"endpoint", clusterEndpoint,
		"token", fmt.Sprintf("%s...", bootstrapToken[:10]),
		"region", nodeClass.Spec.Region)

	// Generate cloud-init script for VPC
	return p.generateCloudInitScript(ctx, options)
}

// getClusterInfo retrieves cluster information for VPC mode
func (p *VPCBootstrapProvider) getClusterInfo(ctx context.Context) (*commonTypes.ClusterInfo, error) {
	// Get cluster endpoint from kube-system configmap or service
	config, err := p.k8sClient.CoreV1().ConfigMaps("kube-system").Get(ctx, "cluster-info", metav1.GetOptions{})
	if err != nil {
		// Fallback to kubernetes service
		kubeService, serviceErr := p.k8sClient.CoreV1().Services("default").Get(ctx, "kubernetes", metav1.GetOptions{})
		if serviceErr != nil {
			return nil, fmt.Errorf("getting cluster endpoint: %w", serviceErr)
		}
		
		endpoint := fmt.Sprintf("https://%s:%d", kubeService.Spec.ClusterIP, kubeService.Spec.Ports[0].Port)
		return &commonTypes.ClusterInfo{
			Endpoint:     endpoint,
			ClusterName:  p.getClusterName(),
			IsIKSManaged: false,
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

	return &commonTypes.ClusterInfo{
		Endpoint:     endpoint,
		CAData:       caData,
		ClusterName:  p.getClusterName(),
		IsIKSManaged: false,
	}, nil
}

// detectContainerRuntime detects the container runtime being used
func (p *VPCBootstrapProvider) detectContainerRuntime(ctx context.Context) string {
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

// buildKubeletConfig builds kubelet configuration for VPC mode
func (p *VPCBootstrapProvider) buildKubeletConfig(clusterConfig *commonTypes.ClusterConfig) *commonTypes.KubeletConfig {
	config := &commonTypes.KubeletConfig{
		ClusterDNS: []string{clusterConfig.DNSClusterIP},
		ExtraArgs:  make(map[string]string),
	}

	// Add cloud provider configuration for VPC mode
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
func (p *VPCBootstrapProvider) getClusterName() string {
	// For VPC mode, use a generic cluster name
	return "karpenter-vpc-cluster"
}

// parseKubeconfig parses kubeconfig to extract endpoint and CA data
func (p *VPCBootstrapProvider) parseKubeconfig(kubeconfig string) (string, []byte, error) {
	return commonTypes.ParseKubeconfig(kubeconfig)
}