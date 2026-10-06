/*
Copyright The Kubernetes Authors.

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

package providers

import (
	"context"
	"fmt"
	"os"
	"sync"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	ibmcache "github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cache"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/providers/common/instancetype"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/providers/common/pricing"
	commonTypes "github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/providers/common/types"
	iksProvider "github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/providers/iks/workerpool"
	vpcProvider "github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/providers/vpc/instance"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/providers/vpc/subnet"
)

// ProviderFactory creates the appropriate instance provider based on the NodeClass configuration
type ProviderFactory struct {
	client               *ibm.Client
	kubeClient           client.Client
	kubernetesClient     kubernetes.Interface
	pricingProvider      pricing.Provider
	subnetProvider       subnet.Provider
	instanceTypeProvider instancetype.Provider
	apiReader            client.Reader
	mu                   sync.Mutex
	vpc                  commonTypes.VPCInstanceProvider
	iks                  commonTypes.IKSWorkerPoolProvider
}

// NewProviderFactory creates a new provider factory
func NewProviderFactory(ctx context.Context, client *ibm.Client, kubeClient client.Client, kubernetesClient kubernetes.Interface, unavailableOfferings *ibmcache.UnavailableOfferings, options ...FactoryOption) *ProviderFactory {
	// Create shared providers
	var region string
	if client != nil {
		region = client.GetRegion()
	}
	pricingProvider := pricing.NewIBMPricingProvider(ctx, client, region)
	subnetProvider := subnet.NewProvider(client)
	instanceTypeProvider := instancetype.NewProvider(client, pricingProvider, unavailableOfferings)

	factory := &ProviderFactory{
		client:               client,
		kubeClient:           kubeClient,
		kubernetesClient:     kubernetesClient,
		pricingProvider:      pricingProvider,
		subnetProvider:       subnetProvider,
		instanceTypeProvider: instanceTypeProvider,
		apiReader:            kubeClient,
	}
	for _, option := range options {
		option(factory)
	}
	return factory
}

type FactoryOption func(*ProviderFactory)

func WithAPIReader(reader client.Reader) FactoryOption {
	return func(f *ProviderFactory) { f.apiReader = reader }
}

// WithVPCInstanceProvider supplies the VPC instance provider instead of building one lazily.
func WithVPCInstanceProvider(provider commonTypes.VPCInstanceProvider) FactoryOption {
	return func(f *ProviderFactory) { f.vpc = provider }
}

func (f *ProviderFactory) GetInstanceProvider(nodeClass *v1alpha1.IBMNodeClass) (commonTypes.InstanceProvider, error) {
	if nodeClass == nil {
		return nil, fmt.Errorf("nodeClass cannot be nil")
	}
	return f.GetInstanceProviderForMode(f.determineProviderMode(nodeClass))
}

func (f *ProviderFactory) GetInstanceProviderForMode(mode commonTypes.ProviderMode) (commonTypes.InstanceProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch mode {
	case commonTypes.IKSMode:
		if f.iks == nil {
			provider, err := iksProvider.NewIKSWorkerPoolProvider(f.client, f.kubeClient,
				iksProvider.WithAPIReader(f.apiReader),
				iksProvider.WithInstanceTypeProvider(f.instanceTypeProvider),
			)
			if err != nil {
				return nil, err
			}
			f.iks = provider
		}
		return f.iks, nil
	case commonTypes.VPCMode:
		if f.vpc == nil {
			options := []vpcProvider.Option{vpcProvider.WithAPIReader(f.apiReader)}
			if f.kubernetesClient != nil {
				options = append(options, vpcProvider.WithKubernetesClient(f.kubernetesClient))
			}
			provider, err := vpcProvider.NewVPCInstanceProvider(f.client, f.kubeClient, options...)
			if err != nil {
				return nil, err
			}
			f.vpc = provider
		}
		return f.vpc, nil
	default:
		return nil, fmt.Errorf("unknown provider mode: %s", mode)
	}
}

func (f *ProviderFactory) GetVPCProvider(nodeClass *v1alpha1.IBMNodeClass) (commonTypes.VPCInstanceProvider, error) {
	if nodeClass == nil {
		return nil, fmt.Errorf("nodeClass cannot be nil")
	}
	mode := f.determineProviderMode(nodeClass)
	if mode != commonTypes.VPCMode {
		return nil, fmt.Errorf("VPC provider requested but NodeClass is configured for %s mode", mode)
	}
	provider, err := f.GetInstanceProviderForMode(mode)
	if err != nil {
		return nil, err
	}
	return provider.(commonTypes.VPCInstanceProvider), nil
}

func (f *ProviderFactory) GetIKSProvider(nodeClass *v1alpha1.IBMNodeClass) (commonTypes.IKSWorkerPoolProvider, error) {
	if nodeClass == nil {
		return nil, fmt.Errorf("nodeClass cannot be nil")
	}
	mode := f.determineProviderMode(nodeClass)
	if mode != commonTypes.IKSMode {
		return nil, fmt.Errorf("IKS provider requested but NodeClass is configured for %s mode", mode)
	}
	provider, err := f.GetInstanceProviderForMode(mode)
	if err != nil {
		return nil, err
	}
	return provider.(commonTypes.IKSWorkerPoolProvider), nil
}

// determineProviderMode determines which provider mode to use based on NodeClass configuration
func (f *ProviderFactory) determineProviderMode(nodeClass *v1alpha1.IBMNodeClass) commonTypes.ProviderMode {
	// Handle nil nodeClass - default to VPC mode
	if nodeClass == nil {
		// Check environment variable for IKS mode
		if os.Getenv("IKS_CLUSTER_ID") != "" {
			return commonTypes.IKSMode
		}
		return commonTypes.VPCMode
	}

	// Check if bootstrap mode is explicitly set
	if nodeClass.Spec.BootstrapMode != nil {
		switch *nodeClass.Spec.BootstrapMode {
		case "iks-api":
			return commonTypes.IKSMode
		case "cloud-init":
			return commonTypes.VPCMode
		case "auto":
			// Continue with automatic detection based on other indicators
		}
	}

	// Check if IKS cluster ID is provided (implies IKS mode)
	if nodeClass.Spec.IKSClusterID != "" {
		return commonTypes.IKSMode
	}

	// Check environment variable
	if os.Getenv("IKS_CLUSTER_ID") != "" {
		return commonTypes.IKSMode
	}

	// Default to VPC mode
	return commonTypes.VPCMode
}

// GetProviderMode returns the provider mode for a given NodeClass
func (f *ProviderFactory) GetProviderMode(nodeClass *v1alpha1.IBMNodeClass) commonTypes.ProviderMode {
	return f.determineProviderMode(nodeClass)
}

// GetPricingProvider returns the shared pricing provider
func (f *ProviderFactory) GetPricingProvider() pricing.Provider {
	return f.pricingProvider
}

// GetSubnetProvider returns the shared subnet provider
func (f *ProviderFactory) GetSubnetProvider() subnet.Provider {
	return f.subnetProvider
}

// GetInstanceTypeProvider returns the shared instance type provider
func (f *ProviderFactory) GetInstanceTypeProvider() instancetype.Provider {
	return f.instanceTypeProvider
}

// GetClient returns the IBM Cloud client
func (f *ProviderFactory) GetClient() *ibm.Client {
	return f.client
}
