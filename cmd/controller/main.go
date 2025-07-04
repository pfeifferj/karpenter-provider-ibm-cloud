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