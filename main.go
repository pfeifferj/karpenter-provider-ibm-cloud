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

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/providers/instancetype"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
	instancetypepkg "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
	"sigs.k8s.io/karpenter/pkg/operator"
)

func init() {
	// Initialize the logger before anything else
	klog.InitFlags(nil)
	logger := zap.New(zap.UseDevMode(true))
	log.SetLogger(logger)
}

func main() {
	ctx, op := operator.NewOperator()

	// Ensure IBM Cloud API key is set
	if os.Getenv("IBM_API_KEY") == "" {
		log.FromContext(ctx).Error(fmt.Errorf("IBM_API_KEY environment variable is required"), "failed to initialize provider")
		os.Exit(1)
	}

	// Create instance type provider
	instanceTypeProvider, instanceTypeErr := instancetypepkg.NewProvider()
	if instanceTypeErr != nil {
		log.FromContext(ctx).Error(instanceTypeErr, "failed to create instance type provider")
		os.Exit(1)
	}

	// Create instance type controller
	instanceTypeController, err := instancetype.NewController()
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create instance type controller")
		os.Exit(1)
	}

	// Create instance provider
	instanceProvider, instanceErr := instance.NewProvider()
	if instanceErr != nil {
		log.FromContext(ctx).Error(instanceErr, "failed to create instance provider")
		os.Exit(1)
	}

	// Create cloud provider
	cloudProvider := cloudprovider.New(
		op.GetClient(),
		op.EventRecorder,
		instanceTypeProvider,
		instanceProvider,
	)

	// Configure health check endpoint
	if err := op.Manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.FromContext(ctx).Error(err, "failed to add healthz check")
		os.Exit(1)
	}
	if err := op.Manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.FromContext(ctx).Error(err, "failed to add readyz check")
		os.Exit(1)
	}

	// Register controllers and cloud provider
	op.RegisterController(ctx, instanceTypeController)
	op.RegisterCloudProvider("ibm", cloudProvider)

	// Start the operator
	if err := op.Start(ctx); err != nil {
		log.FromContext(ctx).Error(err, "failed to start operator")
		os.Exit(1)
	}
}
