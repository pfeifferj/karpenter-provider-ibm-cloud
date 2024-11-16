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
	"fmt"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/providers/instancetype"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
	instancetypepkg "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
	"sigs.k8s.io/karpenter/pkg/operator"
)

func main() {
	ctx, op := operator.NewOperator()

	// Ensure IBM Cloud API key is set
	if os.Getenv("IBMCLOUD_API_KEY") == "" {
		log.FromContext(ctx).Error(fmt.Errorf("IBMCLOUD_API_KEY environment variable is required"), "failed to initialize provider")
		os.Exit(1)
	}

	// Create instance type provider
	instanceTypeProvider, err := instancetypepkg.NewProvider()
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create instance type provider")
		os.Exit(1)
	}

	// Create instance type controller
	instanceTypeController, err := instancetype.NewController()
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create instance type controller")
		os.Exit(1)
	}

	// Create instance provider
	instanceProvider := instance.NewProvider()

	// Create cloud provider
	cloudProvider := cloudprovider.New(
		op.GetClient(),
		op.EventRecorder,
		instanceTypeProvider,
		instanceProvider,
	)

	// Register controllers and start the operator
	op.
		WithControllers(
			ctx,
			instanceTypeController,
		).
		WithCloudProvider(cloudProvider).
		Start(ctx)
}
