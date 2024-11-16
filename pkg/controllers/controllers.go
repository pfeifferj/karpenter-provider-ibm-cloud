package controllers

import (
	"context"

	"github.com/awslabs/operatorpkg/controller"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cache"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/interruption"
	nodeclaimgarbagecollection "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/nodeclaim/garbagecollection"
	nodeclaimtagging "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/nodeclaim/tagging"
	nodeclasshash "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/nodeclass/hash"
	nodeclaasstatus "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/nodeclass/status"
	nodeclasstermination "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/nodeclass/termination"
	providersinstancetype "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/providers/instancetype"
	controllerspricing "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/providers/pricing"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/operator/options"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/pricing"
)

func NewControllers(ctx context.Context, mgr manager.Manager, clk clock.Clock,
	kubeClient client.Client, recorder events.Recorder,
	unavailableOfferings *cache.UnavailableOfferings,
	cloudProvider cloudprovider.CloudProvider,
	instanceProvider instance.Provider, instanceTypeProvider instancetype.Provider,
	pricingProvider pricing.Provider) []controller.Controller {

	controllers := []controller.Controller{
		nodeclasshash.NewController(kubeClient),
		nodeclaasstatus.NewController(kubeClient),
		nodeclasstermination.NewController(kubeClient, recorder),
		controllerspricing.NewController(pricingProvider),
		nodeclaimgarbagecollection.NewController(kubeClient, cloudProvider),
		nodeclaimtagging.NewController(kubeClient, instanceProvider),
		providersinstancetype.NewController(instanceTypeProvider),
	}

	if options.FromContext(ctx).Interruption {
		controllers = append(controllers, interruption.NewController(kubeClient, recorder, unavailableOfferings))
	}
	return controllers
}
