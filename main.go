package main

import (
	ibmcloud "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/operator"

	"sigs.k8s.io/karpenter/pkg/cloudprovider/metrics"
	corecontrollers "sigs.k8s.io/karpenter/pkg/controllers"
	"sigs.k8s.io/karpenter/pkg/controllers/state"
	coreoperator "sigs.k8s.io/karpenter/pkg/operator"
)

func main() {
	// Create core operator (handles options context properly)
	coreCtx, coreOp := coreoperator.NewOperator()
	ctx, op := operator.NewOperator(coreCtx, coreOp)

	// Create IBM cloud provider using providers from the factory
	ibmCloudProvider := ibmcloud.New(
		op.GetClient(),
		op.EventRecorder,
		op.ProviderFactory.GetClient(),
		op.ProviderFactory.GetInstanceTypeProvider(),
		op.ProviderFactory.GetSubnetProvider(),
	)
	cloudProvider := metrics.Decorate(ibmCloudProvider)
	clusterState := state.NewCluster(op.Clock, op.GetClient(), cloudProvider)

	// Register controllers using the proper operator pattern
	op.
		WithControllers(ctx, corecontrollers.NewControllers(
			ctx,
			op.Manager,
			op.Clock,
			op.GetClient(),
			op.EventRecorder,
			cloudProvider,
			clusterState,
		)...).
		WithControllers(ctx, controllers.NewControllers(
			ctx,
			op.Manager,
			op.Clock,
			op.GetClient(),
			op.EventRecorder,
			op.UnavailableOfferings,
			cloudProvider,
			op.ProviderFactory.GetInstanceTypeProvider(),
		)...).
		Start(ctx)
}