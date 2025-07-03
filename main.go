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
	ctx, op := operator.NewOperator(coreoperator.NewOperator())

	// Create IBM cloud provider using operator's providers
	ibmCloudProvider := ibmcloud.New(
		op.GetClient(),
		op.EventRecorder,
		op.IBMClient,
		op.InstanceTypeProvider,
		op.InstanceProvider,
		op.SubnetProvider,
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
			op.InstanceProvider,
			op.InstanceTypeProvider,
		)...).
		Start(ctx)
}