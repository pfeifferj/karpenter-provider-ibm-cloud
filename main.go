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
	"context"
	"fmt"
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/karpenter/pkg/events"
	coreoptions "sigs.k8s.io/karpenter/pkg/operator/options"
	"sigs.k8s.io/karpenter/pkg/operator/injection"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/operator"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/operator/options"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
	instancetypepkg "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
)

func init() {
	// Initialize the logger before anything else
	klog.InitFlags(nil)
	logger := zap.New(zap.UseDevMode(true))
	log.SetLogger(logger)
}

func main() {
	ctx := context.Background()

	// Ensure IBM Cloud API key is set
	if os.Getenv("IBM_API_KEY") == "" {
		log.FromContext(ctx).Error(fmt.Errorf("IBM_API_KEY environment variable is required"), "failed to initialize provider")
		os.Exit(1)
	}

	// Get the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get in-cluster config")
		os.Exit(1)
	}

	// Create operator
	op, err := operator.NewOperator(ctx, config)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create operator")
		os.Exit(1)
	}

	// Create instance type provider
	instanceTypeProvider, err := instancetypepkg.NewProvider()
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create instance type provider")
		os.Exit(1)
	}

	// Create instance provider
	instanceProvider, err := instance.NewProvider()
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create instance provider")
		os.Exit(1)
	}

	// Create IBM client
	ibmClient, err := ibm.NewClient()
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create IBM client")
		os.Exit(1)
	}

	// Create event recorder
	recorder := events.NewRecorder(op.GetEventRecorder())

	// Create cloud provider
	cloudProvider := cloudprovider.New(
		op.GetClient(),
		recorder,
		ibmClient,
		instanceTypeProvider,
		instanceProvider,
	)

	// Inject options into context using the proper Karpenter injection pattern
	// This ensures all controllers have access to options during reconciliation
	ctx = injection.WithOptionsOrDie(ctx, coreoptions.Injectables...)
	
	// Also inject IBM-specific options
	ibmOpts := options.NewOptions()
	ctx = options.WithOptions(ctx, ibmOpts)

	// Create manager with context that has all required options
	// Pass the context to the manager so it can inject options into reconciliation contexts
	mgr, err := manager.New(config, manager.Options{
		BaseContext: func() context.Context {
			return ctx
		},
	})
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create manager")
		os.Exit(1)
	}

	// Configure health check endpoint
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.FromContext(ctx).Error(err, "failed to add healthz check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.FromContext(ctx).Error(err, "failed to add readyz check")
		os.Exit(1)
	}

	// Register controllers
	if err := controllers.RegisterControllers(ctx, mgr, clock.RealClock{}, op.GetClient(), recorder, op.GetUnavailableOfferings(), cloudProvider, instanceProvider, instanceTypeProvider); err != nil {
		log.FromContext(ctx).Error(err, "failed to register controllers with manager")
		os.Exit(1)
	}

	// Start the manager
	if err := mgr.Start(ctx); err != nil {
		log.FromContext(ctx).Error(err, "failed to start manager")
		os.Exit(1)
	}
}
