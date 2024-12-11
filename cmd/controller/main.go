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
	"os"

	ibmv1 "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1"
	ibmv1alpha1 "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/scheme"
	ibmcloud "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/events"
)

func init() {
	klog.InitFlags(nil)
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	logger := zap.New(zap.UseFlagOptions(&opts))
	log.SetLogger(logger)
}

// NodeClaimReconciler handles NodeClaim resources
type NodeClaimReconciler struct {
	client.Client
	CloudProvider *ibmcloud.CloudProvider
}

// NodePoolReconciler handles NodePool resources
type NodePoolReconciler struct {
	client.Client
	CloudProvider *ibmcloud.CloudProvider
}

// IBMNodeClassReconciler handles IBMNodeClass resources
type IBMNodeClassReconciler struct {
	client.Client
	CloudProvider *ibmcloud.CloudProvider
}

func NewNodeClaimReconciler(kubeClient client.Client, provider *ibmcloud.CloudProvider) *NodeClaimReconciler {
	return &NodeClaimReconciler{
		Client:        kubeClient,
		CloudProvider: provider,
	}
}

func NewNodePoolReconciler(kubeClient client.Client, provider *ibmcloud.CloudProvider) *NodePoolReconciler {
	return &NodePoolReconciler{
		Client:        kubeClient,
		CloudProvider: provider,
	}
}

func NewIBMNodeClassReconciler(kubeClient client.Client, provider *ibmcloud.CloudProvider) *IBMNodeClassReconciler {
	return &IBMNodeClassReconciler{
		Client:        kubeClient,
		CloudProvider: provider,
	}
}

func (r *NodeClaimReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("nodeclaim", req.NamespacedName)
	logger.Info("Starting reconciliation")

	var nodeClaim ibmv1.NodeClaim
	if err := r.Get(ctx, req.NamespacedName, &nodeClaim); err != nil {
		logger.Error(err, "Failed to get NodeClaim")
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Implement NodeClaim reconciliation logic
	// This should handle node provisioning and lifecycle management

	return reconcile.Result{}, nil
}

func (r *NodePoolReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("nodepool", req.NamespacedName)
	logger.Info("Starting NodePool reconciliation")

	var nodePool ibmv1.NodePool
	if err := r.Get(ctx, req.NamespacedName, &nodePool); err != nil {
		logger.Error(err, "Failed to get NodePool")
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Implement NodePool reconciliation logic
	// This should handle scaling decisions based on pending pods
	// and coordinate with the CloudProvider for node provisioning

	return reconcile.Result{}, nil
}

func (r *IBMNodeClassReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("ibmnodeclass", req.NamespacedName)
	logger.Info("Starting IBMNodeClass reconciliation")

	var nodeClass ibmv1alpha1.IBMNodeClass
	if err := r.Get(ctx, req.NamespacedName, &nodeClass); err != nil {
		logger.Error(err, "Failed to get IBMNodeClass")
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Implement IBMNodeClass reconciliation logic
	// This should validate and maintain the node class configuration

	return reconcile.Result{}, nil
}

func main() {
	logger := log.FromContext(context.Background())
	logger.Info("Starting controller with debug logging enabled")

	cfg, err := config.GetConfig()
	if err != nil {
		logger.Error(err, "Error getting config")
		os.Exit(1)
	}

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

	// Add the IBM API schemes to the manager using the scheme package
	if err := scheme.AddToScheme(mgr.GetScheme()); err != nil {
		logger.Error(err, "Error adding IBM API schemes")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error(err, "Error setting up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error(err, "Error setting up ready check")
		os.Exit(1)
	}

	// Initialize providers
	var instanceTypeProvider instancetype.Provider
	var instanceProvider instance.Provider

	logger.Info("Initializing instance type provider")
	instanceTypeProviderImpl, err := instancetype.NewProvider()
	if err != nil {
		logger.Error(err, "Error creating instance type provider")
		os.Exit(1)
	}
	instanceTypeProvider = instanceTypeProviderImpl

	logger.Info("Initializing instance provider")
	instanceProviderImpl, err := instance.NewProvider()
	if err != nil {
		logger.Error(err, "Error creating instance provider")
		os.Exit(1)
	}
	instanceProvider = instanceProviderImpl

	recorder := events.NewRecorder(mgr.GetEventRecorderFor("karpenter-ibm-cloud"))

	// Create the cloud provider
	logger.Info("Creating cloud provider")
	cloudProvider := ibmcloud.New(
		mgr.GetClient(),
		recorder,
		instanceTypeProvider,
		instanceProvider,
	)

	// Create and set up the reconcilers
	nodeClaimReconciler := NewNodeClaimReconciler(mgr.GetClient(), cloudProvider)
	nodePoolReconciler := NewNodePoolReconciler(mgr.GetClient(), cloudProvider)
	nodeClassReconciler := NewIBMNodeClassReconciler(mgr.GetClient(), cloudProvider)

	// Set up controllers with their respective reconcilers
	if err := builder.ControllerManagedBy(mgr).
		For(&ibmv1.NodeClaim{}).
		Complete(nodeClaimReconciler); err != nil {
		logger.Error(err, "Error creating NodeClaim controller")
		os.Exit(1)
	}

	if err := builder.ControllerManagedBy(mgr).
		For(&ibmv1.NodePool{}).
		Complete(nodePoolReconciler); err != nil {
		logger.Error(err, "Error creating NodePool controller")
		os.Exit(1)
	}

	if err := builder.ControllerManagedBy(mgr).
		For(&ibmv1alpha1.IBMNodeClass{}).
		Complete(nodeClassReconciler); err != nil {
		logger.Error(err, "Error creating IBMNodeClass controller")
		os.Exit(1)
	}

	logger.Info("Starting manager")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		logger.Error(err, "Error running manager")
		os.Exit(1)
	}
}
