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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	commonTypes "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/types"
)

// VPCBootstrapProvider provides VPC-specific bootstrap functionality
type VPCBootstrapProvider struct {
	client       *ibm.Client
	k8sClient    kubernetes.Interface
	kubeClient   client.Client
}

// NewVPCBootstrapProvider creates a new VPC bootstrap provider
func NewVPCBootstrapProvider(client *ibm.Client, k8sClient kubernetes.Interface, kubeClient client.Client) *VPCBootstrapProvider {
	return &VPCBootstrapProvider{
		client:     client,
		k8sClient:  k8sClient,
		kubeClient: kubeClient,
	}
}

// GetUserData generates VPC-specific user data for node bootstrapping using cloud-init
func (p *VPCBootstrapProvider) GetUserData(ctx context.Context, nodeClass *v1alpha1.IBMNodeClass, nodeClaim types.NamespacedName) (string, error) {
	return p.GetUserDataWithInstanceID(ctx, nodeClass, nodeClaim, "")
}

// GetUserDataWithInstanceID generates VPC-specific user data with a known instance ID
func (p *VPCBootstrapProvider) GetUserDataWithInstanceID(ctx context.Context, nodeClass *v1alpha1.IBMNodeClass, nodeClaim types.NamespacedName, instanceID string) (string, error) {
	logger := log.FromContext(ctx)
	logger.Info("Generating VPC cloud-init user data for direct kubelet bootstrap")

	// Get API server endpoint - use NodeClass override if specified
	var clusterEndpoint string
	var err error
	
	if nodeClass.Spec.APIServerEndpoint != "" {
		clusterEndpoint = nodeClass.Spec.APIServerEndpoint
		logger.Info("Using API server endpoint from NodeClass", "endpoint", clusterEndpoint)
	} else {
		// Fallback to automatic discovery
		clusterEndpoint, err = commonTypes.GetInternalAPIServerEndpoint(ctx, p.k8sClient)
		if err != nil {
			return "", fmt.Errorf("getting internal API server endpoint: %w", err)
		}
		logger.Info("Discovered API server endpoint", "endpoint", clusterEndpoint)
	}

	// Generate or find bootstrap token (valid for 24 hours)
	bootstrapToken, err := commonTypes.FindOrCreateBootstrapToken(ctx, p.k8sClient, 24*time.Hour)
	if err != nil {
		return "", fmt.Errorf("generating bootstrap token: %w", err)
	}

	// Extract CA certificate from current kubeconfig for static configuration
	caCert, err := p.getClusterCA(ctx)
	if err != nil {
		return "", fmt.Errorf("getting cluster CA certificate: %w", err)
	}

	// Get cluster DNS IP (typically 172.21.0.10 for IBM Cloud)
	clusterDNS, err := p.getClusterDNS(ctx)
	if err != nil {
		// Fallback to common default
		clusterDNS = "172.21.0.10"
		logger.Info("Using default cluster DNS", "dns", clusterDNS)
	}

	// Detect container runtime from existing nodes
	containerRuntime := p.detectContainerRuntime(ctx)
	
	// Detect CNI plugin and version from cluster configuration
	cniPlugin, cniVersion := p.detectCNIPluginAndVersion(ctx)
	
	// Get NodeClaim to extract labels, taints, and architecture
	nodeClaimObj, err := p.getNodeClaim(ctx, nodeClaim)
	if err != nil {
		logger.Error(err, "Failed to get NodeClaim, proceeding without labels/taints")
	}
	
	// Detect architecture from instance profile
	architecture := p.detectArchitectureFromInstanceProfile(nodeClass.Spec.InstanceProfile)
	
	// Build bootstrap options for direct kubelet
	options := commonTypes.Options{
		ClusterEndpoint:  clusterEndpoint,
		BootstrapToken:   bootstrapToken,
		CustomUserData:   nodeClass.Spec.UserData,
		ContainerRuntime: containerRuntime,
		CNIPlugin:        cniPlugin,
		CNIVersion:       cniVersion,
		Architecture:     architecture,
		Region:           nodeClass.Spec.Region,
		Zone:             nodeClass.Spec.Zone,
		CABundle:         caCert,
		DNSClusterIP:     clusterDNS,
		NodeName:         nodeClaim.Name, // Use NodeClaim name as the node name
		InstanceID:       instanceID,     // Pass the instance ID if provided
		ProviderID:       "", // Will be set from NodeClaim if available
	}
	
	// Add labels and taints if NodeClaim was found
	if nodeClaimObj != nil {
		// Start with NodeClaim labels
		options.Labels = make(map[string]string)
		for k, v := range nodeClaimObj.Labels {
			options.Labels[k] = v
		}
		
		// Ensure essential Karpenter labels are present
		if nodePool, exists := nodeClaimObj.Labels["karpenter.sh/nodepool"]; exists {
			options.Labels["karpenter.sh/nodepool"] = nodePool
		}
		if nodeClass, exists := nodeClaimObj.Labels["karpenter.ibm.sh/ibmnodeclass"]; exists {
			options.Labels["karpenter.ibm.sh/ibmnodeclass"] = nodeClass
		}
		
		// Add taints from NodeClaim
		options.Taints = nodeClaimObj.Spec.Taints
	} else {
		// If no NodeClaim found, create basic labels map
		options.Labels = make(map[string]string)
	}
	
	// Add kubelet extra args if needed
	if options.KubeletConfig == nil {
		options.KubeletConfig = &commonTypes.KubeletConfig{
			ExtraArgs: make(map[string]string),
		}
	}

	logger.Info("Generated bootstrap configuration", 
		"endpoint", clusterEndpoint,
		"token", fmt.Sprintf("%s...", bootstrapToken[:10]),
		"region", nodeClass.Spec.Region,
		"dns", clusterDNS,
		"cni", cniPlugin,
		"cniVersion", cniVersion,
		"runtime", containerRuntime)

	// Generate cloud-init script for direct kubelet
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

// detectCNIPluginAndVersion detects the CNI plugin and version being used in the cluster
func (p *VPCBootstrapProvider) detectCNIPluginAndVersion(ctx context.Context) (string, string) {
	logger := log.FromContext(ctx)
	
	// Check for Calico
	if ds, err := p.k8sClient.AppsV1().DaemonSets("kube-system").Get(ctx, "calico-node", metav1.GetOptions{}); err == nil {
		version := p.extractVersionFromImage(ds.Spec.Template.Spec.Containers[0].Image)
		logger.Info("Detected Calico CNI plugin", "version", version)
		return "calico", version
	}
	
	// Check for Cilium
	if ds, err := p.k8sClient.AppsV1().DaemonSets("kube-system").Get(ctx, "cilium", metav1.GetOptions{}); err == nil {
		version := p.extractVersionFromImage(ds.Spec.Template.Spec.Containers[0].Image)
		logger.Info("Detected Cilium CNI plugin", "version", version)
		return "cilium", version
	}
	
	// Check for Flannel
	if ds, err := p.k8sClient.AppsV1().DaemonSets("kube-system").Get(ctx, "kube-flannel-ds", metav1.GetOptions{}); err == nil {
		version := p.extractVersionFromImage(ds.Spec.Template.Spec.Containers[0].Image)
		logger.Info("Detected Flannel CNI plugin", "version", version)
		return "flannel", version
	}
	
	// Check for Weave Net
	if ds, err := p.k8sClient.AppsV1().DaemonSets("kube-system").Get(ctx, "weave-net", metav1.GetOptions{}); err == nil {
		version := p.extractVersionFromImage(ds.Spec.Template.Spec.Containers[0].Image)
		logger.Info("Detected Weave Net CNI plugin", "version", version)
		return "weave", version
	}
	
	// Check for CNI configuration files in ConfigMaps
	if cm, err := p.k8sClient.CoreV1().ConfigMaps("kube-system").Get(ctx, "calico-config", metav1.GetOptions{}); err == nil {
		if _, exists := cm.Data["cni_network_config"]; exists {
			logger.Info("Detected Calico CNI plugin from ConfigMap")
			return "calico", "v3.26.4" // Default version if we can't extract it
		}
	}
	
	logger.Info("CNI plugin not detected, defaulting to calico")
	return "calico", "v3.26.4" // Default for IBM Cloud VPC
}

// extractVersionFromImage extracts version from container image
func (p *VPCBootstrapProvider) extractVersionFromImage(image string) string {
	parts := strings.Split(image, ":")
	if len(parts) >= 2 {
		tag := parts[len(parts)-1]
		// If tag doesn't start with 'v', add it
		if !strings.HasPrefix(tag, "v") && tag != "latest" {
			return "v" + tag
		}
		return tag
	}
	return "latest"
}

// buildKubeletConfig builds kubelet configuration for VPC mode
func (p *VPCBootstrapProvider) buildKubeletConfig(clusterConfig *commonTypes.ClusterConfig) *commonTypes.KubeletConfig {
	config := &commonTypes.KubeletConfig{
		ClusterDNS: []string{clusterConfig.DNSClusterIP},
		ExtraArgs:  make(map[string]string),
	}

	// Add provider ID configuration for VPC mode  
	config.ExtraArgs["provider-id"] = "ibm://$(curl -s -H 'Authorization: Bearer TOKEN' https://api.metadata.cloud.ibm.com/metadata/v1/instance | jq -r '.id')"

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

// getClusterCA extracts the cluster CA certificate from the current kubeconfig
func (p *VPCBootstrapProvider) getClusterCA(ctx context.Context) (string, error) {
	logger := log.FromContext(ctx)
	
	// For VPC clusters, try to get CA from kube-root-ca.crt ConfigMap first
	// This is the standard location in modern Kubernetes clusters
	cm, err := p.k8sClient.CoreV1().ConfigMaps("kube-system").Get(ctx, "kube-root-ca.crt", metav1.GetOptions{})
	if err == nil {
		if caCert, exists := cm.Data["ca.crt"]; exists && caCert != "" {
			logger.Info("Successfully extracted cluster CA certificate from ConfigMap", 
				"configMapName", "kube-root-ca.crt", 
				"caSize", len(caCert))
			return caCert, nil
		}
	}
	
	// Fallback: Get cluster CA from kube-system namespace's default service account token
	// This is for older clusters or IKS clusters (though IKS doesn't use this bootstrap method)
	secret, err := p.k8sClient.CoreV1().Secrets("kube-system").Get(ctx, "default-token", metav1.GetOptions{})
	if err != nil {
		// Try to get CA from any service account token in kube-system
		secrets, err := p.k8sClient.CoreV1().Secrets("kube-system").List(ctx, metav1.ListOptions{
			FieldSelector: "type=kubernetes.io/service-account-token",
		})
		if err != nil || len(secrets.Items) == 0 {
			return "", fmt.Errorf("unable to find CA certificate in ConfigMap or service account tokens")
		}
		secret = &secrets.Items[0]
	}
	
	// Extract CA certificate from secret
	caCert, exists := secret.Data["ca.crt"]
	if !exists {
		return "", fmt.Errorf("ca.crt not found in service account token secret")
	}
	
	logger.Info("Successfully extracted cluster CA certificate from service account token", 
		"secretName", secret.Name, 
		"caSize", len(caCert))
	
	return string(caCert), nil
}

// getClusterDNS gets the cluster DNS service IP
func (p *VPCBootstrapProvider) getClusterDNS(ctx context.Context) (string, error) {
	// Get kube-dns or coredns service
	services := []string{"kube-dns", "coredns"}
	for _, svcName := range services {
		svc, err := p.k8sClient.CoreV1().Services("kube-system").Get(ctx, svcName, metav1.GetOptions{})
		if err == nil && svc.Spec.ClusterIP != "" {
			return svc.Spec.ClusterIP, nil
		}
	}
	
	// Try to get from kubelet configmap
	cm, err := p.k8sClient.CoreV1().ConfigMaps("kube-system").Get(ctx, "kubelet-config", metav1.GetOptions{})
	if err == nil {
		// Parse YAML to find clusterDNS - simplified version
		if data, ok := cm.Data["kubelet"]; ok && strings.Contains(data, "clusterDNS:") {
			lines := strings.Split(data, "\n")
			for i, line := range lines {
				if strings.Contains(line, "clusterDNS:") && i+1 < len(lines) {
					// Next line should have the DNS IP
					dnsLine := strings.TrimSpace(lines[i+1])
					if strings.HasPrefix(dnsLine, "- ") {
						return strings.TrimPrefix(dnsLine, "- "), nil
					}
				}
			}
		}
	}
	
	return "", fmt.Errorf("unable to determine cluster DNS IP")
}

// getNodeClaim retrieves the NodeClaim object
func (p *VPCBootstrapProvider) getNodeClaim(ctx context.Context, nodeClaimName types.NamespacedName) (*karpv1.NodeClaim, error) {
	if p.kubeClient == nil {
		return nil, fmt.Errorf("kubeClient is nil, cannot retrieve NodeClaim")
	}
	nodeClaim := &karpv1.NodeClaim{}
	if err := p.kubeClient.Get(ctx, nodeClaimName, nodeClaim); err != nil {
		return nil, err
	}
	return nodeClaim, nil
}

// detectArchitectureFromInstanceProfile detects the architecture from instance profile using IBM Cloud API
func (p *VPCBootstrapProvider) detectArchitectureFromInstanceProfile(instanceProfile string) string {
	if instanceProfile == "" {
		return "amd64"
	}
	
	// Try to get the actual architecture from IBM Cloud API
	if p.client != nil {
		if vpcClient, err := p.client.GetVPCClient(); err == nil {
			if profileCollection, _, err := vpcClient.ListInstanceProfiles(nil); err == nil {
				for _, profile := range profileCollection.Profiles {
					if profile.Name != nil && *profile.Name == instanceProfile {
						if profile.VcpuArchitecture != nil && profile.VcpuArchitecture.Value != nil {
							return *profile.VcpuArchitecture.Value
						}
					}
				}
			}
		}
	}
	
	// Fallback to sensible defaults based on common IBM Cloud instance profiles
	switch {
	case strings.Contains(strings.ToLower(instanceProfile), "z"):
		return "s390x"
	case strings.Contains(strings.ToLower(instanceProfile), "arm"):
		return "arm64"
	default:
		return "amd64"
	}
}

