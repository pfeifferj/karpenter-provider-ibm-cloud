package operator

import (
	"context"
	"fmt"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/karpenter/pkg/operator"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cache"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/subnet"
)

// Operator wraps the core Karpenter operator with IBM-specific providers
type Operator struct {
	*operator.Operator
	UnavailableOfferings *cache.UnavailableOfferings
	IBMClient            *ibm.Client
	InstanceProvider     instance.Provider
	InstanceTypeProvider instancetype.Provider
	SubnetProvider       subnet.Provider
}

func NewOperator(ctx context.Context, coreOperator *operator.Operator) (context.Context, *Operator) {
	// Validate IBM credentials early
	if err := validateIBMCredentials(ctx); err != nil {
		log.FromContext(ctx).Error(err, "Failed to validate IBM Cloud credentials")
		os.Exit(1)
	}

	// Create IBM client
	ibmClient, err := ibm.NewClient()
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to create IBM client")
		os.Exit(1)
	}

	// Create providers
	instanceTypeProvider, err := instancetype.NewProvider()
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to create instance type provider")
		os.Exit(1)
	}

	instanceProvider, err := instance.NewProvider()
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to create instance provider")
		os.Exit(1)
	}
	instanceProvider.SetKubeClient(coreOperator.GetClient())

	subnetProvider, err := subnet.NewProvider()
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to create subnet provider")
		os.Exit(1)
	}

	unavailableOfferings := cache.NewUnavailableOfferings()

	return ctx, &Operator{
		Operator:             coreOperator,
		UnavailableOfferings: unavailableOfferings,
		IBMClient:            ibmClient,
		InstanceProvider:     instanceProvider,
		InstanceTypeProvider: instanceTypeProvider,
		SubnetProvider:       subnetProvider,
	}
}

func validateIBMCredentials(ctx context.Context) error {
	requiredEnvVars := []string{"IBM_REGION", "IBM_API_KEY", "VPC_API_KEY"}
	for _, envVar := range requiredEnvVars {
		if os.Getenv(envVar) == "" {
			return fmt.Errorf("%s environment variable is not set", envVar)
		}
		log.FromContext(ctx).Info("Found required environment variable", "name", envVar)
	}

	// Validate by creating client
	_, err := ibm.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create IBM client: %w", err)
	}

	log.FromContext(ctx).Info("Successfully authenticated with IBM Cloud")
	return nil
}
