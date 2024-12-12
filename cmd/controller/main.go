/*
Copyright 2024 The Kubernetes Authors.

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

	ibmcloud "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/nodeclass/autoplacement"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/subnet"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/events"
)

func init() {
	// Initialize klog and zap logger with debug settings
	klog.InitFlags(nil)
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	logger := zap.New(zap.UseFlagOptions(&opts))
	log.SetLogger(logger)
}

type IBMCloudReconciler struct {
	client.Client
	CloudProvider *ibmcloud.CloudProvider
}

func NewIBMCloudReconciler(kubeClient client.Client, provider *ibmcloud.CloudProvider) *IBMCloudReconciler {
	return &IBMCloudReconciler{
		Client:        kubeClient,
		CloudProvider: provider,
	}
}

func (r *IBMCloudReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("nodeclaim", req.NamespacedName)
	logger.Info("Starting reconciliation")

	var nodeClaim v1.NodeClaim
	if err := r.Get(ctx, req.NamespacedName, &nodeClaim); err != nil {
		logger.Error(err, "Failed to get NodeClaim")
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("Processing NodeClaim",
		"name", nodeClaim.Name,
		"deletionTimestamp", nodeClaim.DeletionTimestamp,
		"providerID", nodeClaim.Status.ProviderID,
		"requirements", nodeClaim.Spec.Requirements)

	// Check if the NodeClaim has a finalizer for cleanup purposes
	if !controllerutil.ContainsFinalizer(&nodeClaim, "karpenter.sh/finalizer") {
		logger.Info("Adding finalizer to NodeClaim")
		controllerutil.AddFinalizer(&nodeClaim, "karpenter.sh/finalizer")
		if err := r.Update(ctx, &nodeClaim); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return reconcile.Result{}, err
		}
	}

	// Handle NodeClaim provisioning or deletion
	if nodeClaim.DeletionTimestamp.IsZero() {
		if nodeClaim.Status.ProviderID == "" {
			logger.Info("Provisioning new node")
			// Provision new instance using IBM Cloud Provider
			createdNodeClaim, err := r.CloudProvider.Create(ctx, &nodeClaim)
			if err != nil {
				logger.Error(err, "Failed to create node",
					"requirements", nodeClaim.Spec.Requirements,
					"labels", nodeClaim.Labels)
				return reconcile.Result{}, fmt.Errorf("creating node claim: %w", err)
			}
			logger.Info("Successfully created node",
				"providerID", createdNodeClaim.Status.ProviderID,
				"requirements", nodeClaim.Spec.Requirements)

			nodeClaim.Status = createdNodeClaim.Status
			if err := r.Status().Update(ctx, &nodeClaim); err != nil {
				logger.Error(err, "Failed to update NodeClaim status")
				return reconcile.Result{}, err
			}
			logger.Info("Successfully updated NodeClaim status")
		} else {
			logger.Info("Node already exists", "providerID", nodeClaim.Status.ProviderID)
		}
	} else {
		logger.Info("Deleting node", "providerID", nodeClaim.Status.ProviderID)
		// Handle deletion
		if err := r.CloudProvider.Delete(ctx, &nodeClaim); err != nil {
			logger.Error(err, "Failed to delete node")
			return reconcile.Result{}, fmt.Errorf("deleting node claim: %w", err)
		}
		logger.Info("Successfully deleted node")

		controllerutil.RemoveFinalizer(&nodeClaim, "karpenter.sh/finalizer")
		if err := r.Update(ctx, &nodeClaim); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return reconcile.Result{}, err
		}
		logger.Info("Successfully removed finalizer")
	}

	logger.Info("Completed reconciliation")
	return reconcile.Result{}, nil
}

func main() {
	logger := log.FromContext(context.Background())
	logger.Info("Starting controller with debug logging enabled")

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		logger.Error(err, "Error getting config")
		os.Exit(1)
	}

	// Create a new manager to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: server.Options{
			BindAddress: ":8080",
		},
		HealthProbeBindAddress: ":8081",
	})
	if err != nil {
		logger.Error(err, "Error creating manager")
		os.Exit(1)
	}

	// Add health check endpoints
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error(err, "Error setting up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error(err, "Error setting up ready check")
		os.Exit(1)
	}

	// Initialize providers
	logger.Info("Initializing providers")

	instanceTypeProviderImpl, err := instancetype.NewProvider()
	if err != nil {
		logger.Error(err, "Error creating instance type provider")
		os.Exit(1)
	}

	instanceProviderImpl, err := instance.NewProvider()
	if err != nil {
		logger.Error(err, "Error creating instance provider")
		os.Exit(1)
	}

	subnetProviderImpl := subnet.NewProvider()

	recorder := events.NewRecorder(mgr.GetEventRecorderFor("karpenter-ibm-cloud"))

	// Create the cloud provider
	logger.Info("Creating cloud provider")
	cloudProvider := ibmcloud.New(
		mgr.GetClient(),
		recorder,
		instanceTypeProviderImpl,
		instanceProviderImpl,
	)

	// Create the reconciler
	logger.Info("Creating reconcilers")
	nodeClaimReconciler := NewIBMCloudReconciler(mgr.GetClient(), cloudProvider)

	// Create the autoplacement controller
	autoplacementController, err := autoplacement.NewController(mgr, instanceTypeProviderImpl, subnetProviderImpl)
	if err != nil {
		logger.Error(err, "Error creating autoplacement controller")
		os.Exit(1)
	}

	// Create controllers
	if err := builder.ControllerManagedBy(mgr).
		For(&v1.NodeClaim{}).
		Complete(nodeClaimReconciler); err != nil {
		logger.Error(err, "Error creating nodeclaim controller")
		os.Exit(1)
	}

	// Add autoplacement controller to manager
	if err := mgr.Add(autoplacementController); err != nil {
		logger.Error(err, "Error adding autoplacement controller")
		os.Exit(1)
	}

	logger.Info("Starting manager")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		logger.Error(err, "Error running manager")
		os.Exit(1)
	}
}
